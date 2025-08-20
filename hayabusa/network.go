package hayabusa

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
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
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
)

type Network struct {
	ctx       context.Context
	config    *Config
	network   *local.Local
	genesis   *genesis.CustomGenesis
	nodes     []node.Config
	usedPorts []int // to track used ports for cleanup

	mu sync.Mutex
}

func NewNetwork(config *Config, ctx context.Context) (*Network, error) {
	buildMutex.Lock()
	defer buildMutex.Unlock()

	if err := config.Validate(); err != nil {
		return nil, err
	}

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
		builder := thorbuilder.New(thorBuilder)
		err := builder.Download()
		if err != nil {
			return nil, fmt.Errorf("failed to download thor: %w", err)
		}
		workingDir = builder.DownloadPath
	}
	filePath := workingDir + "/thor/params.go"
	blockInterval := thor.BlockInterval()
	if config.BlockInterval != nil {
		blockInterval = *config.BlockInterval
	}
	if err := SetBlockInterval(filePath, blockInterval); err != nil {
		return nil, fmt.Errorf("failed to patch BlockInterval: %w", err)
	}

	nodes := make([]node.Config, 0)
	genesis := Genesis(config)
	usedPorts := make([]int, 0, config.Nodes*2)
	for i := range config.Nodes {
		node, apiPort, p2pPort := makeNode(config, i, ValidatorAccounts[i], genesis)
		nodes = append(nodes, node)
		usedPorts = append(usedPorts, apiPort, p2pPort)
	}

	network := local.NewEnv()
	netConfig := &networkhubNetwork.Network{
		BaseID:      config.Name,
		Nodes:       nodes,
		ThorBuilder: thorBuilder,
	}
	builder := thorbuilder.New(netConfig.ThorBuilder)
	builder.Download()
	if _, err := network.LoadConfig(netConfig); err != nil {
		return nil, fmt.Errorf("failed to load network config: %w", err)
	}

	return &Network{
		ctx:       ctx,
		config:    config,
		network:   network,
		genesis:   genesis,
		nodes:     nodes,
		usedPorts: usedPorts,
	}, nil
}

func (n *Network) ThorClient() *thorclient.Client {
	return thorclient.New(n.nodes[0].GetHTTPAddr())
}

func (n *Network) Genesis() *genesis.CustomGenesis {
	return n.genesis
}

func (n *Network) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()

	if err := n.network.StopNetwork(); err != nil {
		slog.Error("🛑 failed to stop network", "error", err)
	}
	globalPortManager.RemovePorts(n.config.Name)
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
		n.Stop()
	}()

	if err := n.network.StartNetwork(); err != nil {
		return err
	}

	for _, nodeConfig := range n.nodes {
		if err := nodeConfig.HealthCheck(0, 30*time.Second); err != nil {
			return err
		}
	}

	if err := utils.WaitForPeersConnection(n.nodes, n.ctx); err != nil {
		return fmt.Errorf("failed to connect all nodes: %w", err)
	}

	return nil
}

func (n *Network) AttachNode(buildConfig *thorbuilder.Config, additionalArgs map[string]string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	buildMutex.Lock()
	defer buildMutex.Unlock()

	builder := thorbuilder.New(buildConfig)

	if err := builder.Download(); err != nil {
		return fmt.Errorf("failed to download builder: %w", err)
	}
	path, err := builder.Build()
	if err != nil {
		return fmt.Errorf("failed to build node: %w", err)
	}

	node, apiPort, p2pPort := makeNode(n.config, len(n.nodes), ValidatorAccounts[len(n.nodes)], n.genesis)
	node.SetExecArtifact(path)
	node.SetAdditionalArgs(additionalArgs)
	n.nodes = append(n.nodes, node)
	n.usedPorts = append(n.usedPorts, apiPort, p2pPort)
	if err := n.network.AttachNode(node); err != nil {
		return fmt.Errorf("failed to attach node: %w", err)
	}
	if err := node.HealthCheck(0, 30*time.Second); err != nil {
		return fmt.Errorf("failed to health check attached node: %w", err)
	}

	return nil
}

func makeNode(config *Config, i int, signer *NodePair, customGenesis *genesis.CustomGenesis) (node.Config, int, int) {
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
		Key:            common.Bytes2Hex(signer.Node.D.Bytes()),
		Genesis:        customGenesis,
		Verbosity:      verbosity,
		AdditionalArgs: additionalArgs,
		APIAddr:        fmt.Sprintf("0.0.0.0:%d", apiPort),
		P2PListenPort:  p2pPort,
	}, apiPort, p2pPort
}

func SetBlockInterval(path string, v uint64) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	re := regexp.MustCompile(`(?m)(BlockInterval\s+uint64\s*=\s*)\d+`)
	out := re.ReplaceAll(b, []byte(fmt.Sprintf("${1}%d", v)))
	return os.WriteFile(path, out, 0o644)
}
