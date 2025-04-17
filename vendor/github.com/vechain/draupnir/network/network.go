package network

import (
	"fmt"
	"github.com/vechain/draupnir/common"
	"github.com/vechain/networkhub/network"
	"os"
)

type ConnectionDetails struct {
	Address      string
	ChainTag     int
	SmokeAccount *common.Account
	NetworkCfg   *network.Network
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
	networkAddr := os.Getenv("SMOKE_TEST_NETWORK_ADDR")
	privateKey := os.Getenv("SMOKE_TEST_PK")

	// no env was set, use the default
	if networkAddr == "" && privateKey == "" {
		return n.LoadCustomNetwork(), nil
	}
	if networkAddr == "" || privateKey == "" {
		return nil, fmt.Errorf("all envs must be set")
	}

	return NewRunningNetwork(networkAddr, common.NewAccount(privateKey))
}

func (n *Network) LoadCustomNetwork() *CustomNetwork {
	return NewCustomNetwork()
}

func (n *Network) LoadThorSolo() (ActiveNetwork, error) {
	return NewRunningNetwork(thorSolo.Address, thorSolo.SmokeAccount)
}

func (n *Network) LoadTestnet() (ActiveNetwork, error) {
	return NewRunningNetwork(testnet.Address, testnet.SmokeAccount)
}
