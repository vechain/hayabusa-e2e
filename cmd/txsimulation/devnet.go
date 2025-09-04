package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/big"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/contract"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/delegations"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/devnetcleanup"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/validators"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/xnodes"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	utils2 "github.com/vechain/hayabusa-e2e/utils"
	protocolbuiltin "github.com/vechain/thor/v2/builtin"
	genesisthor "github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
	"github.com/vechain/thor/v2/thorclient/httpclient"
)

func startAgainstDevnet(ctx context.Context) (*lifecycle.Engine, func()) {
	client := thorclient.New(*devnetFlag)

	staker, err := builtin.NewStaker(client)
	if err != nil {
		slog.Error("failed to create staker client", "error", err)
		os.Exit(1)
	}
	config, err := leadNetworkConfig(*devnetGenesisFlag)
	if err != nil {
		slog.Error("failed to load Hayabusa genesis config", "error", err)
		os.Exit(1)
	}
	keys, err := loadDevnetKeys(*devnetKeysDir)
	if err != nil {
		slog.Error("failed to load devnet keys", "error", err)
		os.Exit(1)
	}
	slog.Info("🔑 created devnet keys",
		"executors", len(keys.Executors),
		"authorities", len(keys.Authorities),
		"endorsors", len(keys.Endorsors),
		"rotatingValidators", len(keys.RotatingValidators))

	stargate := keys.FaucetKeys[len(keys.FaucetKeys)-1]
	if *delegationsEnabled {
		// set stargate in params if not already set
		if err = setStargate(ctx, client, keys.Executors, stargate.Address()); err != nil {
			slog.Error("failed to set stargate", "error", err)
			os.Exit(1)
		}
		slog.Info("✅  stargate set to", "address", stargate.Address())
	}

	stack := stack.NewStack(ctx, staker, config)
	contractService := contract.NewState(stack)
	positions := xnodes.NoPositions
	if *delegationsEnabled {
		positions = xnodes.DevnetPositions(config.MaxBlockProposers)
	}
	xnodes := xnodes.NewManager(config.MaxBlockProposers, xnodes.DistributionTypeEven, positions)
	generator := &devnetGenerator{
		stargate:        stargate,
		validators:      keys.RotatingValidators,
		contractService: contractService,
		xnodes:          xnodes,
		stack:           stack,
	}
	engine := lifecycle.NewEngine(stack, xnodes, generator)

	// initial seeding of authority accounts
	authorityConfigs := createAuthorityConfigs(keys, config)
	for _, cfg := range authorityConfigs {
		engine.AddLifecycle(validators.NewValidatorLifecycle(cfg, contractService, xnodes, stack, config.MinStakingPeriod))
	}
	if err := engine.Flush(lifecycle.StatusQueued); err != nil {
		slog.Error("failed to flush initial authority validators", "error", err)
		os.Exit(1)
	}
	best, err := client.Block("best")
	if err != nil {
		slog.Error("failed to get best block", "error", err)
		os.Exit(1)
	}
	block := best.Number + config.TransitionPeriod*4
	slog.Info("🕰️  waiting for dPoS to become active", "expected-by", block)
	if err := utils2.WaitForPOS(ctx, staker, block); err != nil {
		slog.Error("failed to wait for PoS", "error", err)
		os.Exit(1)
	}
	slog.Info("✅  dPoS is now active, starting devnet simulation")

	cleaner := devnetcleanup.New(stack, contractService, stargate)
	// cleanup old delegation positions from previous runs
	if err := cleaner.Run(*delegationsEnabled); err != nil {
		slog.Error("failed to run initial cleanup", "error", err)
	}

	return engine, func() {
		if !*delegationsEnabled {
			return
		}
		// cleanup current xnodes for future runs, wait for a while to let pending txs be mined
		delay := 30 * time.Second
		slog.Info("🧹 running final cleanup...", "delay", delay)
		time.Sleep(delay)
		if err := cleaner.Run(true); err != nil {
			slog.Error("failed to run final cleanup", "error", err)
		}
	}
}

// loadHayabusaGenesis loads Hayabusa genesis configuration
func leadNetworkConfig(genesisURL string) (*hayabusa.Config, error) {
	resp, err := http.Get(genesisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch genesis.json: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read genesis.json: %w", err)
	}
	println(string(body))

	var genesis genesisthor.CustomGenesis
	if err := json.Unmarshal(body, &genesis); err != nil {
		return nil, fmt.Errorf("failed to parse genesis.json: %w", err)
	}

	config, err := extractConfigFromAccounts(genesis.Accounts, &genesis)
	if err != nil {
		return nil, fmt.Errorf("failed to extract config from accounts: %w", err)
	}

	slog.Info("loaded Hayabusa genesis configuration",
		"config", *config)

	return config, nil
}

func setStargate(ctx context.Context, client *thorclient.Client, executors []*bind.PrivateKeySigner, stargate thor.Address) error {
	// init contracts
	params, err := builtin.NewParams(client)
	if err != nil {
		return fmt.Errorf("failed to create params client: %w", err)
	}
	executor, err := builtin.NewExecutor(client)
	if err != nil {
		return fmt.Errorf("failed to create executor client: %w", err)
	}

	// check existing
	stargateKey := thor.BytesToBytes32(thor.KeyDelegatorContractAddress.Bytes())
	stargateBigInt, err := params.Get(stargateKey)
	if err != nil {
		slog.Error("failed to get stargate address from params", "error", err)
		return err
	}
	currentAddress := thor.BytesToAddress(stargateBigInt.Bytes())
	if currentAddress == stargate {
		slog.Info("✅  stargate address already set in params", "address", stargate)
		return nil
	}

	// create executor proposal
	stargateBigInt = new(big.Int).SetBytes(stargate.Bytes())
	setClause, err := params.Set(stargateKey, stargateBigInt).Clause()
	if err != nil {
		return fmt.Errorf("failed to create set stargate clause: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	slog.Info("🧑‍💻 proposing set stargate address in params", "address", stargate)
	receipt, _, err := executor.Propose(*params.Raw().Address(), setClause.Data()).
		Send().
		WithSigner(executors[0]).
		SubmitAndConfirm(ctx)
	if err != nil {
		return fmt.Errorf("failed to propose set stargate transaction: %w", err)
	}
	if receipt.Reverted {
		return fmt.Errorf("set stargate transaction reverted: %s", receipt.Meta.TxID)
	}
	proposalID := receipt.Outputs[0].Events[0].Topics[1]

	// approve by other executors
	slog.Info("🕵️‍♀️ approving set stargate address in params", "address", stargate)
	txIDs := make([]thor.Bytes32, 0, len(executors))
	for _, executorKey := range executors {
		tx, err := executor.Approve(proposalID).Send().WithSigner(executorKey).Submit()
		if err != nil {
			return fmt.Errorf("failed to approve set stargate transaction: %w", err)
		}
		txIDs = append(txIDs, tx.ID())
	}

	// wait for the receipts
	ticker := utils2.NewTicker(client)
	approved := make(map[thor.Address]bool)
	for range 6 {
		if len(approved) >= len(executors) {
			break
		}
		ticker.Wait(20 * time.Second)
		for i, txID := range txIDs {
			if _, ok := approved[executors[i].Address()]; ok {
				continue
			}
			receipt, err := client.TransactionReceipt(&txID)
			if err != nil {
				if errors.Is(err, httpclient.ErrNotFound) {
					continue
				}
				return fmt.Errorf("failed to get receipt for approve tx %s: %w", txID, err)
			}
			if receipt.Reverted {
				return fmt.Errorf("approve tx %s reverted", txID)
			}
			approved[executors[i].Address()] = true
		}
	}

	if len(approved) < len(executors) {
		return fmt.Errorf("not all executors approved the proposal, approved: %d, total: %d", len(approved), len(executors))
	}

	// execute the proposal
	slog.Info("🧑‍⚖️ executing set stargate address in params", "address", stargate)
	executeCtx, executeCancel := context.WithTimeout(ctx, time.Minute)
	defer executeCancel()
	receipt, _, err = executor.Execute(proposalID).Send().WithSigner(executors[0]).SubmitAndConfirm(executeCtx)
	if err != nil {
		return fmt.Errorf("failed to execute set stargate transaction: %w", err)
	}
	if receipt.Reverted {
		return fmt.Errorf("execute set stargate transaction reverted: %s", receipt.Meta.TxID)
	}
	slog.Info("set stargate address in params", "address", stargate, "tx", receipt.Meta.TxID)
	return nil
}

func extractConfigFromAccounts(accounts []genesisthor.Account, genesis *genesisthor.CustomGenesis) (*hayabusa.Config, error) {
	config := &hayabusa.Config{
		Nodes:            1,
		ForkBlock:        genesis.ForkConfig.HAYABUSA,
		TransitionPeriod: *genesis.Config.HayabusaTP,
	}

	var paramsAccount *genesisthor.Account
	for _, account := range accounts {
		accountAddr := account.Address
		if accountAddr == protocolbuiltin.Params.Address {
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
	stargate        *bind.PrivateKeySigner
	validators      []*bind.PrivateKeySigner
	contractService *contract.Service
	validatorIndex  int
	xnodes          *xnodes.PositionManager
	stack           *stack.Stack
}

func (g *devnetGenerator) CreateValidator(startBlock uint32) (lifecycle.Lifecycle, bool) {
	// Create validators with different staking strategies
	stakingStrategy := utils.RandomBetween(1, 4)
	var stakingPeriods uint32
	var queueDelay lifecycle.Delay

	if g.validatorIndex >= len(g.validators) {
		return nil, false
	}
	acc := &hayabusa.NodePair{
		Node:     g.validators[g.validatorIndex],
		Endorser: g.validators[g.validatorIndex],
	}
	g.validatorIndex++

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

	config := validators.Config{
		Config: lifecycle.Config{
			QueueDelay:     queueDelay,
			StartBlock:     startBlock,
			StakingPeriods: stakingPeriods,
			WithdrawDelay:  lifecycle.Delay{Blocks: uint32(utils.RandomBetween(1, 10)), Epochs: 0},
		},
		Account:             acc,
		StakeChangeInterval: uint32(utils.RandomBetween(10, 30)),
	}

	stakingPeriod := g.stack.Config().MinStakingPeriod
	stakingPeriodStrategy := utils.RandomBetween(0, 50)
	if stakingPeriodStrategy > 47 { // rarely want a high staking period, otherwise entire network eventually locks into high staking periods
		stakingPeriod = g.stack.Config().HighStakingPeriod
	} else if stakingPeriodStrategy > 40 { // medium staking period, similar logic to above
		stakingPeriod = g.stack.Config().MidStakingPeriod
	}

	return validators.NewValidatorLifecycle(config, g.contractService, g.xnodes, g.stack, stakingPeriod), true
}

func (g *devnetGenerator) CreateDelegator(startBlock uint32) (lifecycle.Lifecycle, bool) {
	id, pos, ok := g.xnodes.NewPosition()
	if !ok {
		return nil, false
	}
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
	withdrawDelay := lifecycle.Delay{
		Blocks: uint32(utils.RandomBetween(5, 15)),
		Epochs: uint32(utils.RandomBetween(1, 3)),
	}

	config := delegations.Config{
		Config: lifecycle.Config{
			QueueDelay:     queueDelay,
			StartBlock:     startBlock,
			StakingPeriods: stakingPeriods,
			WithdrawDelay:  withdrawDelay,
		},
		Account:      g.stargate,
		ValidationID: pos.Validator,
		Position:     pos.Position,
		PositionID:   id,
	}
	return delegations.NewDelegatorLifecycle(config, g.xnodes, g.contractService, g.stack), true
}

type DevnetKeys struct {
	Executors          []*bind.PrivateKeySigner
	Authorities        []*bind.PrivateKeySigner
	Endorsors          []*bind.PrivateKeySigner
	RotatingValidators []*bind.PrivateKeySigner
	FaucetKeys         []*bind.PrivateKeySigner
}

type KeyEntry struct {
	Key     string `json:"key"`
	Address string `json:"address"`
}

func loadDevnetKeys(dir string) (*DevnetKeys, error) {
	loadFile := func(fileName string) ([]*bind.PrivateKeySigner, error) {
		filePath := path.Join(dir, fileName)
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s file (path=%s): %w", fileName, filePath, err)
		}
		var entries []KeyEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			return nil, fmt.Errorf("failed to parse %s JSON: %w", fileName, err)
		}
		var signers []*bind.PrivateKeySigner
		for _, entry := range entries {
			key, err := crypto.HexToECDSA(strings.TrimPrefix(entry.Key, "0x"))
			if err != nil {
				return nil, fmt.Errorf("failed to parse key %s in %s: %w", entry.Key, fileName, err)
			}
			signer := bind.NewSigner(key)
			signers = append(signers, signer)
		}
		return signers, nil
	}
	authorities, err := loadFile("authority-keys.json")
	if err != nil {
		return nil, err
	}
	endorsors, err := loadFile("endorsor-keys.json")
	if err != nil {
		return nil, err
	}
	if len(authorities) != len(endorsors) {
		return nil, fmt.Errorf("mismatched authorities and endorsors count: %d vs %d", len(authorities), len(endorsors))
	}
	executors, err := loadFile("executor-keys.json")
	if err != nil {
		return nil, err
	}
	rotatingValidators, err := loadFile("rotating-validators-keys.json")
	if err != nil {
		return nil, err
	}
	faucetKeys, err := loadFile("faucet-keys.json")
	if err != nil {
		return nil, err
	}

	return &DevnetKeys{
		Executors:          executors,
		Authorities:        authorities,
		Endorsors:          endorsors,
		RotatingValidators: rotatingValidators,
		FaucetKeys:         faucetKeys,
	}, nil
}

func createAuthorityConfigs(keys *DevnetKeys, config *hayabusa.Config) []validators.Config {
	configs := make([]validators.Config, len(keys.Authorities))
	slog.Info("creating authority configs",
		"count", len(keys.Authorities),
		"cooldownPeriod", config.CooldownPeriod,
		"minStakingPeriod", config.MinStakingPeriod,
		"devnetLongTermValidators", *devnetLongTermValidators)
	for i, key := range keys.Authorities {
		cfg := validators.Config{
			Config: lifecycle.Config{
				WithdrawDelay: lifecycle.Delay{
					Blocks: config.CooldownPeriod,
					Epochs: 5,
				},
				StartBlock:     0,
				StakingPeriods: 1,
				QueueDelay:     lifecycle.Delay{},
			},
			Account:             &hayabusa.NodePair{Endorser: keys.Endorsors[i], Node: key},
			StakeChangeInterval: 8,
		}
		if i < *devnetLongTermValidators { // we keep a certain amount of long term validators to ensure stability in the network
			slog.Info("keeping long term validator", "address", key.Address())
			cfg.StakingPeriods = math.MaxUint32
		}
		configs[i] = cfg
	}
	return configs
}
