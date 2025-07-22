package lifecycle

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

type ValidatorLifecycle struct {
	Config

	status          Status
	queuedReceipt   *api.Receipt // the receipt of the queued transaction
	activatedBlock  uint32       // the block at which this lifecycle was activated
	exitReceipt     *api.Receipt // the receipt of the exit transaction
	withdrawReceipt *api.Receipt // the receipt of the withdraw transaction
	id              thor.Bytes32

	mu sync.Mutex
}

func NewValidatorLifecycle(config Config) *ValidatorLifecycle {
	return &ValidatorLifecycle{
		Config: config,
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

	firstQueueBlock := v.StartBlock +
		v.QueueDelay.Blocks +
		(v.QueueDelay.Epochs * engine.stack.Config().EpochLength)
	if block < firstQueueBlock {
		return nil
	}
	slog.Debug("queuing validator", "account", v.Account.Address(), "block", block)

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

func (v *ValidatorLifecycle) ProcessQueued(engine *Engine, block uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.queuedReceipt == nil {
		return errors.New("cannot set activated block for validator that has not been queued")
	}
	if v.activatedBlock != 0 {
		return nil
	}
	validation, err := engine.stack.Staker().Get(v.id)
	if err != nil {
		slog.Error("failed to get validator", "error", err, "id", v.id)
		return err
	}
	if validation.Status == builtin.StakerStatusActive {
		slog.Debug("validator activated", "account", v.Account.Address(), "block", block)
		v.activatedBlock = validation.StartBlock
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
	minExitBlock := v.activatedBlock +
		(v.StakingPeriods * engine.stack.Config().MinStakingPeriod) + 1
	if block < minExitBlock {
		return nil
	}

	receipt, err := engine.validators.DisableAutoRenew(v.id, v.Account)
	if err != nil {
		slog.Error("failed to disable auto-renew for validator", "error", err, "id", v.id)
		return err
	}
	v.status = StatusExitSignalled
	v.exitReceipt = receipt
	slog.Debug("validator exit signalled", "id", v.id, "account", v.Account.Address())
	return nil
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
	minWithdrawBlock := v.exitReceipt.Meta.BlockNumber +
		engine.stack.Config().MinStakingPeriod +
		v.WithdrawDelay.Blocks +
		(v.WithdrawDelay.Epochs * engine.stack.Config().MinStakingPeriod)
	if block < minWithdrawBlock {
		return nil
	}
	receipt, err := engine.validators.Withdraw(v.id, v.Account)
	if err != nil {
		slog.Error("failed to withdraw validator", "error", err, "id", v.id)
		return err
	}

	v.status = StatusWithdrawn
	v.withdrawReceipt = receipt

	slog.Debug("validator withdrawn", "id", v.id, "account", v.Account.Address())
	return nil
}
