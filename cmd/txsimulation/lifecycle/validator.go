package lifecycle

import (
	"log/slog"

	"github.com/vechain/thor/v2/thorclient/builtin"
)

type ValidatorLifecycle struct {
	BaseLifecycle
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
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.status == StatusUnknown {
		v.status = StatusPending
	}

	if v.queuedReceipt == nil || v.id.IsZero() {
		return v.queue(engine, block)
	}
	validation, err := engine.stack.Staker().Get(v.id)
	if err != nil {
		slog.Error("failed to get validator", "error", err, "id", v.id)
		return err
	}
	if v.activatedBlock == 0 && validation.Status == builtin.StakerStatusActive {
		slog.Info("validator activated", "account", v.Account.Address(), "block", block)
		v.activatedBlock = validation.StartBlock
		v.status = StatusActive
		return nil
	}
	lastActiveBlock := v.activatedBlock + (v.StakingPeriods * engine.stack.Config().EpochLength) + 1
	if block < lastActiveBlock {
		return nil
	}
	if validation.AutoRenew && block >= lastActiveBlock {
		slog.Info("signalling exit for validator", "account", v.Account.Address(), "block", block)
		return v.exit(engine)
	}
	firstWithdrawBlock := lastActiveBlock +
		v.WithdrawDelay.Blocks +
		(v.WithdrawDelay.Epochs * engine.stack.Config().EpochLength)
	if v.exitReceipt != nil && // must have signalled exit
		block >= firstWithdrawBlock && // must be past the withdraw delay
		v.exitReceipt.Meta.BlockNumber+engine.stack.Config().MinStakingPeriod <= block { // must have completed the last staking period
		slog.Info("withdrawing validator", "account", v.Account.Address(), "block", block)
		return v.withdraw(engine)
	}
	return nil
}

func (v *ValidatorLifecycle) queue(engine *Engine, block uint32) error {
	firstQueueBlock := v.StartBlock +
		v.QueueDelay.Blocks +
		(v.QueueDelay.Epochs * engine.stack.Config().EpochLength)
	if block < firstQueueBlock {
		return nil
	}
	slog.Info("queuing validator", "account", v.Account.Address(), "block", block)

	id, receipt, err := engine.validators.QueueValidator(v.Account, true)
	if err != nil {
		slog.Error("failed to queue validator", "error", err, "account", v.Account.Address())
		return err
	}

	v.id = id
	v.queuedReceipt = receipt
	v.status = StatusQueued

	return nil
}

func (v *ValidatorLifecycle) exit(engine *Engine) error {
	if v.status == StatusExitSignalled {
		return nil
	}

	receipt, err := engine.validators.DisableAutoRenew(v.id)
	if err != nil {
		slog.Error("failed to disable auto-renew for validator", "error", err, "id", v.id)
		return err
	}
	v.status = StatusExitSignalled
	v.exitReceipt = receipt
	slog.Info("validator exit signalled", "id", v.id, "account", v.Account.Address())
	return nil
}

func (v *ValidatorLifecycle) withdraw(engine *Engine) error {
	if v.status != StatusExitSignalled {
		slog.Error("cannot withdraw validator that is not in exit signalled state", "id", v.id)
		return nil
	}

	receipt, err := engine.validators.Withdraw(v.id)
	if err != nil {
		slog.Error("failed to withdraw validator", "error", err, "id", v.id)
		return err
	}

	v.status = StatusWithdrawn
	v.withdrawReceipt = receipt

	slog.Info("validator withdrawn", "id", v.id, "account", v.Account.Address())
	return nil
}
