package lifecycle

import (
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/delegations"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
	"log/slog"
	"math/big"
)

type DelegatorLifecycle struct {
	BaseLifecycle
	validationID thor.Bytes32
}

var _ Lifecycle = (*DelegatorLifecycle)(nil)

func (d *DelegatorLifecycle) Type() Type {
	return TypeDelegator
}

func (d *DelegatorLifecycle) Status() Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status
}

func (d *DelegatorLifecycle) Info() *Info {
	d.mu.Lock()
	defer d.mu.Unlock()

	return &Info{
		Type:            d.Type(),
		ID:              d.ID(),
		Status:          d.status,
		QueuedReceipt:   d.queuedReceipt,
		ActivatedBlock:  d.activatedBlock,
		WithdrawReceipt: d.withdrawReceipt,
		ExitReceipt:     d.exitReceipt,
		ValidationID:    d.validationID,
	}
}

func (d *DelegatorLifecycle) ID() string {
	if d.id.IsZero() {
		return "n/a"
	}
	return big.NewInt(0).SetBytes(d.id[:]).String()
}

func (d *DelegatorLifecycle) Process(engine *Engine, block uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status == StatusUnknown {
		d.status = StatusPending
	}

	if d.queuedReceipt == nil || d.id.IsZero() {
		return d.queue(engine, block)
	}
	delegation, err := engine.stack.Staker().GetDelegation(d.id)
	if err != nil {
		slog.Error("failed to get delegation", "error", err, "id", d.ID())
		return err
	}
	if d.activatedBlock == 0 && delegation.Locked {
		return d.setActivatedBlock(engine, delegation, block)
	}
	lastActiveBlock := d.activatedBlock + (d.StakingPeriods * engine.stack.Config().EpochLength) + 1
	if block < lastActiveBlock {
		return nil
	}
	if delegation.AutoRenew && block >= lastActiveBlock {
		return d.exit(engine)
	}
	firstWithdrawBlock := lastActiveBlock +
		d.WithdrawDelay.Blocks +
		(d.WithdrawDelay.Epochs * engine.stack.Config().EpochLength)
	if d.exitReceipt != nil && // must have signalled exit
		d.exitReceipt.Meta.BlockNumber+engine.stack.Config().MinStakingPeriod > block && // min block after signalling exit
		block >= firstWithdrawBlock { // must be after withdraw delay
		return d.withdraw(engine)
	}
	return nil
}

func (d *DelegatorLifecycle) setActivatedBlock(engine *Engine, delegation *builtin.Delegation, block uint32) error {
	validator, err := engine.stack.Staker().Get(delegation.ValidationID)
	if err != nil {
		slog.Error("failed to get validator for delegation", "error", err, "id", d.ID())
		return err
	}
	slog.Info("delegation activated", "id", d.ID(), "block", block)

	d.status = StatusActive
	d.activatedBlock = validator.StartBlock +
		(delegation.StartPeriod * engine.stack.Config().MinStakingPeriod)

	return nil
}

func (d *DelegatorLifecycle) queue(engine *Engine, block uint32) error {
	firstQueueBlock := d.StartBlock +
		d.QueueDelay.Blocks +
		(d.QueueDelay.Epochs * engine.stack.Config().EpochLength)
	if block < firstQueueBlock {
		return nil
	}

	validation, validationID := engine.validators.RandomActiveAutoRenewValidator()
	if validationID.IsZero() {
		return nil
	}
	slog.Info("queuing delegator", "validation", validation.Master.String())

	position := delegations.RandomPosition()
	eth := big.NewInt(1e18)
	stake := big.NewInt(0).Mul(position.Stake, eth)
	sender := engine.stack.Staker().AddDelegation(validationID, stake, true, position.Multiplier)
	receipt, err := engine.stack.SendTransaction(sender, d.Account)
	if err != nil {
		slog.Error("failed to queue delegator", "error", err)
		return err
	}

	d.validationID = validationID
	d.id = receipt.Outputs[0].Events[0].Topics[2]
	d.queuedReceipt = receipt
	d.status = StatusQueued

	slog.Info("delegator queued", "id", d.ID())
	return nil
}

func (d *DelegatorLifecycle) exit(engine *Engine) error {
	if d.status == StatusExitSignalled {
		slog.Warn("delegator already exit signalled", "id", d.ID())
		return nil
	}

	slog.Info("signalling exit for delegator", "id", d.ID())

	sender := engine.stack.Staker().UpdateDelegationAutoRenew(d.id, false)
	receipt, err := engine.stack.SendTransaction(sender, d.Account)
	if err != nil {
		d.status = StatusError
		slog.Error("failed to signal exit for delegator", "error", err, "id", d.ID())
		return err
	}
	slog.Info("delegator exit signalled", "id", d.ID())
	d.status = StatusExitSignalled
	d.exitReceipt = receipt
	return nil
}

func (d *DelegatorLifecycle) withdraw(engine *Engine) error {
	if d.status != StatusExitSignalled {
		slog.Warn("cannot withdraw delegator that is not exit signalled", "id", d.ID())
		return nil
	}

	slog.Info("withdrawing delegator", "id", d.ID())

	sender := engine.stack.Staker().WithdrawDelegation(d.id)
	receipt, err := engine.stack.SendTransaction(sender, d.Account)
	if err != nil {
		d.status = StatusError
		slog.Error("failed to withdraw delegator", "error", err, "id", d.ID())
		return err
	}
	slog.Info("delegator withdrawn", "id", d.ID())
	d.status = StatusWithdrawn
	d.withdrawReceipt = receipt
	return nil
}
