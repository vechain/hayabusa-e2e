package network

import (
	"fmt"
	"github.com/vechain/networkhub/network"
	"github.com/vechain/networkhub/network/node"
	"os"
	"strings"
)

type ConnectionDetails struct {
	Address    string
	ChainTag   int
	NetworkCfg *network.Network
}

type ActiveNetwork interface {
	Start() error
	Stop() error

	Details() *ConnectionDetails
}

type Network struct {
}

func New() *Network {
	return &Network{}
}

func (n *Network) LoadEnvNetwork() (ActiveNetwork, error) {
	networkAddr := os.Getenv("TEST_NETWORK_ADDR")
	privateKey := os.Getenv("TEST_PK")

	// no env was set, use the default
	if networkAddr == "" && privateKey == "" {
		return n.LoadCustomNetwork(), nil
	}
	if networkAddr == "" || privateKey == "" {
		return nil, fmt.Errorf("all envs must be set")
	}

	// todo refactor this
	soloNode := node.New()
	soloNode.SetAPIAddr(strings.Replace(thorSolo.Address, "http://", "", 1))

	soloNetworkCfg := &network.Network{
		Environment: "local",
		Nodes: []node.Config{
			soloNode,
		},
		ID: "thor-solo",
	}

	return NewRunningNetworkWithNetworkCfg(networkAddr, soloNetworkCfg)
}

func (n *Network) LoadCustomNetwork() *CustomNetwork {
	return NewCustomNetwork()
}

func (n *Network) LoadThorSolo() (ActiveNetwork, error) {
	soloNode := node.New()
	soloNode.SetAPIAddr(strings.Replace(thorSolo.Address, "http://", "", 1))

	soloNetworkCfg := &network.Network{
		Environment: "local",
		Nodes: []node.Config{
			soloNode,
		},
		ID: "thor-solo",
	}
	return NewRunningNetworkWithNetworkCfg(thorSolo.Address, soloNetworkCfg)
}

func (n *Network) LoadTestnet() (ActiveNetwork, error) {
	return NewRunningNetwork(testnet.Address)
}
