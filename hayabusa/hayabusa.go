package hayabusa

import (
	_ "embed"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/vechain/draupnir/genesisbuilder"
	"github.com/vechain/draupnir/network"
	networkhubNetwork "github.com/vechain/networkhub/network"
	"github.com/vechain/networkhub/network/node"
	thorgenesis "github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
)

var (
	Stargate          = mustGenerateAccount()
	ParamsStargateKey = nameToBytes32("stargate-contract-address")
	ValidatorAccounts = thorgenesis.DevAccounts()
)

func StartNetwork(config *Config) (*thorclient.Client, *network.CustomNetwork, func(), error) {
	if config.Nodes < 2 {
		return nil, nil, nil, fmt.Errorf("at least 2 nodes are required")
	}
	customGenesis := genesisbuilder.New(int(config.MaxBlockProposers)).
		Overrider(config.Apply).
		Accounts(genesisAccounts()).
		Build()

	repo := "git@github.com:vechain/hayabusa.git"
	var net *network.CustomNetwork
	workingDir, ok := os.LookupEnv("THOR_WORKING_DIR")
	if ok {
		net = network.NewCustomWithRepoAndDownloadPath(repo, workingDir)
	} else {
		slog.Warn("THOR_WORKING_DIR not set, using default repo/branch")
		net = network.NewCustomNetworkWithBranchAndRepo(repo, "release/hayabusa")
	}

	verbosity := 3
	if config.Verbosity > 0 {
		verbosity = config.Verbosity
	}

	nodes := make([]node.Node, config.Nodes)
	for i := range config.Nodes {
		generatedNode := &node.BaseNode{
			ID:        "Node-" + strconv.Itoa(i),
			Key:       common.Bytes2Hex(ValidatorAccounts[i].PrivateKey.D.Bytes()),
			Genesis:   customGenesis,
			Verbosity: verbosity,
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
		return nil, nil, nil, fmt.Errorf("failed to start network: %w", err)
	}

	client := thorclient.New(net.Details().Address)

	return client, net, func() {
		if err := net.Stop(); err != nil {
			slog.Error("failed to stop network", "error", err)
		}
	}, nil
}

func genesisAccounts() []thorgenesis.Account {
	accounts := make([]thorgenesis.Account, 0)

	tenBillion := big.NewInt(10e9)
	tenBillion = tenBillion.Mul(tenBillion, big.NewInt(1e18))

	for _, account := range ValidatorAccounts {
		accounts = append(accounts, thorgenesis.Account{
			Address: account.Address,
			Balance: (*thorgenesis.HexOrDecimal256)(tenBillion),
			Energy:  (*thorgenesis.HexOrDecimal256)(tenBillion),
			Code:    "0x",
			Storage: make(map[string]thor.Bytes32),
		})
	}

	accounts = append(accounts, thorgenesis.Account{
		Address: Stargate.Address,
		Balance: (*thorgenesis.HexOrDecimal256)(tenBillion),
		Energy:  (*thorgenesis.HexOrDecimal256)(tenBillion),
		Code:    "0x",
		Storage: make(map[string]thor.Bytes32),
	})

	return accounts
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

func mustGenerateAccount() thorgenesis.DevAccount {
	key, err := crypto.GenerateKey()
	if err != nil {
		panic(fmt.Sprintf("failed to generate key: %v", err))
	}
	address := crypto.PubkeyToAddress(key.PublicKey)
	return thorgenesis.DevAccount{
		Address:    thor.Address(address),
		PrivateKey: key,
	}
}
