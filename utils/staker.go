package utils

import (
	"errors"
	"time"

	"github.com/vechain/thor/v2/thorclient/builtin"
	"github.com/vechain/thor/v2/thorclient/httpclient"
)

func WaitForPOS(staker *builtin.Staker, maxBlock uint32) error {
	return WaitForCondition(staker.Raw().Client(), maxBlock, func() (bool, error) {
		_, id, err := staker.FirstActive()
		return err == nil && !id.IsZero(), nil
	})
}

func WaitForFork(staker *builtin.Staker, forkBlock uint32) error {
	addr := staker.Raw().Address()
	return WaitForCondition(staker.Raw().Client(), forkBlock, func() (bool, error) {
		acc, err := staker.Raw().Client().GetAccountCode(&addr, "best")
		if err != nil {
			return false, err
		}
		return len(acc.Code) > 100, nil
	})
}

func WaitForCondition(client *httpclient.Client, maxBlock uint32, condition func() (bool, error)) error {
	for {
		ok, err := condition()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		best, err := client.GetBlock("best")
		if err != nil {
			return err
		}
		if best.Number > maxBlock {
			return errors.New("condition not met, max block reached")
		}
		time.Sleep(1 * time.Second)
	}
}
