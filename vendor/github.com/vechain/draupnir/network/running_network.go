package network

import (
	"fmt"
	"github.com/vechain/draupnir/common"
	"github.com/vechain/thor/v2/thorclient"
	"time"
)

type RunningNetwork struct {
	chainTag     int
	smokeAccount *common.Account
	details      *ConnectionDetails
}

func NewRunningNetwork(addr string, smokeAccount *common.Account) (*RunningNetwork, error) {
	chainClient := thorclient.New(addr)
	tag, err := chainClient.ChainTag()
	if err != nil {
		return nil, err
	}
	return &RunningNetwork{
		details: &ConnectionDetails{
			Address:      addr,
			ChainTag:     int(tag),
			SmokeAccount: smokeAccount,
		},
	}, nil
}

func (r RunningNetwork) Start() error {
	c := thorclient.New(r.details.Address)
	// sometimes the running networks is spun off immediately before the test
	// it's fine to retry a few times before failing
	return common.Retry(func() error {
		block, err := c.Block("best")
		if err != nil {
			return fmt.Errorf("unable to access network: %w", err)
		}
		if block.Number == 0 {
			return fmt.Errorf("unable to access network: received best block has height 0")
		}

		return nil
	}, 5*time.Second, 30*time.Second)
}

func (r RunningNetwork) Stop() error {
	return nil
}

func (r RunningNetwork) Details() *ConnectionDetails {
	return r.details
}
