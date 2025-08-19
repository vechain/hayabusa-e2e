package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/validations"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/hayabusa/stargate"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/thor/v2/api"
	genesisthor "github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
	"github.com/vechain/thor/v2/tx"
)

const (
	stakerAddress = "0x00000000000000000000000000005374616b6572"
	paramsAddress = "0x0000000000000000000000000000506172616d73"
)

type LowercaseString string

func (s *LowercaseString) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	*s = LowercaseString(strings.ToLower(str))
	return nil
}

type AddressKey struct {
	Address LowercaseString `json:"address"`
	Key     LowercaseString `json:"key"`
}

func startAgainstDevnet(ctx context.Context, devnet string, genesisURL string) (*lifecycle.Engine, func()) {
	client := thorclient.New(devnet)

	staker, err := builtin.NewStaker(client)
	if err != nil {
		slog.Error("failed to create staker client", "error", err)
		os.Exit(1)
	}

	genesis, config, err := loadHayabusaGenesis(genesisURL)
	if err != nil {
		slog.Error("failed to load Hayabusa genesis config", "error", err)
		os.Exit(1)
	}

	validators, err := loadHayabusaValidators(genesis)
	if err != nil {
		slog.Error("failed to load Hayabusa validators", "error", err)
		os.Exit(1)
	}

	stargateSigner, err := setStargate(staker)
	if err != nil {
		slog.Error("failed to set stargate", "error", err)
		os.Exit(1)
	}

	stack := stack.NewStack(ctx, staker, config, validators, stargateSigner)
	validationsState := validations.NewState(stack)
	generator := &devnetGenerator{
		config: config,
		stack:  stack,
	}
	engine := lifecycle.NewEngine(stack, validationsState, generator)
	if err := initializeSyntheticActivity(engine, generator, genesis); err != nil {
		slog.Error("failed to initialize synthetic activity", "error", err)
		os.Exit(1)
	}

	stop := func() {
		slog.Info("stopping Hayabusa devnet simulation")
	}

	return engine, stop
}

// loadHayabusaGenesis loads Hayabusa genesis configuration
func loadHayabusaGenesis(genesisURL string) (*genesisthor.CustomGenesis, *hayabusa.Config, error) {
	resp, err := http.Get(genesisURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch genesis.json: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read genesis.json: %w", err)
	}

	var genesis genesisthor.CustomGenesis
	if err := json.Unmarshal(body, &genesis); err != nil {
		return nil, nil, fmt.Errorf("failed to parse genesis.json: %w", err)
	}

	config, err := extractConfigFromAccounts(genesis.Accounts, &genesis)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to extract config from accounts: %w", err)
	}

	slog.Info("loaded Hayabusa genesis configuration",
		"config", config)

	return &genesis, config, nil
}

// Load validators from Hayabusa genesis.json
func loadHayabusaValidators(genesis *genesisthor.CustomGenesis) (map[thor.Address]*hayabusa.NodePair, error) {
	validators := make(map[thor.Address]*hayabusa.NodePair)

	endorsorAddressKeys, err := parseAddressKeysFromEnvToMap("ENDORSOR_KEYS")
	if err != nil {
		return nil, fmt.Errorf("failed to parse ENDORSOR_KEYS: %w", err)
	}

	authorityAddressKeys, err := parseAddressKeysFromEnvToMap("AUTHORITY_KEYS")
	if err != nil {
		return nil, fmt.Errorf("failed to parse AUTHORITY_KEYS: %w", err)
	}

	for _, authority := range genesis.Authority {
		endorserKey, err := crypto.HexToECDSA(endorsorAddressKeys[authority.EndorsorAddress.String()])
		if err != nil {
			return nil, fmt.Errorf("failed to parse endorser key %s: %w", endorsorAddressKeys[authority.EndorsorAddress.String()], err)
		}
		endorserSigner := bind.NewSigner(endorserKey)

		nodeKey, err := crypto.HexToECDSA(authorityAddressKeys[authority.MasterAddress.String()])
		if err != nil {
			return nil, fmt.Errorf("failed to parse master key %s: %w", authorityAddressKeys[authority.MasterAddress.String()], err)
		}
		nodeSigner := bind.NewSigner(nodeKey)

		nodePair := hayabusa.NewNodePairWithNode(endorserSigner, nodeSigner)
		validators[nodeSigner.Address()] = nodePair
	}

	slog.Info("loaded Hayabusa validators from genesis", "validatorCount", len(validators))

	return validators, nil
}

func setStargate(staker *builtin.Staker) (*bind.PrivateKeySigner, error) {
	accountsKeys, err := parseAddressKeysFromEnv("ACCOUNTS_KEYS")
	if err != nil {
		slog.Error("failed to parse ACCOUNTS_KEYS", "error", err)
		os.Exit(1)
	}

	accountKey, err := crypto.HexToECDSA(string(accountsKeys[0].Key))
	if err != nil {
		slog.Error("failed to parse account key", "error", err)
		return nil, err
	}
	accountSigner := bind.NewSigner(accountKey)

	// Check if stargate address is already set in params
	params, err := builtin.NewParams(staker.Raw().Client())
	if err != nil {
		return nil, fmt.Errorf("failed to create params: %w", err)
	}

	stargateKey := thor.MustParseBytes32(hayabusa.ParamsStargateKey)
	stargateAddress, err := params.Get(stargateKey)

	if err != nil {
		slog.Error("failed to get stargate address from params", "error", err)
	}
	if stargateAddress != nil && stargateAddress.Cmp(big.NewInt(0)) != 0 {
		slog.Info("stargate address already set in params", "stargateAddress", stargateAddress)
		return accountSigner, nil
	}

	// If stargate address is not set in params, deploy stargate contract
	executorAddressKeys, err := parseAddressKeysFromEnv("EXECUTOR_KEYS")
	if err != nil {
		slog.Error("failed to parse EXECUTOR_KEYS", "error", err)
		os.Exit(1)
	}

	genesis, err := staker.Raw().Client().Block("0")
	if err != nil {
		slog.Error("failed to get genesis block to set stargate", "error", err)
		return nil, err
	}
	chainTag := genesis.ID[31]

	bytecode := stargate.Bin
	bytecode = strings.TrimSpace(bytecode)
	bytes, err := hexutil.Decode("0x" + bytecode)
	clause := tx.NewClause(nil).WithData(bytes)

	energy, err := builtin.NewEnergy(staker.Raw().Client())
	if err != nil {
		slog.Error("failed to create energy client", "error", err)
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var receipt *api.Receipt

	balance, err := energy.BalanceOf(thor.MustParseAddress(string(executorAddressKeys[0].Address)))
	if err != nil {
		slog.Error("failed to get energy balance of executor", "error", err)
		return nil, err
	}

	if balance.Cmp(big.NewInt(0)) == 0 {
		executorEnergyValue, _ := new(big.Int).SetString("10000000000000000000000000000", 10)
		receipt, _, err = energy.Transfer(thor.MustParseAddress(string(executorAddressKeys[0].Address)), executorEnergyValue).
			Send().
			WithSigner(accountSigner).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(ctx)

		slog.Info("energy transfer receipt", "receipt", receipt)

		if err != nil || receipt == nil {
			slog.Error("failed to transfer energy to executor", "error", err)
			return nil, err
		}
		if receipt.Reverted {
			return nil, fmt.Errorf("transaction to transfer energy to executor reverted: %s", receipt.Meta.TxID)
		}
		slog.Info("energy transferred to executor", "receipt", receipt)
	}

	initialBaseFee := new(big.Int).SetUint64(thor.InitialBaseFee)
	feesHistory, err := staker.Raw().Client().FeesHistory(1, "best", []float64{0.5})
	if err != nil {
		slog.Error("failed to get fees history", "error", err)
		return nil, err
	}
	baseFee := (*big.Int)(feesHistory.BaseFeePerGas[0])
	maxPriorityFeePerGas := new(big.Int).Div(new(big.Int).Mul(baseFee, big.NewInt(5)), big.NewInt(100))
	maxFeePerGas := new(big.Int).Add(initialBaseFee, maxPriorityFeePerGas)
	trx := tx.NewBuilder(tx.TypeDynamicFee).
		Clause(clause).
		Gas(40_000_000).
		Nonce(datagen.RandUint64()).
		ChainTag(chainTag).
		Expiration(100000).
		MaxFeePerGas(maxFeePerGas).
		MaxPriorityFeePerGas(maxPriorityFeePerGas).
		Build()

	trx, err = accountSigner.SignTransaction(trx)
	if err != nil {
		slog.Error("failed to sign transaction to set stargate", "error", err)
		return nil, err
	}
	res, err := staker.Raw().Client().SendTransaction(trx)
	if err != nil {
		slog.Error("failed to send transaction to set stargate", "error", err)
		return nil, err
	}

	for range 30 {
		receipt, err = staker.Raw().Client().TransactionReceipt(res.ID)
		if err == nil && receipt != nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if receipt == nil {
		return nil, fmt.Errorf("failed to get transaction receipt to set stargate")
	}

	// The stargate contract should be deployed by now
	contractAddr := receipt.Outputs[0].ContractAddress

	slog.Info("stargate contract deployed", "contractAddr", contractAddr)

	// Set stargate address in params
	stargateAddress = new(big.Int).SetBytes(contractAddr.Bytes())

	executorKey, err := crypto.HexToECDSA(string(executorAddressKeys[0].Key))
	if err != nil {
		slog.Error("failed to parse executor key", "error", err)
		os.Exit(1)
	}
	executorSigner := bind.NewSigner(executorKey)

	receipt, _, err = params.Set(stargateKey, stargateAddress).
		Send().
		WithSigner(executorSigner).
		WithOptions(testutil.TxOptions()).
		SubmitAndConfirm(ctx)

	if err != nil || receipt == nil {
		return nil, fmt.Errorf("failed to set stargate address in params: %w", err)
	}
	if receipt.Reverted {
		return nil, fmt.Errorf("transaction to set stargate address in params reverted: %s", receipt.Meta.TxID)
	}

	return accountSigner, nil
}

func parseAddressKeysFromEnv(envVar string) ([]AddressKey, error) {
	keysStr := os.Getenv(envVar)
	if keysStr == "" {
		return nil, fmt.Errorf("%s environment variable is required", envVar)
	}

	var addressKeys []AddressKey
	if err := json.Unmarshal([]byte(keysStr), &addressKeys); err != nil {
		return nil, fmt.Errorf("failed to parse %s JSON: %w", envVar, err)
	}

	return addressKeys, nil
}

func parseAddressKeysFromEnvToMap(envVar string) (map[string]string, error) {
	addressKeys, err := parseAddressKeysFromEnv(envVar)
	if err != nil {
		return nil, err
	}

	addressKeyMap := make(map[string]string)
	for _, key := range addressKeys {
		addressKeyMap[string(key.Address)] = string(key.Key)
	}

	return addressKeyMap, nil
}

func extractConfigFromAccounts(accounts []genesisthor.Account, genesis *genesisthor.CustomGenesis) (*hayabusa.Config, error) {
	config := &hayabusa.Config{
		Nodes:            1,
		ForkBlock:        uint32(genesis.ForkConfig.HAYABUSA),
		TransitionPeriod: uint32(genesis.ForkConfig.HAYABUSA_TP),
	}

	var stakerAccount, paramsAccount *genesisthor.Account

	for _, account := range accounts {
		accountAddr := account.Address.String()

		switch accountAddr {
		case stakerAddress:
			stakerAccount = &account
		case paramsAddress:
			paramsAccount = &account
		}
	}

	if stakerAccount != nil {
		if err := processStakerConfig(config, stakerAccount.Storage); err != nil {
			return nil, fmt.Errorf("failed to process staker config: %w", err)
		}
	}

	if paramsAccount != nil {
		if err := processParamsConfig(config, paramsAccount.Storage); err != nil {
			return nil, fmt.Errorf("failed to process params config: %w", err)
		}
	}

	return config, nil
}

func processStakerConfig(config *hayabusa.Config, storage map[string]thor.Bytes32) error {
	paramMappings := map[string]*uint32{
		"staker-low-staking-period":    &config.MinStakingPeriod,
		"staker-medium-staking-period": &config.MidStakingPeriod,
		"staker-high-staking-period":   &config.HighStakingPeriod,
		"cooldown-period":              &config.CooldownPeriod,
		"epoch-length":                 &config.EpochLength,
	}

	return processStorageValues(storage, paramMappings)
}

func processParamsConfig(config *hayabusa.Config, storage map[string]thor.Bytes32) error {
	paramMappings := map[string]*uint32{
		"max-block-proposers": &config.MaxBlockProposers,
	}

	return processStorageValues(storage, paramMappings)
}

func processStorageValues(storage map[string]thor.Bytes32, paramMappings map[string]*uint32) error {
	for storageKey, storageValue := range storage {
		storageKeyWithout0x := strings.TrimPrefix(storageKey, "0x")
		storageKeyBytes, err := hex.DecodeString(storageKeyWithout0x)
		if err != nil {
			return fmt.Errorf("failed to decode storage key %s: %w", storageKey, err)
		}

		paramName := string(storageKeyBytes)
		paramName = strings.TrimLeft(paramName, "\x00")

		if fieldPtr, exists := paramMappings[paramName]; exists {
			value, err := parseStorageValue(storageValue)
			if err != nil {
				return fmt.Errorf("failed to parse storage value for %s: %w", paramName, err)
			}
			*fieldPtr = value
		}
	}

	return nil
}

func parseStorageValue(storageValue thor.Bytes32) (uint32, error) {
	valueStr := strings.TrimPrefix(storageValue.String(), "0x")

	value, err := strconv.ParseUint(valueStr, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid storage value format: %s", storageValue)
	}

	return uint32(value), nil
}

// Devnet generator with realistic synthetic activity
type devnetGenerator struct {
	config *hayabusa.Config
	stack  *stack.Stack
}

func (g *devnetGenerator) CreateValidator(acc *hayabusa.NodePair, startBlock uint32) lifecycle.ValidatorConfig {
	// Create validators with different staking strategies
	stakingStrategy := utils.RandomBetween(1, 4)
	var stakingPeriods uint32
	var queueDelay lifecycle.Delay

	switch stakingStrategy {
	case 1: // Long-term validators
		stakingPeriods = uint32(utils.RandomBetween(100, 500))
		queueDelay = lifecycle.Delay{Blocks: 0, Epochs: 0}
	case 2: // Medium-term validators
		stakingPeriods = uint32(utils.RandomBetween(30, 100))
		queueDelay = lifecycle.Delay{Blocks: uint32(utils.RandomBetween(1, 10)), Epochs: 0}
	case 3: // Short-term validators
		stakingPeriods = uint32(utils.RandomBetween(6, 30))
		queueDelay = lifecycle.Delay{Blocks: uint32(utils.RandomBetween(5, 20)), Epochs: 0}
	case 4: // Validators with entry delay
		stakingPeriods = uint32(utils.RandomBetween(20, 80))
		queueDelay = lifecycle.Delay{Blocks: 0, Epochs: uint32(utils.RandomBetween(1, 5))}
	}

	return lifecycle.ValidatorConfig{
		Config: lifecycle.Config{
			QueueDelay:     queueDelay,
			StartBlock:     startBlock,
			StakingPeriods: stakingPeriods,
			WithdrawDelay:  lifecycle.Delay{Blocks: uint32(utils.RandomBetween(1, 10)), Epochs: 0},
		},
		Account: acc,
	}
}

func (g *devnetGenerator) CreateDelegator(acc bind.Signer, startBlock uint32) lifecycle.DelegatorConfig {
	// Create delegators with different strategies
	delegationStrategy := utils.RandomBetween(1, 3)
	var stakingPeriods uint32
	var queueDelay lifecycle.Delay

	switch delegationStrategy {
	case 1: // Long-term delegators
		stakingPeriods = uint32(utils.RandomBetween(50, 200))
		queueDelay = lifecycle.Delay{Blocks: 0, Epochs: 0}
	case 2: // Medium-term delegators
		stakingPeriods = uint32(utils.RandomBetween(20, 50))
		queueDelay = lifecycle.Delay{Blocks: uint32(utils.RandomBetween(1, 15)), Epochs: 0}
	case 3: // Short-term delegators
		stakingPeriods = uint32(utils.RandomBetween(6, 20))
		queueDelay = lifecycle.Delay{Blocks: uint32(utils.RandomBetween(5, 25)), Epochs: 0}
	}

	return lifecycle.DelegatorConfig{
		Config: lifecycle.Config{
			QueueDelay:     queueDelay,
			StartBlock:     startBlock,
			StakingPeriods: stakingPeriods,
			WithdrawDelay:  lifecycle.Delay{Blocks: uint32(utils.RandomBetween(1, 15)), Epochs: 0},
		},
		Account: acc,
	}
}

// Initialize realistic synthetic activity
func initializeSyntheticActivity(engine *lifecycle.Engine, generator *devnetGenerator, genesis *genesisthor.CustomGenesis) error {
	validators, err := loadHayabusaValidators(genesis)
	if err != nil {
		slog.Error("failed to load validators for synthetic activity", "error", err)
		return err
	}

	// Create initial validators with different strategies
	validatorCount := 0
	totalValidators := len(validators)

	for _, nodePair := range validators {
		config := generator.CreateValidator(nodePair, 0)

		// Distribute staking strategies based on available validators
		if validatorCount < totalValidators/3 { // First third: long-term validators
			config.StakingPeriods = uint32(utils.RandomBetween(200, 500))
		} else if validatorCount < (totalValidators*2)/3 { // Second third: medium-term validators
			config.StakingPeriods = uint32(utils.RandomBetween(50, 150))
		} else { // Last third: short-term validators
			config.StakingPeriods = uint32(utils.RandomBetween(10, 40))
		}

		cycle := lifecycle.NewValidatorLifecycle(config)
		engine.AddLifecycle(cycle)
		validatorCount++
	}

	accountsAddressKeys, err := parseAddressKeysFromEnvToMap("ACCOUNTS_KEYS")
	if err != nil {
		return fmt.Errorf("failed to parse ACCOUNTS_KEYS: %w", err)
	}

	delegatorsCount := 15
	for i, account := range genesis.Accounts[0:delegatorsCount] {
		accountKey, err := crypto.HexToECDSA(accountsAddressKeys[account.Address.String()])
		if err != nil {
			return fmt.Errorf("failed to parse account key for %s: %w", account.Address, err)
		}
		delegatorSigner := bind.NewSigner(accountKey)
		config := generator.CreateDelegator(delegatorSigner, 0)

		// Distribute delegation strategies
		if i < delegatorsCount/3 { // Long-term delegators
			config.StakingPeriods = uint32(utils.RandomBetween(100, 300))
		} else if i < (delegatorsCount*2)/3 { // Medium-term delegators
			config.StakingPeriods = uint32(utils.RandomBetween(30, 80))
		} else { // Short-term delegators
			config.StakingPeriods = uint32(utils.RandomBetween(10, 30))
		}

		cycle := lifecycle.NewDelegatorLifecycle(config)
		engine.AddLifecycle(cycle)
	}

	slog.Info("initialized synthetic activity",
		"validatorCount", validatorCount,
		"delegatorCount", delegatorsCount)

	return nil
}
