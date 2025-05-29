package hayabusa

import (
	"crypto/rand"
	_ "embed"
	"github.com/vechain/networkhub/environments"
	"time"

	"fmt"
	"log/slog"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/vechain/draupnir/datagen"
	"github.com/vechain/networkhub/entrypoint/client"
	"github.com/vechain/networkhub/genesisbuilder"
	"github.com/vechain/networkhub/network/node"
	"github.com/vechain/networkhub/network/node/genesis"
	"github.com/vechain/networkhub/thorbuilder"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"

	networkhubNetwork "github.com/vechain/networkhub/network"
	thorgenesis "github.com/vechain/thor/v2/genesis"
)

var (
	ValidatorAccounts  = mustGenerateAccounts(101)
	Stargate           = mustGenerateAccounts(1)[0]
	ParamsStargateKey  = nameToBytes32("stargate-contract-address")
	Executor           = thorgenesis.DevAccounts()[0] // from genesisbuilder default
	AdditionalAccounts = mustGenerateAccounts(100)
)

func Genesis(config *Config) *genesis.CustomGenesis {
	return genesisbuilder.New(int(config.MaxBlockProposers)).
		Overrider(config.Apply).
		Accounts(genesisAccounts()).
		Authority(authorities()).
		Build()
}

func StartNetwork(config *Config) (*thorclient.Client, environments.Actions, func(), error) {
	if config.Nodes < 2 {
		return nil, nil, nil, fmt.Errorf("at least 2 nodes are required")
	}
	repo := "git@github.com:vechain/hayabusa.git"
	// reimplement this logic
	// workingDir, ok := os.LookupEnv("THOR_WORKING_DIR")
	//if ok {
	//	net = network.NewCustomWithRepoAndDownloadPath(repo, workingDir, config.Debug)
	//} else {
	//	slog.Warn("THOR_WORKING_DIR not set, using default repo/branch")
	//	net = network.NewCustomNetworkWithBranchAndRepo(repo, "release/hayabusa")
	//}
	thorBuilder := &thorbuilder.BuilderConfig{
		RepoUrl:  repo,
		Branch:   "release/hayabusa",
		Reusable: false,
	}

	verbosity := 3
	if config.Verbosity > 0 {
		verbosity = config.Verbosity
	}

	customGenesis := Genesis(config)
	nodes := make([]node.Config, config.Nodes)
	used := make(map[int]bool)
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
			Key:            common.Bytes2Hex(ValidatorAccounts[i].PrivateKey.D.Bytes()),
			Genesis:        customGenesis,
			Verbosity:      verbosity,
			AdditionalArgs: additionalArgs,
			APIAddr:        fmt.Sprintf("127.0.0.1:%d", rndPort(used)),
			P2PListenPort:  rndPort(used),
		}
	}
	networkCfg := &networkhubNetwork.Network{
		Environment: "local",
		BaseID:      "test-id",
		Nodes:       nodes,
		ThorBuilder: thorBuilder,
	}

	networkHub := client.New()
	networkID, err := networkHub.Config(networkCfg)
	if err != nil {
		return nil, nil, nil, err
	}

	hayabusaNetwork, err := networkHub.GetNetwork(networkID.ID())
	if err != nil {
		return nil, nil, nil, err
	}

	err = hayabusaNetwork.StartNetwork()
	if err != nil {
		hayabusaNetwork.StopNetwork()
		return nil, nil, nil, err
	}

	if err = networkCfg.HealthCheck(0, 20*time.Second); err != nil {
		return nil, nil, nil, fmt.Errorf("health check failed: %w", err)
	}

	// verbose logging for node 0, use node 1 for http (simulation etc.). Amount validated on first line of function
	client := thorclient.New(nodes[1].GetHTTPAddr())

	return client, hayabusaNetwork, func() {
		if err := hayabusaNetwork.StopNetwork(); err != nil {
			slog.Error("failed to stop network", "error", err)
		}
	}, nil
}

func authorities() []thorgenesis.Authority {
	authorities := make([]thorgenesis.Authority, 0)

	for _, account := range ValidatorAccounts {
		authorities = append(authorities, thorgenesis.Authority{
			MasterAddress:   account.Address,
			EndorsorAddress: account.Address,
			Identity:        datagen.RandKey(),
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

	addAccount := func(account thorgenesis.DevAccount, balance *big.Int) {
		accounts = append(accounts, thorgenesis.Account{
			Address: account.Address,
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

func mustGenerateAccounts(amount int) []thorgenesis.DevAccount {
	accounts := make([]thorgenesis.DevAccount, amount)

	for i := range amount {
		key, err := crypto.GenerateKey()
		if err != nil {
			panic(fmt.Sprintf("failed to generate key: %v", err))
		}
		address := crypto.PubkeyToAddress(key.PublicKey)
		accounts[i] = thorgenesis.DevAccount{
			Address:    thor.Address(address),
			PrivateKey: key,
		}
	}

	return accounts
}

func rndPort(used map[int]bool) int {
	const (
		minPort = 49152
		maxPort = 65535
	)
	for {
		buf := make([]byte, 2)
		// Ignoring the error for brevity—not recommended in production code!
		_, _ = rand.Read(buf)

		// Convert 2 bytes to a 16-bit number, then mod by the range size.
		n := int(buf[0])<<8 | int(buf[1])
		port := minPort + (n % (maxPort - minPort + 1))
		if _, ok := used[port]; !ok {
			used[port] = true
			return port
		}
	}
}
