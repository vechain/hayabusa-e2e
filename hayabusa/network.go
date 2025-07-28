package hayabusa

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/networkhub/environments/local"
	networkhubNetwork "github.com/vechain/networkhub/network"
	"github.com/vechain/networkhub/network/node"
	"github.com/vechain/networkhub/network/node/genesis"
	"github.com/vechain/networkhub/thorbuilder"
	"github.com/vechain/thor/v2/thorclient"
)

type Network struct {
	ctx       context.Context
	config    *Config
	network   *local.Local
	genesis   *genesis.CustomGenesis
	builder   *thorbuilder.Config
	nodes     []node.Config
	usedPorts []int // to track used ports for cleanup
	started   bool

	mu sync.Mutex
}

func NewNetworkV2(config *Config, ctx context.Context) *Network {
	buildMutex.Lock()
	defer buildMutex.Unlock()

	repo := "git@github.com:vechain/thor.git"

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
	nodes := make([]node.Config, 0)
	genesis := Genesis(config)
	usedPorts := make([]int, 0, config.Nodes*2)
	for i := range config.Nodes {
		node, apiPort, p2pPort := makeNode(config, i, genesis)
		nodes = append(nodes, node)
		usedPorts = append(usedPorts, apiPort, p2pPort)
	}

	network := local.NewLocalEnv()

	return &Network{
		ctx:       ctx,
		config:    config,
		network:   network,
		genesis:   Genesis(config),
		builder:   thorBuilder,
		nodes:     nodes,
		usedPorts: usedPorts,
	}
}

func (n *Network) ThorClient() *thorclient.Client {
	return thorclient.New(n.nodes[0].GetHTTPAddr())
}

func (n *Network) Genesis() *genesis.CustomGenesis {
	return n.genesis
}

func (n *Network) Stop() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	globalPortManager.RemovePorts(n.config.Name)

	if err := n.network.StopNetwork(); err != nil {
		return fmt.Errorf("failed to stop network: %w", err)
	}
	n.started = false
	return nil
}

func (n *Network) MustStop() {
	if err := n.Stop(); err != nil {
		panic(err)
	}
}

func (n *Network) NodeConfigs() []node.Config {
	n.mu.Lock()
	defer n.mu.Unlock()

	return n.nodes
}

func (n *Network) NodeLifecycles() map[string]node.Lifecycle {
	n.mu.Lock()
	defer n.mu.Unlock()

	return n.network.Nodes()
}

func (n *Network) Start() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	buildMutex.Lock()
	defer buildMutex.Unlock()

	go func() {
		<-n.ctx.Done()
		slog.Info("context done, cleaning up network")
		if err := n.Stop(); err != nil {
			slog.Error("failed to stop network", "error", err)
		}
	}()

	netConfig := &networkhubNetwork.Network{
		BaseID:      n.config.Name,
		Nodes:       n.nodes,
		ThorBuilder: n.builder,
	}
	if _, err := n.network.LoadConfig(netConfig); err != nil {
		return fmt.Errorf("failed to load network config: %w", err)
	}

	if err := n.network.StartNetwork(); err != nil {
		return err
	}

	for _, nodeConfig := range n.nodes {
		if err := nodeConfig.HealthCheck(0, 30*time.Second); err != nil {
			return err
		}
	}

	if err := utils.WaitForPeersConnection(n.nodes, n.config.Nodes-1, n.ctx); err != nil {
		return fmt.Errorf("failed to connect all nodes: %w", err)
	}

	n.started = true

	return nil
}

func (n *Network) AttachNode(buildConfig *thorbuilder.DownloadConfig) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	buildMutex.Lock()
	defer buildMutex.Unlock()

	builder := thorbuilder.New(&thorbuilder.Config{DownloadConfig: buildConfig})

	if err := builder.Download(); err != nil {
		return fmt.Errorf("failed to download builder: %w", err)
	}
	path, err := builder.Build()
	if err != nil {
		return fmt.Errorf("failed to build node: %w", err)
	}

	node, apiPort, p2pPort := makeNode(n.config, len(n.nodes), n.genesis)
	node.SetExecArtifact(path)
	n.nodes = append(n.nodes, node)
	n.usedPorts = append(n.usedPorts, apiPort, p2pPort)
	if err := n.network.AttachNode(node); err != nil {
		return fmt.Errorf("failed to attach node: %w", err)
	}
	if n.started {
		if err := node.HealthCheck(0, 30*time.Second); err != nil {
			return fmt.Errorf("failed to health check attached node: %w", err)
		}
	}

	return nil
}

func makeNode(config *Config, i int, customGenesis *genesis.CustomGenesis) (node.Config, int, int) {
	verbosity := 3
	if config.Verbosity > 0 {
		verbosity = config.Verbosity
	}
	additionalArgs := map[string]string{
		"txpool-limit-per-account": "100000",
		"api-allowed-tracers":      "all",
	}
	if i == 0 { // enable verbose staker logs for 1 node
		additionalArgs["verbosity-staker"] = strconv.Itoa(max(config.StakerVerbosity, 3))
	}
	nodeID := fmt.Sprintf("Node-%d", i)
	if config.Name != "" {
		nodeID = fmt.Sprintf("%s-%s", config.Name, nodeID)
	}

	apiPort := globalPortManager.NewPort(config.Name)
	p2pPort := globalPortManager.NewPort(config.Name)

	return &node.BaseNode{
		ID:             nodeID,
		Key:            common.Bytes2Hex(ValidatorAccounts[i].D.Bytes()),
		Genesis:        customGenesis,
		Verbosity:      verbosity,
		AdditionalArgs: additionalArgs,
		APIAddr:        fmt.Sprintf("0.0.0.0:%d", apiPort),
		P2PListenPort:  p2pPort,
	}, apiPort, p2pPort
}
