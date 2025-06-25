package utils

import (
	"errors"
	"time"

	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func WaitForPOS(staker *builtin.Staker, maxBlock uint32) error {
	return WaitForCondition(staker.Raw().Client(), maxBlock+20, func() (bool, error) {
		_, id, err := staker.FirstActive()
		return err == nil && !id.IsZero(), nil
	})
}

func WaitForFork(staker *builtin.Staker, forkBlock uint32) error {
	addr := staker.Raw().Address()
	return WaitForCondition(staker.Raw().Client(), forkBlock, func() (bool, error) {
		acc, err := staker.Raw().Client().AccountCode(addr)
		if err != nil {
			return false, err
		}
		return len(acc.Code) > 100, nil
	})
}

func WaitForCondition(client *thorclient.Client, maxBlock uint32, condition func() (bool, error)) error {
	for {
		ok, err := condition()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		best, err := client.Block("best")
		if err != nil {
			return err
		}
		if best.Number > maxBlock {
			return errors.New("condition not met, max block reached")
		}
		time.Sleep(1 * time.Second)
	}
}
