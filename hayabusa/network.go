package hayabusa

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vechain/networkhub/environments/local"
	networkhubNetwork "github.com/vechain/networkhub/network"
	"github.com/vechain/networkhub/network/node"
	"github.com/vechain/networkhub/network/node/genesis"
	"github.com/vechain/networkhub/thorbuilder"
	"github.com/vechain/thor/v2/thorclient"
)

type Network struct {
	ctx     context.Context
	config  *Config
	network *local.Local
	genesis *genesis.CustomGenesis
	nodes   []node.Config

	mu sync.Mutex
}

func NewNetwork(config *Config, ctx context.Context) (*Network, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	nodes := make([]node.Config, 0)
	genesis := Genesis(config)
	for i := range config.Nodes {
		node := makeNode(config, i, ValidatorAccounts[i], genesis)
		nodes = append(nodes, node)
	}

	network := local.NewEnv()
	netConfig := &networkhubNetwork.Network{
		BaseID:      config.Name,
		Nodes:       nodes,
		ThorBuilder: thorbuilder.DefaultConfig(),
	}
	if _, err := network.LoadConfig(netConfig); err != nil {
		return nil, fmt.Errorf("failed to load network config: %w", err)
	}

	return &Network{
		ctx:     ctx,
		config:  config,
		network: network,
		genesis: genesis,
		nodes:   nodes,
	}, nil
}

func (n *Network) ThorClient() *thorclient.Client {
	return thorclient.New(n.nodes[0].GetHTTPAddr())
}

func (n *Network) Genesis() *genesis.CustomGenesis {
	return n.genesis
}

func (n *Network) Stop() {
	if err := n.network.StopNetwork(); err != nil {
		slog.Error("🛑 failed to stop network", "error", err)
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
	go func() {
		<-n.ctx.Done()
		slog.Info("context done, cleaning up network")
		n.Stop()
	}()

	if err := n.network.StartNetwork(); err != nil {
		return err
	}

	return nil
}

func (n *Network) AttachNode(buildConfig *thorbuilder.Config, additionalArgs map[string]string) error {
	node := makeNode(n.config, len(n.nodes), ValidatorAccounts[len(n.nodes)], n.genesis)
	if err := n.network.AttachNode(node, buildConfig, additionalArgs); err != nil {
		return fmt.Errorf("failed to attach node: %w", err)
	}

	return nil
}

func makeNode(config *Config, i int, signer *NodePair, customGenesis *genesis.CustomGenesis) node.Config {
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

	return &node.BaseNode{
		ID:             nodeID,
		Key:            common.Bytes2Hex(signer.Node.D.Bytes()),
		Genesis:        customGenesis,
		Verbosity:      verbosity,
		AdditionalArgs: additionalArgs,
	}
}
