package utils

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/vechain/networkhub/network/node"
	"github.com/vechain/thor/v2/thorclient"

	"github.com/vechain/thor/v2/api/blocks"
	"github.com/vechain/thor/v2/thorclient/common"
)

type Ticker struct {
	client *thorclient.Client
}

func NewTicker(client *thorclient.Client) *Ticker {
	return &Ticker{
		client: client,
	}
}

// Wait waits for a new best block to be available
func (t *Ticker) Wait(timeout time.Duration) (*blocks.JSONExpandedBlock, error) {
	best, err := t.client.ExpandedBlock("best")
	if err != nil {
		return nil, err
	}

	ticker := time.NewTicker(timeout)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			return nil, errors.New("timeout waiting for block")
		default:
			block, err := t.client.ExpandedBlock("best")
			if err == nil && block != nil && block.Number > best.Number {
				return block, nil
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (t *Ticker) WaitForBlock(blockNumber uint32) error {
	best, err := t.client.ExpandedBlock("best")
	if err != nil {
		return err
	}
	if blockNumber <= best.Number {
		return nil
	}
	if best.Number == 0 { // edge case -> spinning up a new network with old genesis timestamps
		best.Timestamp = uint64(time.Now().Unix())
	}
	expectedTime := best.Timestamp + uint64(blockNumber-best.Number)*10
	timeout := time.Until(time.Unix(int64(expectedTime), 0).Add(20 * time.Second))
	ticker := time.NewTicker(timeout)
	defer ticker.Stop()

	slog.Info("waiting for block...", "block", blockNumber, "timeout", timeout.Seconds())

	for {
		select {
		case <-ticker.C:
			return errors.New("timeout waiting for block")
		default:
			block, err := t.client.ExpandedBlock(strconv.Itoa(int(blockNumber)))
			if block != nil && block.Number >= blockNumber {
				return nil
			}
			if err != nil && !errors.Is(err, common.ErrNotFound) {
				return fmt.Errorf("unexpected error getting block: %w", err)
			}
			time.Sleep(1 * time.Second)
			slog.Warn("waiting for block...", "block", blockNumber, "timeout", time.Until(time.Unix(int64(expectedTime), 0).Add(2*time.Second)))
		}
	}
}

type ConditionFunc func() (bool, error)

func (t *Ticker) WaitForCondition(timeout time.Duration, conditionalFunc ConditionFunc) error {
	ticker := time.NewTicker(timeout)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			return errors.New("timeout waiting for block")
		default:
			resp, err := conditionalFunc()
			if err != nil {
				return err
			}
			if resp {
				return nil
			}
			time.Sleep(10 * time.Second)
		}
	}
}

// WaitForPeersConnection waits for all nodes to connect to each other
func WaitForPeersConnection(t *testing.T, nodes []node.Config, expectedPeersLen int) []*thorclient.Client {
	// Timeout configuration
	timeout := 5 * time.Minute
	timeoutChan := time.After(timeout)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()

	clients := make([]*thorclient.Client, 0)
	attempts := 0

	slog.Info("waiting for peers to connect...", "expected_peers", expectedPeersLen, "timeout", timeout.Seconds())

	for {
		select {
		case <-timeoutChan:
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
			t.Fatal("timed out waiting for nodes to connect")

		case <-tick.C:
			attempts++
			allConnected := true

			for i, node := range nodes {
				c := thorclient.New(node.GetHTTPAddr())
				peers, err := c.Peers()
				if err != nil {
					slog.Warn("failed to get peers", "attempt", attempts, "node", i, "error", err)
					allConnected = false
					clients = clients[:0]
					break
				}
				if len(peers) != expectedPeersLen {
					slog.Warn("incorrect peer count", "attempt", attempts, "node", i, "peers", len(peers), "expected", expectedPeersLen)
					allConnected = false
					clients = clients[:0]
					break
				}
				clients = append(clients, c)
			}
			if allConnected {
				slog.Info("all nodes connected successfully", "attempts", attempts)
				return clients
			}
		}
	}
}
