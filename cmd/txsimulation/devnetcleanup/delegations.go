package devnetcleanup

import (
	"log/slog"
	"math/big"
	"time"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/contract"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/networkhub/utils/common"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thorclient/bind"
)

type Cleanup struct {
	stack           *stack.Stack
	contract        *contract.Service
	stargate        *bind.PrivateKeySigner
	previousCleaned *big.Int
}

func New(stack *stack.Stack, contract *contract.Service, stargate *bind.PrivateKeySigner) *Cleanup {
	return &Cleanup{
		stack:    stack,
		contract: contract,
		stargate: stargate,
	}
}

func (c *Cleanup) Run() error {
	limit := uint64(1)
	events, err := c.stack.Staker().FilterDelegationAdded(nil, &api.Options{Limit: &limit}, "desc")
	if err != nil {
		slog.Error("failed to find last ID")
		return err
	}
	if len(events) == 0 {
		slog.Info("no delegations found, nothing to clean up")
		return nil
	}

	currentID := big.NewInt(1)

	lastID := events[0].DelegationID
	searchLimit := big.NewInt(999)
	if c.previousCleaned != nil {
		currentID = c.previousCleaned.Add(c.previousCleaned, big.NewInt(1))
	}

	var locked []*big.Int
	var withdrawable []*big.Int

	for currentID.Cmp(lastID) <= 0 {
		end := big.NewInt(0).Add(currentID, searchLimit)
		if end.Cmp(lastID) == 1 {
			end = lastID
		}
		slog.Info("searching delegations", "start", currentID, "end", end)
		currentLocked, currentWithdrawable, err := c.contract.FetchLockedDelegators(currentID, end)
		if err != nil {
			slog.Error("failed to fetch delegations", "err", err)
			return err
		}
		locked = append(locked, currentLocked...)
		withdrawable = append(withdrawable, currentWithdrawable...)
		currentID = big.NewInt(0).Add(end, big.NewInt(1))
	}

	if len(locked) > 0 {
		slog.Info("signalling exit for all delegations", "first", locked[0].String(), "last", locked[len(locked)-1].String())
		c.exitDelegations(locked, c.stack.Staker().SignalDelegationExit)
	}

	if len(withdrawable) > 0 {
		slog.Info("withdrawing for all delegations", "first", withdrawable[0].String(), "last", withdrawable[len(withdrawable)-1].String())
		c.exitDelegations(withdrawable, c.stack.Staker().WithdrawDelegation)
	}

	c.previousCleaned = lastID
	slog.Info("cleanup completed")
	return nil
}

type exitMethod func(id *big.Int) *bind.MethodBuilder

func (c *Cleanup) exitDelegations(delegations []*big.Int, method exitMethod) {
	var groups [][]*big.Int
	groupSize := 100
	for i := 0; i < len(delegations); i += groupSize {
		end := min(i+groupSize, len(delegations))
		groups = append(groups, delegations[i:end])
	}

	ticker := common.NewTicker(c.stack.Client())

	for _, ids := range groups {
		slog.Info("processing cleanup", "first", ids[0], "last", ids[len(ids)-1], "count", len(ids))

		for _, id := range ids {
			_, err := c.stack.SendTransaction(method(id), c.stargate)
			if err != nil {
				slog.Error("failed to send exit delegation transaction", "id", id, "error", err)
			}
		}
		ticker.Wait(60 * time.Second)
	}
}
