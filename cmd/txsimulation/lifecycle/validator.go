package lifecycle

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/validations"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

type ValidatorLifecycle struct {
	ValidatorConfig

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

func NewValidatorLifecycle(config ValidatorConfig) *ValidatorLifecycle {
	return &ValidatorLifecycle{
		ValidatorConfig: config,
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

func (v *ValidatorLifecycle) Process(engine *Engine, block uint32) error {
	switch v.status {
	case StatusPending:
		return v.ProcessPending(engine, block)
	case StatusQueued:
		return v.ProcessQueued(engine, block)
	case StatusActive:
		return v.ProcessActive(engine, block)
	case StatusExitSignalled:
		return v.ProcessExited(engine, block)
	case StatusWithdrawn:

	}

	return nil
}

func (v *ValidatorLifecycle) ProcessPending(engine *Engine, block uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.queuedReceipt != nil {
		return nil
	}

	if block < v.Config.QueueBlock(engine.stack.Config()) {
		return nil
	}
	slog.Debug("queuing validator", "account", v.Account.Node.Address(), "block", block)

	period := engine.stack.RandomStakingPeriod()
	id, receipt, err := engine.validators.QueueValidator(v.Account, period)
	if err != nil {
		slog.Error("failed to queue validator", "error", err, "account", v.Account.Node.Address())
		return err
	}

	v.id = id
	v.queuedReceipt = receipt
	v.status = StatusQueued
	v.stakingPeriodLength = period

	return nil
}

func (v *ValidatorLifecycle) ProcessQueued(engine *Engine, block uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.queuedReceipt == nil {
		return errors.New("cannot set activated block for validator that has not been queued")
	}
	if v.activatedBlock != 0 {
		return nil
	}
	stakingPeriods, err := engine.stack.Staker().GetValidatorPeriodDetails(v.id)
	if err != nil {
		slog.Error("failed to get validator", "error", err, "id", v.id)
		return err
	}
	validationStatus, err := engine.stack.Staker().GetValidatorStatus(v.id)
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

func (v *ValidatorLifecycle) ProcessActive(engine *Engine, block uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.exitReceipt != nil {
		return nil
	}

	if v.status == StatusExitSignalled {
		return nil
	}
	if block < v.Config.MinExitBlock(v.activatedBlock, v.stakingPeriodLength) {
		return v.stakeChange(engine, block)
	}
	receipt, err := engine.validators.DisableAutoRenew(v.Account)
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

func (v *ValidatorLifecycle) stakeChange(engine *Engine, block uint32) error {
	interval := v.StakeChangeInterval * v.stakingPeriodLength
	if v.lastStakeUpdate+interval < block {
		return nil
	}
	var sender *bind.MethodBuilder
	if v.stakeIncreased {
		sender = engine.stack.Staker().DecreaseStake(v.Account.Node.Address(), validations.RandomStakeBetween(3, 5))
	} else {
		sender = engine.stack.Staker().IncreaseStake(v.Account.Node.Address(), validations.RandomStakeBetween(3, 5))
	}
	v.lastStakeUpdate = block
	v.stakeIncreased = !v.stakeIncreased
	_, err := engine.stack.SendTransaction(sender, v.Account.Endorser)
	return err
}

func (v *ValidatorLifecycle) ProcessExited(engine *Engine, block uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.withdrawReceipt != nil {
		return nil
	}

	if v.status != StatusExitSignalled || v.exitReceipt == nil {
		return errors.New("cannot withdraw validator that has not signalled exit")
	}
	if block < v.Config.MinWithdrawBlock(v.exitReceipt.Meta.BlockNumber, engine.stack.Config()) {
		return nil
	}
	receipt, err := engine.validators.Withdraw(v.Account)
	if err != nil {
		slog.Error("failed to withdraw validator", "error", err, "id", v.id)
		return err
	}

	v.status = StatusWithdrawn
	v.withdrawReceipt = receipt

	slog.Debug("validator withdrawn", "id", v.id, "account", v.Account.Endorser.Address())
	return nil
}
