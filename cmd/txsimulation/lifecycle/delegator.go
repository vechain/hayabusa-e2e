package lifecycle

import (
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/delegations"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

type DelegatorLifecycle struct {
	config Config

	status          Status
	queuedReceipt   *api.Receipt // the receipt of the queued transaction
	activatedBlock  uint32       // the block at which this lifecycle was activated
	exitReceipt     *api.Receipt // the receipt of the exit transaction
	withdrawReceipt *api.Receipt // the receipt of the withdraw transaction
	id              thor.Bytes32
	validationID    thor.Address

	mu sync.Mutex
}

func NewDelegatorLifecycle(base Config) *DelegatorLifecycle {
	return &DelegatorLifecycle{
		config: base,
	}
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
	switch d.status {
	case StatusPending:
		return d.ProcessPending(engine, block)
	case StatusQueued:
		return d.ProcessQueued(engine, block)
	case StatusActive:
		return d.ProcessActive(engine, block)
	case StatusExitSignalled:
		return d.ProcessExited(engine, block)
	case StatusWithdrawn:

	}

	return nil
}

func (d *DelegatorLifecycle) ProcessPending(engine *Engine, block uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.queuedReceipt != nil {
		return nil
	}

	firstQueueBlock := d.config.StartBlock +
		d.config.QueueDelay.Blocks +
		(d.config.QueueDelay.Epochs * engine.stack.Config().EpochLength)
	if block < firstQueueBlock {
		return nil
	}

	validation, validationID := engine.validators.RandomActiveValidator()
	if validationID.IsZero() {
		return nil
	}
	slog.Debug("queuing delegator", "validation", validation.Master.String())

	position := delegations.RandomPosition()
	eth := big.NewInt(1e18)
	stake := big.NewInt(0).Mul(position.Stake, eth)
	sender := engine.stack.Staker().AddDelegation(validationID, stake, position.Multiplier)
	receipt, err := engine.stack.SendTransaction(sender, d.config.Account)
	if err != nil {
		slog.Error("failed to queue delegator", "error", err)
		return err
	}

	d.validationID = validationID
	d.id = receipt.Outputs[0].Events[0].Topics[2]
	d.queuedReceipt = receipt
	d.status = StatusQueued

	slog.Debug("delegator queued", "id", d.ID())
	return nil
}

func (d *DelegatorLifecycle) ProcessQueued(engine *Engine, block uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.status < StatusQueued {
		return fmt.Errorf("cannot set activated block for delegator that is not queued: %s", d.ID())
	}
	if d.activatedBlock != 0 {
		return nil // already activated
	}
	delegation, err := engine.stack.Staker().GetDelegation(d.id)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to get delegation for ID %s", d.ID()))
	}
	if !delegation.Locked {
		return nil // not locked, no activation needed
	}
	validator, err := engine.stack.Staker().Get(delegation.ValidationID)
	if err != nil {
		slog.Error("failed to get validator for delegation", "error", err, "id", d.ID())
		return err
	}
	slog.Debug("delegation activated", "id", d.ID(), "block", block)

	d.status = StatusActive
	d.activatedBlock = validator.StartBlock +
		(delegation.StartPeriod * engine.stack.Config().MinStakingPeriod)

	return nil
}

func (d *DelegatorLifecycle) ProcessActive(engine *Engine, block uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.exitReceipt != nil {
		return nil
	}
	if d.status == StatusExitSignalled {
		slog.Warn("delegator already exit signalled", "id", d.ID())
		return nil
	}
	validation, err := engine.stack.Staker().Get(d.validationID)
	if err != nil {
		slog.Error("failed to get validator for delegation", "error", err, "id", d.ID())
		return err
	}
	lastActiveBlock := d.activatedBlock + (d.config.StakingPeriods * engine.stack.Config().MinStakingPeriod) + 1
	if block < lastActiveBlock && validation.Status < builtin.StakerStatusExited {
		return nil
	}
	slog.Debug("signalling exit for delegator", "id", d.ID())

	sender := engine.stack.Staker().SignalDelegationExit(d.id)
	receipt, err := engine.stack.SendTransaction(sender, d.config.Account)
	if err != nil && !strings.Contains(err.Error(), "delegation is not active") {
		slog.Error("failed to signal exit for delegator", "error", err, "id", d.ID())
		return err
	}
	if receipt != nil {
		d.exitReceipt = receipt
	}
	slog.Debug("delegator exit signalled", "id", d.ID())
	d.status = StatusExitSignalled
	return nil
}

func (d *DelegatorLifecycle) ProcessExited(engine *Engine, block uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.withdrawReceipt != nil {
		return nil
	}

	if d.status != StatusExitSignalled {
		return fmt.Errorf("cannot withdraw delegator that is not exit signalled: %s", d.ID())
	}
	if d.exitReceipt == nil {
		return fmt.Errorf("cannot withdraw delegator that has not signalled exit: %s", d.ID())
	}
	minWithdrawBlock := d.exitReceipt.Meta.BlockNumber + engine.stack.Config().MinStakingPeriod +
		d.config.WithdrawDelay.Blocks +
		(d.config.WithdrawDelay.Epochs * engine.stack.Config().EpochLength)
	if block < minWithdrawBlock {
		return nil
	}

	slog.Debug("withdrawing delegator", "id", d.ID())

	sender := engine.stack.Staker().WithdrawDelegation(d.id)
	receipt, err := engine.stack.SendTransaction(sender, d.config.Account)
	if err != nil {
		slog.Error("failed to withdraw delegator", "error", err, "id", d.ID())
		return err
	}
	slog.Debug("delegator withdrawn", "id", d.ID())
	d.status = StatusWithdrawn
	d.withdrawReceipt = receipt
	return nil
}
