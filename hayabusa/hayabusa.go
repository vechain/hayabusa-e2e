package hayabusa

import (
	"crypto/rand"
	_ "embed"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/vechain/thor/v2/thorclient"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/vechain/networkhub/entrypoint/client"
	"github.com/vechain/networkhub/environments"
	"github.com/vechain/networkhub/genesisbuilder"
	"github.com/vechain/networkhub/network/node"
	"github.com/vechain/networkhub/network/node/genesis"
	"github.com/vechain/networkhub/thorbuilder"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"

	networkhubNetwork "github.com/vechain/networkhub/network"
	thorgenesis "github.com/vechain/thor/v2/genesis"
)

var (
	ValidatorAccounts  = mustParseKeys(validatorKeys)
	Stargate           = mustParseKey("274c9caa1b72003d86eab9ea817f9b4c172246e75a9e20d1baaf44bbf5c89762")
	ParamsStargateKey  = nameToBytes32("stargate-contract-address")
	Executor           = (*bind.PrivateKeySigner)(thorgenesis.DevAccounts()[0].PrivateKey)
	AdditionalAccounts = mustParseKeys(additionalKeys)

	// Global port management to avoid race conditions
	portMutex       sync.Mutex
	globalUsedPorts = make(map[int]bool)

	// Global build mutex to prevent multiple Thor binary builds simultaneously
	buildMutex sync.Mutex
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

func StartNetwork(config *Config) (*thorclient.Client, environments.Actions, func(), error) {
	return StartNetworkWithID(config, "default")
}

func StartNetworkWithID(config *Config, networkID string) (*thorclient.Client, environments.Actions, func(), error) {
	if config.Nodes < 2 {
		return nil, nil, nil, fmt.Errorf("at least 2 nodes are required")
	}

	// Synchronize Thor binary build
	buildMutex.Lock()
	defer buildMutex.Unlock()

	repo := "git@github.com:vechain/thor.git"

	// reimplement this logic
	workingDir, ok := os.LookupEnv("THOR_WORKING_DIR")
	var thorBuilder *thorbuilder.Config
	if ok {
		thorBuilder = &thorbuilder.Config{
			BuildConfig: &thorbuilder.BuildConfig{
				ExistingPath: workingDir,
				DebugBuild:   config.Debug,
			},
		}
	} else {
		slog.Warn("THOR_WORKING_DIR not set, using default repo/branch")
		thorBuilder = &thorbuilder.Config{
			DownloadConfig: &thorbuilder.DownloadConfig{
				RepoUrl:    repo,
				Branch:     "release/hayabusa",
				IsReusable: true,
			},
		}
	}

	verbosity := 3
	if config.Verbosity > 0 {
		verbosity = config.Verbosity
	}

	customGenesis := Genesis(config)
	nodes := make([]node.Config, config.Nodes)
	usedPorts := make([]int, 0, config.Nodes*2) // API and P2P ports

	for i := range config.Nodes {
		additionalArgs := map[string]string{
			"txpool-limit-per-account": "100000",
			"api-allowed-tracers":      "all",
		}
		stakerVerbosity := max(config.StakerVerbosity, 0)
		if i == 0 { // enable verbose staker logs for 1 node
			additionalArgs["verbosity-staker"] = strconv.Itoa(stakerVerbosity)
		}

		nodeID := fmt.Sprintf("%s-Node-%d", networkID, i)
		apiPort := rndPort()
		p2pPort := rndPort()
		usedPorts = append(usedPorts, apiPort, p2pPort)

		nodes[i] = &node.BaseNode{
			ID:             nodeID,
			Key:            common.Bytes2Hex(ValidatorAccounts[i].D.Bytes()),
			Genesis:        customGenesis,
			Verbosity:      verbosity,
			AdditionalArgs: additionalArgs,
			APIAddr:        fmt.Sprintf("127.0.0.1:%d", apiPort),
			P2PListenPort:  p2pPort,
		}
	}
	networkCfg := &networkhubNetwork.Network{
		Environment: "local",
		BaseID:      networkID,
		Nodes:       nodes,
		ThorBuilder: thorBuilder,
	}

	networkHub := client.New()
	networkIDResult, err := networkHub.Config(networkCfg)
	if err != nil {
		cleanupPorts(usedPorts)
		return nil, nil, nil, err
	}

	hayabusaNetwork, err := networkHub.GetNetwork(networkIDResult.ID())
	if err != nil {
		cleanupPorts(usedPorts)
		return nil, nil, nil, err
	}

	err = hayabusaNetwork.StartNetwork()
	if err != nil {
		hayabusaNetwork.StopNetwork()

		cleanupPorts(usedPorts)
		return nil, nil, nil, err
	}

	if err = networkCfg.HealthCheck(0, 60*time.Second); err != nil {
		hayabusaNetwork.StopNetwork()

		cleanupPorts(usedPorts)
		return nil, nil, nil, fmt.Errorf("health check failed: %w", err)
	}

	// verbose logging for node 0, use node 1 for http (simulation etc.). Amount validated on first line of function
	client := thorclient.New(nodes[1].GetHTTPAddr())

	return client, hayabusaNetwork, func() {
		cleanupPorts(usedPorts)

		if err := hayabusaNetwork.StopNetwork(); err != nil {
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

func isPortAvailable(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// rndPort generates a random port using global synchronization to avoid race conditions
func rndPort() int {
	portMutex.Lock()
	defer portMutex.Unlock()

	const (
		minPort     = 49152
		maxPort     = 65535
		maxAttempts = 100
	)

	attempts := 0
	for attempts < maxAttempts {
		buf := make([]byte, 2)
		// Ignoring the error for brevity—not recommended in production code!
		_, _ = rand.Read(buf)

		// Convert 2 bytes to a 16-bit number, then mod by the range size.
		n := int(buf[0])<<8 | int(buf[1])
		port := minPort + (n % (maxPort - minPort + 1))

		// Check if port is not in our global map AND actually available
		if !globalUsedPorts[port] && isPortAvailable(port) {
			globalUsedPorts[port] = true
			return port
		}
		attempts++
	}

	// If we can't find an available port after maxAttempts,
	// try sequential search starting from minPort
	for port := minPort; port <= maxPort; port++ {
		if !globalUsedPorts[port] && isPortAvailable(port) {
			globalUsedPorts[port] = true
			return port
		}
	}

	panic(fmt.Sprintf("no available ports found in range %d-%d", minPort, maxPort))
}

// cleanupPorts releases the specified ports back to the global pool
func cleanupPorts(ports []int) {
	portMutex.Lock()
	defer portMutex.Unlock()

	for _, port := range ports {
		delete(globalUsedPorts, port)
	}
}
