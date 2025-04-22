package hayabusa

import (
	_ "embed"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/vechain/draupnir/genesisbuilder"
	"github.com/vechain/draupnir/network"
	"github.com/vechain/hayabusa-e2e/builtins"
	networkhubNetwork "github.com/vechain/networkhub/network"
	"github.com/vechain/networkhub/network/node"
	"github.com/vechain/networkhub/network/node/genesis"
	"github.com/vechain/thor/v2/builtin"
	devgenesis "github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/runtime"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
)

type Config struct {
	Nodes             int    // The number of nodes to create
	MaxBlockProposers uint32 // The number of max block proposers
	ForkBlock         uint32 // ForkConfig.HAYABUSA
	TransitionPeriod  uint32 // ForkConfig.HAYABUSA_TP
	EpochLength       uint32 // epoch-length
	CooldownPeriod    uint32 // cooldown-period
	MinStakingPeriod  uint32 // staker-low-staking-period
	MidStakingPeriod  uint32 // staker-medium-staking-period
	HighStakingPeriod uint32 // staker-high-staking-period
}

func NewConfig() *Config {
	return &Config{
		Nodes:             3,
		MaxBlockProposers: 3,
		ForkBlock:         6,
		TransitionPeriod:  6,
		EpochLength:       6,
		CooldownPeriod:    3,
		MinStakingPeriod:  6,
		MidStakingPeriod:  30,
		HighStakingPeriod: 180,
	}
}

// Apply the configuration to the genesis file.
func (h Config) Apply(genesis *genesis.CustomGenesis) {
	genesis.LaunchTime = uint64(time.Now().Unix())
	genesis.ForkConfig.AddField("HAYABUSA", h.ForkBlock)
	genesis.ForkConfig.AddField("HAYABUSA_TP", h.TransitionPeriod)

	// staker config - set all values
	stakerIndex := slices.IndexFunc(genesis.Accounts, func(acc devgenesis.Account) bool {
		return acc.Address == builtins.StakerAddress
	})
	if stakerIndex == -1 {
		genesis.Accounts = append(genesis.Accounts, devgenesis.Account{
			Address: builtins.StakerAddress,
			Storage: map[string]thor.Bytes32{},
			Code:    hexutil.Encode(runtime.EmptyRuntimeBytecode),
			Balance: (*devgenesis.HexOrDecimal256)(big.NewInt(0)),
			Energy:  (*devgenesis.HexOrDecimal256)(big.NewInt(0)),
		})
		stakerIndex = len(genesis.Accounts) - 1
	}
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("epoch-length")] = uint32ToBytes32(h.EpochLength, 6)
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("cooldown-period")] = uint32ToBytes32(h.CooldownPeriod, 6)
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("staker-low-staking-period")] = uint32ToBytes32(h.MinStakingPeriod, 6)
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("staker-medium-staking-period")] = uint32ToBytes32(h.MidStakingPeriod, 30)
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("staker-high-staking-period")] = uint32ToBytes32(h.HighStakingPeriod, 180)

	// params config - set max-block-proposers
	paramsIndex := slices.IndexFunc(genesis.Accounts, func(acc devgenesis.Account) bool {
		return acc.Address == builtin.Params.Address
	})
	if paramsIndex == -1 {
		genesis.Accounts = append(genesis.Accounts, devgenesis.Account{
			Address: builtin.Params.Address,
			Storage: map[string]thor.Bytes32{},
			Code:    "",
			Balance: (*devgenesis.HexOrDecimal256)(big.NewInt(0)),
			Energy:  (*devgenesis.HexOrDecimal256)(big.NewInt(0)),
		})
		paramsIndex = len(genesis.Accounts) - 1
	}
	genesis.Accounts[paramsIndex].Storage[nameToBytes32("max-block-proposers")] = uint32ToBytes32(h.MaxBlockProposers, 3)

}

func StartNetwork(config *Config) (*thorclient.Client, func(), error) {
	if config.Nodes < 2 {
		return nil, nil, fmt.Errorf("at least 2 nodes are required")
	}
	customGenesis := genesisbuilder.New(int(config.MaxBlockProposers)).
		Overrider(config.Apply).
		Build()

	repo := "git@github.com:vechain/hayabusa.git"
	net := network.NewCustomNetworkWithBranchAndRepo(repo, "release/hayabusa")
	workingDir, ok := os.LookupEnv("THOR_WORKING_DIR")
	if ok {
		net = network.NewCustomWithRepoAndDownloadPath(repo, workingDir)
	}

	nodes := make([]node.Node, config.Nodes)
	for i := range config.Nodes {
		generatedNode := &node.BaseNode{
			ID:        "node" + strconv.Itoa(i),
			Key:       common.Bytes2Hex(devgenesis.DevAccounts()[i].PrivateKey.D.Bytes()),
			Genesis:   customGenesis,
			Verbosity: 3,
		}
		nodes[i] = generatedNode
	}
	networkCfg := &networkhubNetwork.Network{
		Environment: "local",
		ID:          "test-id",
		Nodes:       nodes,
	}
	if err := net.StartWithNetwork(networkCfg); err != nil {
		net.Stop()
		return nil, nil, fmt.Errorf("failed to start network: %w", err)
	}

	client := thorclient.New(net.Details().Address)

	return client, func() {
		if err := net.Stop(); err != nil {
			slog.Error("failed to stop network", "error", err)
		}
	}, nil
}

// uint32ToBytes32 converts a uint32 value to a Bytes32 value.
// If the value is 0, it returns the default value.
func uint32ToBytes32(value uint32, defaultValue uint32) thor.Bytes32 {
	var bigValue *big.Int
	if value == 0 {
		bigValue = big.NewInt(0).SetUint64(uint64(defaultValue))
	} else {
		bigValue = big.NewInt(0).SetUint64(uint64(value))
	}
	return thor.Bytes32(common.BigToHash(bigValue))
}

func nameToBytes32(name string) string {
	return thor.BytesToBytes32([]byte(name)).String()
}
