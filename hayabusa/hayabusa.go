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
	"github.com/vechain/hayabusa-e2e/genesisbuilder"
	"github.com/vechain/hayabusa-e2e/network"
	networkhubNetwork "github.com/vechain/networkhub/network"
	"github.com/vechain/networkhub/network/node"
	"github.com/vechain/networkhub/network/node/genesis"
	thorgenesis "github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/httpclient"
)

var (
	ValidatorAccounts  = mustParseKeys(validatorKeys)
	Stargate           = mustParseKey("274c9caa1b72003d86eab9ea817f9b4c172246e75a9e20d1baaf44bbf5c89762")
	ParamsStargateKey  = nameToBytes32("stargate-contract-address")
	Executor           = (*bind.PrivateKeySigner)(thorgenesis.DevAccounts()[0].PrivateKey)
	AdditionalAccounts = mustParseKeys(additionalKeys)
)

func Genesis(config *Config) *genesis.CustomGenesis {
	executor := Executor.Address()
	return genesisbuilder.New(int(config.MaxBlockProposers)).
		Overrider(config.Apply).
		Accounts(genesisAccounts()).
		Authority(authorities()).
		Executor(thorgenesis.Executor{
			Approvers: make([]thorgenesis.Approver, 0),
		}).
		Params(
			thorgenesis.Params{
				ExecutorAddress: &executor,
			},
		).
		Build()
}

func StartNetwork(config *Config) (*httpclient.Client, *network.CustomNetwork, func(), error) {
	if config.Nodes < 2 {
		return nil, nil, nil, fmt.Errorf("at least 2 nodes are required")
	}
	repo := "git@github.com:vechain/thor.git"
	var net *network.CustomNetwork
	workingDir, ok := os.LookupEnv("THOR_WORKING_DIR")
	if ok {
		net = network.NewCustomWithRepoAndDownloadPath(repo, workingDir, config.Debug)
	} else {
		slog.Warn("THOR_WORKING_DIR not set, using default repo/branch")
		net = network.NewCustomNetworkWithBranchAndRepo(repo, "release/hayabusa")
	}

	verbosity := 3
	if config.Verbosity > 0 {
		verbosity = config.Verbosity
	}

	customGenesis := Genesis(config)
	nodes := make([]node.Config, config.Nodes)
	for i := range config.Nodes {
		additionalArgs := map[string]string{
			"txpool-limit-per-account": "100000",
			"api-allowed-tracers":      "all",
		}
		if i == 0 { // enable verbose staker logs for 1 node
			additionalArgs["verbosity-staker"] = "4"
		}
		nodes[i] = &node.BaseNode{
			ID:             "Node-" + strconv.Itoa(i),
			Key:            common.Bytes2Hex(ValidatorAccounts[i].D.Bytes()),
			Genesis:        customGenesis,
			Verbosity:      verbosity,
			AdditionalArgs: additionalArgs,
		}
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

	// verbose logging for node 0, use node 1 for http (simulation etc.). Amount validated on first line of function
	client := httpclient.New(nodes[1].GetHTTPAddr())

	return client, net, func() {
		if err := net.Stop(); err != nil {
			slog.Error("failed to stop network", "error", err)
		}
	}, nil
}

func authorities() []thorgenesis.Authority {
	authorities := make([]thorgenesis.Authority, 0)

	for _, account := range ValidatorAccounts {
		authorities = append(authorities, thorgenesis.Authority{
			MasterAddress:   account.Address(),
			EndorsorAddress: account.Address(),
			Identity:        datagen.RandomHash(),
		})
	}

	return authorities
}

func genesisAccounts() []thorgenesis.Account {
	accounts := make([]thorgenesis.Account, 0)

	oneEth := big.NewInt(1e18)

	tenBillion := new(big.Int).Mul(oneEth, big.NewInt(10e9))
	hundredBillion := new(big.Int).Mul(oneEth, big.NewInt(100e9))
	oneBillion := new(big.Int).Mul(oneEth, big.NewInt(1e9))

	addAccount := func(account bind.Signer, balance *big.Int) {
		accounts = append(accounts, thorgenesis.Account{
			Address: account.Address(),
			Balance: (*thorgenesis.HexOrDecimal256)(balance),
			Energy:  (*thorgenesis.HexOrDecimal256)(balance),
			Code:    "0x",
			Storage: make(map[string]thor.Bytes32),
		})
	}

	for _, account := range ValidatorAccounts {
		addAccount(account, tenBillion)
	}
	addAccount(Executor, tenBillion)
	for _, account := range AdditionalAccounts {
		addAccount(account, oneBillion)
	}

	addAccount(Stargate, hundredBillion)

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

func mustParseKey(hexKey string) *bind.PrivateKeySigner {
	key, err := crypto.HexToECDSA(hexKey)
	if err != nil {
		panic(fmt.Sprintf("failed to parse key: %v", err))
	}
	return (*bind.PrivateKeySigner)(key)
}

func mustParseKeys(hexKeys []string) []*bind.PrivateKeySigner {
	keys := make([]*bind.PrivateKeySigner, len(hexKeys))

	for i, hexKey := range hexKeys {
		keys[i] = mustParseKey(hexKey)
	}

	return keys
}
