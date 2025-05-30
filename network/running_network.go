package network

import (
	"fmt"
	"time"

	"github.com/vechain/networkhub/network"
	"github.com/vechain/thor/v2/thorclient"
)

type RunningNetwork struct {
	chainTag int
	details  *ConnectionDetails
}

func NewRunningNetwork(addr string) (*RunningNetwork, error) {
	return NewRunningNetworkWithNetworkCfg(addr, nil)
}

func NewRunningNetworkWithNetworkCfg(addr string, networkCfg *network.Network) (*RunningNetwork, error) {
	chainClient := thorclient.New(addr)
	tag, err := chainClient.ChainTag()
	if err != nil {
		return nil, err
	}
	return &RunningNetwork{
		details: &ConnectionDetails{
			Address:    addr,
			ChainTag:   int(tag),
			NetworkCfg: networkCfg,
		},
	}, nil
}

func (r RunningNetwork) Start() error {
	c := thorclient.New(r.details.Address)
	for i := 0; i < 10; i++ {
		block, err := c.Block("best")
		if err != nil {
			return fmt.Errorf("unable to access network: %w", err)
		}
		if block.Number > 0 {
			return nil
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("unable to access network: received best block has height 0")
}

func (r RunningNetwork) Stop() error {
	return nil
}

func (r RunningNetwork) Details() *ConnectionDetails {
	return r.details
}
