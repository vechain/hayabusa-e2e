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

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/delegations"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/validators"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	genesisthor "github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
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

	hayabusaValidators, err := loadHayabusaValidators(genesis)
	if err != nil {
		slog.Error("failed to load Hayabusa validators", "error", err)
		os.Exit(1)
	}

	initialValidators := make(map[thor.Address]*hayabusa.NodePair)
	extraValidators := make(map[thor.Address]*hayabusa.NodePair)

	genesisValidatorCount := len(genesis.Authority)
	totalValidators := len(hayabusaValidators)

	validatorCount := 0
	for addr, validator := range hayabusaValidators {
		if validatorCount < genesisValidatorCount/2 {
			initialValidators[addr] = validator
		} else {
			extraValidators[addr] = validator
		}
		validatorCount++
	}

	slog.Info("separated validators",
		"genesisValidatorCount", genesisValidatorCount,
		"totalValidators", totalValidators,
		"initialValidators", len(initialValidators),
		"extraValidators", len(extraValidators))

	stargateSigner, err := setStargate(staker)
	if err != nil {
		slog.Error("failed to set stargate", "error", err)
		os.Exit(1)
	}

	stack := stack.NewStack(ctx, staker, config, extraValidators, stargateSigner)
	validationsState := validators.NewState(stack)
	delegations := delegations.NewManager(config.MaxBlockProposers, delegations.DistributionTypeEven, delegations.MainnetPositions)
	generator := &devnetGenerator{
		config: config,
		stack:  stack,
	}
	engine := lifecycle.NewEngine(stack, validationsState, delegations, generator)
	if err := initializeSyntheticActivity(stack, engine, generator, validationsState, initialValidators, delegations); err != nil {
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
		"config", *config)

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

func getGasFees(staker *builtin.Staker) (gas uint64, maxFeePerGas *big.Int, maxPriorityFeePerGas *big.Int, err error) {
	gas = uint64(40_000_000)
	feesHistory, err := staker.Raw().Client().FeesHistory(1, "next", []float64{})
	if err != nil {
		slog.Error("failed to get fees history", "error", err)
		return 0, nil, nil, err
	}
	baseFee := (*big.Int)(feesHistory.BaseFeePerGas[0])
	maxPriorityFeePerGas = new(big.Int).Div(new(big.Int).Mul(baseFee, big.NewInt(5)), big.NewInt(100))
	maxFeePerGas = new(big.Int).Add(baseFee, maxPriorityFeePerGas)

	slog.Info("gas fees", "gas", gas, "maxFeePerGas", maxFeePerGas, "maxPriorityFeePerGas", maxPriorityFeePerGas)

	return gas, maxFeePerGas, maxPriorityFeePerGas, nil
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
		slog.Info("stargate address already set in params", "stargateAddress", thor.BytesToAddress(stargateAddress.Bytes()))
		return accountSigner, nil
	}

	// If stargate address is not set in params, set it
	gas, maxFeePerGas, maxPriorityFeePerGas, err := getGasFees(staker)
	if err != nil {
		slog.Error("failed to get gas fees for setting stargate address in params", "error", err)
		return nil, err
	}

	txOptions := &bind.TxOptions{
		Gas:                  &gas,
		MaxFeePerGas:         maxFeePerGas,
		MaxPriorityFeePerGas: maxPriorityFeePerGas,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	receipt, _, err := params.Set(stargateKey, new(big.Int).SetBytes(accountSigner.Address().Bytes())).
		Send().
		WithSigner(accountSigner).
		WithOptions(txOptions).
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

	var paramsAccount *genesisthor.Account
	for _, account := range accounts {
		accountAddr := account.Address.String()
		if accountAddr == paramsAddress {
			paramsAccount = &account
		}
	}

	if paramsAccount != nil {
		if err := processParamsConfig(config, paramsAccount.Storage); err != nil {
			return nil, fmt.Errorf("failed to process params config: %w", err)
		}
	}

	// From the config field
	config.BlockInterval = genesis.Config.BlockInterval
	config.EpochLength = genesis.Config.EpochLength
	config.MinStakingPeriod = genesis.Config.LowStakingPeriod
	config.MidStakingPeriod = genesis.Config.MediumStakingPeriod
	config.HighStakingPeriod = genesis.Config.HighStakingPeriod
	config.CooldownPeriod = genesis.Config.CooldownPeriod

	return config, nil
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
func initializeSyntheticActivity(stack *stack.Stack, engine *lifecycle.Engine, generator *devnetGenerator, validationsState *validators.Service, initialValidators map[thor.Address]*hayabusa.NodePair, delegations *delegations.PositionManager) error {
	// Create initial validators with different strategies
	validatorCount := 0
	totalValidators := len(initialValidators)

	for _, nodePair := range initialValidators {
		config := generator.CreateValidator(nodePair, 0)

		// Distribute staking strategies based on available validators
		if validatorCount < totalValidators/3 { // First third: long-term validators
			config.StakingPeriods = uint32(utils.RandomBetween(200, 500))
		} else if validatorCount < (totalValidators*2)/3 { // Second third: medium-term validators
			config.StakingPeriods = uint32(utils.RandomBetween(50, 150))
		} else { // Last third: short-term validators
			config.StakingPeriods = uint32(utils.RandomBetween(10, 40))
		}

		cycle := lifecycle.NewValidatorLifecycle(config, validationsState, delegations, stack)
		engine.AddLifecycle(cycle)
		validatorCount++
	}

	slog.Info("initialized synthetic activity",
		"validatorCount", validatorCount,
		"totalValidators", totalValidators)

	return nil
}
