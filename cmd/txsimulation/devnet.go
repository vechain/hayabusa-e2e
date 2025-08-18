package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/validations"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

const (
	stakerAddress = "0x00000000000000000000000000005374616b6572"
)

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

	stack := stack.NewStack(ctx, staker, config, validators, hayabusa.Stargate)
	validationsState := validations.NewState(stack)
	generator := &devnetGenerator{
		config: config,
		stack:  stack,
	}
	engine := lifecycle.NewEngine(stack, validationsState, generator)
	initializeSyntheticActivity(engine, generator, genesis)

	stop := func() {
		slog.Info("stopping Hayabusa devnet simulation")
	}

	return engine, stop
}

// HayabusaGenesis represents the structure of the Hayabusa genesis.json
type HayabusaGenesis struct {
	GasLimit   string `json:"gasLimit"`
	ExtraData  string `json:"extraData"`
	ForkConfig struct {
		VIP191      int `json:"VIP191"`
		ETH_CONST   int `json:"ETH_CONST"`
		BLOCKLIST   int `json:"BLOCKLIST"`
		ETH_IST     int `json:"ETH_IST"`
		VIP214      int `json:"VIP214"`
		FINALITY    int `json:"FINALITY"`
		GALACTICA   int `json:"GALACTICA"`
		HAYABUSA    int `json:"HAYABUSA"`
		HAYABUSA_TP int `json:"HAYABUSA_TP"`
	} `json:"forkConfig"`
	Accounts []struct {
		Address string            `json:"address"`
		Balance string            `json:"balance"`
		Energy  string            `json:"energy"`
		Code    string            `json:"code,omitempty"`
		Storage map[string]string `json:"storage,omitempty"`
	} `json:"accounts"`
	Authority []struct {
		MasterAddress   string `json:"masterAddress"`
		EndorsorAddress string `json:"endorsorAddress"`
		Identity        string `json:"identity"`
	} `json:"authority"`
	Executor struct {
		Approvers []struct {
			Address  string `json:"address"`
			Identity string `json:"identity"`
		} `json:"approvers"`
	} `json:"executor"`
	Params struct {
		ExecutorAddress     string `json:"executorAddress"`
		BaseGasPrice        string `json:"baseGasPrice"`
		RewardRatio         string `json:"rewardRatio"`
		ProposerEndorsement string `json:"proposerEndorsement"`
	} `json:"params"`
	LaunchTime int64 `json:"launchTime"`
}

// loadHayabusaGenesis loads Hayabusa genesis configuration
func loadHayabusaGenesis(genesisURL string) (*HayabusaGenesis, *hayabusa.Config, error) {
	resp, err := http.Get(genesisURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch genesis.json: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read genesis.json: %w", err)
	}

	var genesis HayabusaGenesis
	if err := json.Unmarshal(body, &genesis); err != nil {
		return nil, nil, fmt.Errorf("failed to parse genesis.json: %w", err)
	}

	var (
		lowStakingPeriod    uint32
		mediumStakingPeriod uint32
		highStakingPeriod   uint32
		cooldownPeriod      uint32
		epochLength         uint32
	)
	for _, account := range genesis.Accounts {
		if account.Address == stakerAddress {
			for storageKey, storageValue0x := range account.Storage {
				storageKeyBytes, err := hex.DecodeString(storageKey)
				if err != nil {
					return nil, nil, fmt.Errorf("failed to decode storage key: %w", err)
				}
				storageValueStr := strings.TrimPrefix(storageValue0x, "0x")
				storageValueUint64, err := strconv.ParseUint(storageValueStr, 16, 32)
				if err != nil {
					slog.Error("failed to parse energy", "error", err)
					return nil, nil, fmt.Errorf("failed to decode storage value: %w", err)
				}
				storageValue := uint32(storageValueUint64)
				stakerParamName := thor.BytesToBytes32(storageKeyBytes).String()
				switch stakerParamName {
				case "staker-low-staking-period":
					lowStakingPeriod = storageValue
				case "staker-medium-staking-period":
					mediumStakingPeriod = storageValue
				case "staker-high-staking-period":
					highStakingPeriod = storageValue
				case "cooldown-period":
					cooldownPeriod = storageValue
				case "epoch-length":
					epochLength = storageValue
				}
			}
			break
		}
	}

	// Create config based on parsed genesis.json
	// Values not directly related are coming from the networkhub config in terms of proportions
	config := &hayabusa.Config{
		Nodes:             1, // Single node in devnet
		MaxBlockProposers: uint32(len(genesis.Accounts)),
		ForkBlock:         uint32(genesis.ForkConfig.HAYABUSA),
		TransitionPeriod:  uint32(genesis.ForkConfig.HAYABUSA_TP),
		EpochLength:       epochLength,
		CooldownPeriod:    cooldownPeriod,
		MinStakingPeriod:  lowStakingPeriod,
		MidStakingPeriod:  mediumStakingPeriod,
		HighStakingPeriod: highStakingPeriod,
	}

	slog.Info("loaded Hayabusa genesis configuration",
		"config", config)

	return &genesis, config, nil
}

// Load validators from Hayabusa genesis.json
func loadHayabusaValidators(genesis *HayabusaGenesis) (map[thor.Address]*hayabusa.NodePair, error) {
	// Create validators from authority section
	validators := make(map[thor.Address]*hayabusa.NodePair)

	validatorPrivateKeysStr := os.Getenv("VALIDATOR_PRIVATE_KEYS")
	if validatorPrivateKeysStr == "" {
		return nil, fmt.Errorf("VALIDATOR_PRIVATE_KEYS environment variable is required")
	}
	validatorPrivateKeys := strings.Split(validatorPrivateKeysStr, ",")

	for i, authority := range genesis.Authority {
		masterAddr := thor.MustParseAddress(authority.MasterAddress)

		// Create endorser signer from the endorsor address
		endorserKey, err := crypto.HexToECDSA(validatorPrivateKeys[i])
		if err != nil {
			slog.Warn("failed to parse endorser identity", "address", authority.MasterAddress, "error", err)
			continue
		}

		endorserSigner := bind.NewSigner(endorserKey)
		endorserAddr := endorserSigner.Address().String()
		if endorserAddr != authority.EndorsorAddress {
			slog.Error("endorser address does not match genesis address", "endorser", endorserAddr, "endorser-genesis", authority.EndorsorAddress)
			return nil, fmt.Errorf("endorser address does not match genesis address %s vs %s", endorserAddr, authority.EndorsorAddress)
		}

		//TODO: These 2 lines are wrong, the first one creates a new key when it should not
		nodePair := hayabusa.MustCreateNodePair(endorserSigner)
		validators[masterAddr] = nodePair
	}

	slog.Info("loaded Hayabusa validators from genesis", "validatorCount", len(validators))

	return validators, nil
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
func initializeSyntheticActivity(engine *lifecycle.Engine, generator *devnetGenerator, genesis *HayabusaGenesis) {
	// Load validators from Hayabusa genesis.json
	validators, err := loadHayabusaValidators(genesis)
	if err != nil {
		slog.Error("failed to load validators for synthetic activity", "error", err)
		return
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

	// Create initial delegators using additional accounts
	initialDelegators := hayabusa.AdditionalAccounts[0:15] // Use first 15 delegators
	for i, acc := range initialDelegators {
		config := generator.CreateDelegator(acc, 0)

		// Distribute delegation strategies
		if i < 5 { // 5 long-term delegators
			config.StakingPeriods = uint32(utils.RandomBetween(100, 300))
		} else if i < 10 { // 5 medium-term delegators
			config.StakingPeriods = uint32(utils.RandomBetween(30, 80))
		} else { // 5 short-term delegators
			config.StakingPeriods = uint32(utils.RandomBetween(10, 30))
		}

		cycle := lifecycle.NewDelegatorLifecycle(config)
		engine.AddLifecycle(cycle)
	}

	slog.Info("initialized synthetic activity",
		"validators", validatorCount,
		"delegators", len(initialDelegators))
}
