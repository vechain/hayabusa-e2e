package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/delegations"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	utils2 "github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/validators"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/networkhub/thorbuilder"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func startAgainstNetworkHub(ctx context.Context) (*lifecycle.Engine, func()) {
	config := &hayabusa.Config{
		Nodes:             *networkHubNodes,
		MaxBlockProposers: uint32(*networkHubNodes),
		ForkBlock:         0,
		TransitionPeriod:  6,
		EpochLength:       6,
		CooldownPeriod:    6,
		MinStakingPeriod:  6,
		MidStakingPeriod:  12,
		HighStakingPeriod: 24,
		BlockInterval:     5,
	}
	if *networkHubManyKeyNode {
		config.MaxBlockProposers = 101
	}
	network, err := hayabusa.NewNetwork(config, ctx)
	if err != nil {
		slog.Error("failed to create hayabusa network", "error", err)
		os.Exit(1)
	}

	slog.SetLogLoggerLevel(slog.LevelInfo)

	if *networkHubManyKeyNode {
		if err := addManyKeyNode(network); err != nil {
			slog.Error("failed to add many key node", "error", err)
			os.Exit(1)
		}
	}

	port := 8569
	for i, node := range network.NodeConfigs() {
		if i == 0 {
			node.AddAdditionalArg("enable-metrics", "true")
		}
		addr := net.JoinHostPort("localhost", strconv.Itoa(port))
		port++
		node.SetAPIAddr(addr)
		node.AddAdditionalArg("txpool-limit-per-account", "10000")
		slog.Info("node API address", "node", node.GetID(), "address", addr)
	}
	if err := network.Start(); err != nil {
		slog.Error("failed to start network", "error", err)
		os.Exit(1)
	}
	client := network.ThorClient()
	staker, err := builtin.NewStaker(client)
	if err != nil {
		slog.Error("failed to create staker client", "error", err)
		os.Exit(1)
	}

	initialValidators := hayabusa.ValidatorAccounts[0:90]
	extraValidators := make([]*hayabusa.NodePair, 0)
	extraValidators = append(extraValidators, hayabusa.ValidatorAccounts[90:100]...)
	for _, acc := range hayabusa.AdditionalAccounts {
		signer := hayabusa.NodePair{
			Endorser: acc,
			Node:     acc,
		}
		extraValidators = append(extraValidators, &signer)
	}

	stack := stack.NewStack(ctx, staker, config)
	positions := delegations.NoPositions
	if *delegationsEnabled {
		positions = delegations.MainnetPositions
	}
	delegations := delegations.NewManager(config.MaxBlockProposers, delegations.DistributionTypeEven, positions)
	validators := validators.NewState(stack)
	generator := &networkHubGenerator{config: config, delegations: delegations, stargate: hayabusa.Stargate, validators: extraValidators}
	engine := lifecycle.NewEngine(stack, validators, delegations, generator)

	utils.WaitForFork(staker, config.ForkBlock)

	// initial seeding of validator accounts
	for i, acc := range initialValidators {
		config := lifecycle.ValidatorConfig{
			Config: lifecycle.Config{
				WithdrawDelay: lifecycle.Delay{
					Blocks: uint32(utils2.RandomBetween(0, int(config.EpochLength))),
					Epochs: uint32(utils2.RandomBetween(1, 3)),
				},
				StartBlock: 0,
			},
			Account:             acc,
			StakeChangeInterval: uint32(utils2.RandomBetween(5, 20)),
		}
		if i < 50 { // create 50 long term validators
			config.StakingPeriods = 5000
		} else if i < 70 { // create 20 mid-term validators
			config.StakingPeriods = uint32(utils2.RandomBetween(30, 100)) // create 20 mid term validators
		} else {
			config.StakingPeriods = uint32(utils2.RandomBetween(6, 12)) // create 20 short term validators
		}
		config.QueueDelay = lifecycle.Delay{Blocks: 0, Epochs: 0}
		cycle := lifecycle.NewValidatorLifecycle(config, validators, delegations, stack)
		engine.AddLifecycle(cycle)
	}

	if err := engine.Flush(lifecycle.StatusActive); err != nil {
		slog.Error("failed to flush validator lifecycles", "error", err)
		os.Exit(1)
	}

	slog.Info("✅ validator lifecycles flushed")

	if err := utils.WaitForPOS(staker, config.ForkBlock+config.TransitionPeriod); err != nil {
		slog.Error("failed to wait for POS", "error", err)
		os.Exit(1)
	}

	return engine, network.Stop
}

type networkHubGenerator struct {
	config         *hayabusa.Config
	delegations    *delegations.PositionManager
	stargate       *bind.PrivateKeySigner
	validators     []*hayabusa.NodePair
	validatorIndex int
}

func (n *networkHubGenerator) CreateValidator(startBlock uint32) (lifecycle.ValidatorConfig, bool) {
	if n.validatorIndex >= len(n.validators) {
		return lifecycle.ValidatorConfig{}, false
	}
	acc := n.validators[n.validatorIndex]
	n.validatorIndex++

	return lifecycle.ValidatorConfig{
		Config: lifecycle.Config{
			QueueDelay: lifecycle.Delay{
				Blocks: uint32(utils2.RandomBetween(0, int(n.config.EpochLength))),
				Epochs: uint32(utils2.RandomBetween(0, 3)),
			},

			StakingPeriods: uint32(utils2.RandomBetween(5, 100)),
			WithdrawDelay: lifecycle.Delay{
				Blocks: uint32(utils2.RandomBetween(0, int(n.config.EpochLength))),
				Epochs: uint32(utils2.RandomBetween(1, 3)),
			},
			StartBlock: startBlock,
		},
		Account:             acc,
		StakeChangeInterval: uint32(utils2.RandomBetween(5, 20)),
	}, true
}

func (n *networkHubGenerator) CreateDelegator(startBlock uint32) (lifecycle.DelegatorConfig, bool) {
	id, pos, ok := n.delegations.NewPosition()
	if !ok {
		return lifecycle.DelegatorConfig{}, false
	}

	return lifecycle.DelegatorConfig{
		Config: lifecycle.Config{
			QueueDelay: lifecycle.Delay{
				Blocks: uint32(utils2.RandomBetween(0, int(n.config.EpochLength))),
				Epochs: uint32(utils2.RandomBetween(0, 3)),
			},
			StakingPeriods: uint32(utils2.RandomBetween(3, 120)),
			WithdrawDelay: lifecycle.Delay{
				Blocks: uint32(utils2.RandomBetween(0, int(n.config.EpochLength))),
				Epochs: uint32(utils2.RandomBetween(5, 10)),
			},
			StartBlock: startBlock,
		},
		Account:      n.stargate,
		Position:     pos.Position,
		ValidationID: pos.Validator,
		PositionID:   id,
	}, true
}

func addManyKeyNode(network *hayabusa.Network) error {
	args := make(map[string]string)
	keys := ""
	for i := 2; i < 101; i++ {
		hex := hexutil.Encode(hayabusa.ValidatorAccounts[i].Node.D.Bytes())
		hex = strings.TrimPrefix(hex, "0x")
		keys += hex + ","
	}
	for _, acc := range hayabusa.AdditionalAccounts {
		hex := hexutil.Encode(acc.D.Bytes())
		hex = strings.TrimPrefix(hex, "0x")
		keys += hex + ","
	}
	keys = strings.TrimSuffix(keys, ",")
	args["keys"] = keys
	config := &thorbuilder.Config{
		DownloadConfig: &thorbuilder.DownloadConfig{
			RepoUrl:    "git@github.com:vechain/hayabusa.git",
			Branch:     "darren/testing/multiple-keys",
			IsReusable: true,
		},
	}
	return network.AttachNode(config, args)
}
