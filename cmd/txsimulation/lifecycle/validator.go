package lifecycle

import (
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/delegations"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/validations"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/logdb"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

type ValidatorLifecycle struct {
	ValidatorConfig
	validations *validations.State
	delegations *delegations.PositionManager
	stack       *stack.Stack

	status          Status
	queuedReceipt   *api.Receipt // the receipt of the queued transaction
	activatedBlock  uint32       // the block at which this lifecycle was activated
	exitReceipt     *api.Receipt // the receipt of the exit transaction
	withdrawReceipt *api.Receipt // the receipt of the withdraw transaction
	id              thor.Address

	stakingPeriodLength uint32 // the length of the staking period in blocks
	stakeIncreased      bool   // indicates if the stake as previously increased or decreased
	lastStakeUpdate     uint32 // the last block at which the stake was updated

	mu sync.Mutex
}

func NewValidatorLifecycle(
	config ValidatorConfig,
	validations *validations.State,
	delegations *delegations.PositionManager,
	stack *stack.Stack,
) *ValidatorLifecycle {
	return &ValidatorLifecycle{
		ValidatorConfig: config,
		validations:     validations,
		delegations:     delegations,
		stack:           stack,
	}
}

var _ Lifecycle = (*ValidatorLifecycle)(nil)

func (v *ValidatorLifecycle) ID() string {
	if v.id.IsZero() {
		return "n/a"
	}
	return v.id.String()
}

func (v *ValidatorLifecycle) Type() Type {
	return TypeValidator
}

func (v *ValidatorLifecycle) Status() Status {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.status
}

func (v *ValidatorLifecycle) Info() *Info {
	v.mu.Lock()
	defer v.mu.Unlock()

	return &Info{
		Type:            v.Type(),
		ID:              v.ID(),
		Status:          v.status,
		QueuedReceipt:   v.queuedReceipt,
		ActivatedBlock:  v.activatedBlock,
		WithdrawReceipt: v.withdrawReceipt,
		ExitReceipt:     v.exitReceipt,
		ValidationID:    v.id,
	}
}

func (v *ValidatorLifecycle) Process(block uint32) error {
	status, ok := v.validations.LookupAddress(v.id)
	if ok && status.Status == builtin.StakerStatusExited {
		v.delegations.UnregisterValidator(v.id)
	}

	switch v.status {
	case StatusPending:
		return v.ProcessPending(block)
	case StatusQueued:
		return v.ProcessQueued(block)
	case StatusActive:
		return v.ProcessActive(block)
	case StatusExitSignalled:
		return v.ProcessExited(block)
	case StatusWithdrawn:

	}

	return nil
}

func (v *ValidatorLifecycle) ProcessPending(block uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.queuedReceipt != nil {
		return nil
	}

	if block < v.Config.QueueBlock(v.stack.Config()) {
		return nil
	}
	slog.Debug("queuing validator", "account", v.Account.Node.Address(), "block", block)

	period := v.stack.RandomStakingPeriod()
	id, receipt, err := v.validations.QueueValidator(v.Account, period)
	if err != nil {
		if strings.Contains(err.Error(), " validator already exists") {
			return v.setQueuedReceipt()
		}
		slog.Error("failed to queue validator", "error", err, "account", v.Account.Node.Address())
		return err
	}

	v.id = id
	v.queuedReceipt = receipt
	v.status = StatusQueued
	v.stakingPeriodLength = period

	return nil
}

func (v *ValidatorLifecycle) setQueuedReceipt() error {
	offset := uint64(0)
	limit := uint64(1000)
	var id thor.Bytes32
	for {
		events, err := v.stack.Staker().FilterValidatorQueued(nil, &api.Options{Limit: limit, Offset: offset}, logdb.ASC)
		if err != nil {
			slog.Error("failed to filter validator queued events", "error", err, "id", v.id)
			return err
		}
		for _, event := range events {
			if event.Node == v.id {
				v.status = StatusQueued
				v.stakingPeriodLength = event.Period
				v.id = event.Node
				break
			}
		}

		if !id.IsZero() || len(events) == 0 {
			break
		}
		offset += limit
	}
	if !id.IsZero() {
		receipt, err := v.stack.Client().TransactionReceipt(&id)
		if err != nil {
			slog.Error("failed to get transaction receipt for queued validator", "error", err, "id", v.id)
			return err
		}
		v.queuedReceipt = receipt

		slog.Debug("validator queued", "id", v.id, "account", v.Account.Node.Address())
		return nil
	}

	return nil
}

func (v *ValidatorLifecycle) ProcessQueued(block uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.queuedReceipt == nil {
		return errors.New("cannot set activated block for validator that has not been queued")
	}
	if v.activatedBlock != 0 {
		return nil
	}
	stakingPeriods, err := v.stack.Staker().GetValidatorPeriodDetails(v.id)
	if err != nil {
		slog.Error("failed to get validator", "error", err, "id", v.id)
		return err
	}
	validationStatus, err := v.stack.Staker().GetValidatorStatus(v.id)
	if err != nil {
		slog.Error("failed to get validator status", "error", err, "id", v.id)
	}
	if validationStatus.Status == builtin.StakerStatusActive {
		slog.Debug("validator activated", "account", v.Account.Node.Address(), "block", block)
		v.activatedBlock = stakingPeriods.StartBlock
		v.status = StatusActive
	}
	return nil
}

func (v *ValidatorLifecycle) ProcessActive(block uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.exitReceipt != nil {
		return nil
	}

	if v.status == StatusExitSignalled {
		return nil
	}
	if block < v.Config.MinExitBlock(v.activatedBlock, v.stakingPeriodLength) {
		return v.stakeChange(block)
	}
	receipt, err := v.validations.DisableAutoRenew(v.Account)
	if receipt != nil {
		v.status = StatusExitSignalled
		v.exitReceipt = receipt
	}
	if err != nil {
		slog.Error("failed to disable auto-renew for validator", "error", err, "id", v.id)
		return err
	}
	v.status = StatusExitSignalled
	slog.Debug("validator exit signalled", "id", v.id, "account", v.Account.Endorser.Address())
	return nil
}

func (v *ValidatorLifecycle) stakeChange(block uint32) error {
	interval := v.StakeChangeInterval * v.stakingPeriodLength
	if v.lastStakeUpdate+interval > block {
		return nil
	}
	var sender *bind.MethodBuilder
	if v.stakeIncreased {
		sender = v.stack.Staker().DecreaseStake(v.Account.Node.Address(), validations.RandomStakeBetween(3, 5))
	} else {
		sender = v.stack.Staker().IncreaseStake(v.Account.Node.Address(), validations.RandomStakeBetween(3, 5))
	}
	v.lastStakeUpdate = block
	v.stakeIncreased = !v.stakeIncreased
	_, err := v.stack.SendTransaction(sender, v.Account.Endorser)
	return err
}

func (v *ValidatorLifecycle) ProcessExited(block uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.withdrawReceipt != nil {
		return nil
	}

	if v.status != StatusExitSignalled || v.exitReceipt == nil {
		return errors.New("cannot withdraw validator that has not signalled exit")
	}
	if block < v.Config.MinWithdrawBlock(v.exitReceipt.Meta.BlockNumber, v.stack.Config()) {
		return nil
	}
	receipt, err := v.validations.Withdraw(v.Account)
	if err != nil {
		slog.Error("failed to withdraw validator", "error", err, "id", v.id)
		return err
	}

	v.status = StatusWithdrawn
	v.withdrawReceipt = receipt

	slog.Debug("validator withdrawn", "id", v.id, "account", v.Account.Endorser.Address())
	return nil
}
