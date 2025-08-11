package utils

import (
	"context"
	"errors"
	"github.com/vechain/thor/v2/tx"
	"log/slog"
	"time"

	"github.com/vechain/networkhub/network/node"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func WaitForPOS(staker *builtin.Staker, maxBlock uint32) error {
	maxBlockWithBuffer := maxBlock + 10 // add 10 blocks in case of forks
	return WaitForCondition(staker.Raw().Client(), maxBlockWithBuffer, func() (bool, error) {
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

// WaitForPeersConnection waits for all nodes to connect to each other
func WaitForPeersConnection(nodes []node.Config, ctx context.Context) error {
	// Timeout configuration
	timeout := 5 * time.Minute
	context, cancel := context.WithTimeout(ctx, timeout)
	tick := time.NewTicker(5 * time.Second)
	defer cancel()
	attempts := 0

	expectedPeersLen := len(nodes) - 1 // Each node should connect to all others except itself

	slog.Info("waiting for peers to connect...", "expected_peers", expectedPeersLen, "timeout", timeout.Seconds())

	for {
		select {
		case <-context.Done():
			// Log detailed information before failing
			for i, node := range nodes {
				c := thorclient.New(node.GetHTTPAddr())
				peers, err := c.Peers()
				if err != nil {
					slog.Error("failed to get peers", "node", i, "error", err)
				} else {
					slog.Error("node peer count", "node", i, "peers", len(peers), "expected", expectedPeersLen)
				}
			}
			return errors.New("timed out waiting for nodes to connect")

		case <-tick.C:
			attempts++
			allConnected := true

			for i, node := range nodes {
				c := thorclient.New(node.GetHTTPAddr())
				peers, err := c.Peers()
				if err != nil {
					slog.Warn("failed to get peers", "attempt", attempts, "node", i, "error", err)
					allConnected = false
					break
				}
				if len(peers) > expectedPeersLen {
					slog.Warn("too many peers connected", "attempt", attempts, "node", i, "peers", len(peers), "expected", expectedPeersLen)
					for _, p := range peers {
						slog.Warn("peer details", "address", p.NetAddr, "best", tx.NewBlockRefFromID(p.BestBlockID).Number())
					}
					return errors.New("too many peers connected")
				}
				if len(peers) < expectedPeersLen {
					slog.Warn("incorrect peer count", "attempt", attempts, "node", i, "peers", len(peers), "expected", expectedPeersLen)
					allConnected = false
					break
				}
			}
			if allConnected {
				slog.Info("all nodes connected successfully", "attempts", attempts)
				return nil
			}
		}
	}
}
