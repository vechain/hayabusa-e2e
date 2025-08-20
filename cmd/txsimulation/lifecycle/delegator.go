package lifecycle

import (
	"fmt"
	"log/slog"
	"math/big"
	"sync"

	"github.com/pkg/errors"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/delegations"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
	"github.com/vechain/thor/v2/thorclient/httpclient"
)

type transaction struct {
	id      thor.Bytes32
	receipt *api.Receipt
}

type DelegatorLifecycle struct {
	config       DelegatorConfig
	delegations  *delegations.PositionManager
	stack        *stack.Stack
	position     *delegations.Position
	validationID thor.Address

	status              Status
	queuedTx            transaction // the receipt of the queued transaction
	exitTx              transaction // the receipt of the exit transaction
	withdrawTw          transaction // the receipt of the withdraw transaction
	activatedBlock      uint32      // the block at which this lifecycle was activated
	stakingPeriodLength uint32
	id                  *big.Int

	mu sync.Mutex
}

func NewDelegatorLifecycle(
	base DelegatorConfig,
	delegations *delegations.PositionManager,
	stack *stack.Stack,
	position *delegations.Position,
	validationID thor.Address,
) *DelegatorLifecycle {
	return &DelegatorLifecycle{
		config:       base,
		delegations:  delegations,
		stack:        stack,
		position:     position,
		validationID: validationID,
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
		QueuedReceipt:   d.queuedTx.receipt,
		ActivatedBlock:  d.activatedBlock,
		WithdrawReceipt: d.withdrawTw.receipt,
		ExitReceipt:     d.exitTx.receipt,
		ValidationID:    d.validationID,
	}
}

func (d *DelegatorLifecycle) ID() string {
	if d.id == nil {
		return ""
	}
	return d.id.String()
}

func (d *DelegatorLifecycle) Process(block uint32) error {
	defer func() {
		if d.status == StatusWithdrawn && d.position != nil {
			d.delegations.UnregisterDelegator(d.position, d.validationID)
		}
	}()
	switch d.status {
	case StatusPending:
		return d.ProcessPending(block)
	case StatusQueued:
		return d.ProcessQueued(block)
	case StatusActive:
		return d.ProcessActive(block)
	case StatusExitSignalled:
		return d.ProcessExited(block)
	case StatusWithdrawn:

	}

	return nil
}

func (d *DelegatorLifecycle) ProcessPending(block uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.queuedTx.receipt != nil {
		return nil
	}
	if block < d.config.QueueBlock(d.stack.Config()) {
		return nil
	}

	stakingPeriodInfo, err := d.stack.Staker().GetValidatorPeriodDetails(d.validationID)
	if err != nil {
		slog.Error("failed to get staking period info for validation", "error", err, "id", d.validationID)
		return err
	}

	d.stakingPeriodLength = stakingPeriodInfo.Period

	if d.queuedTx.id.IsZero() { // send the transaction
		eth := big.NewInt(1e18)
		stake := big.NewInt(0).Mul(d.position.Stake, eth)
		sender := d.stack.Staker().AddDelegation(d.validationID, stake, d.position.Multiplier)
		tx, err := d.stack.SendTransaction(sender, d.config.Account)
		if err != nil {
			slog.Error("failed to queue delegator", "error", err)
			return err
		}
		d.queuedTx.id = tx.ID()
	} else { // poll for the receipt
		receipt, err := d.stack.Client().TransactionReceipt(&d.queuedTx.id)
		if err != nil {
			if errors.Is(err, httpclient.ErrNotFound) {
				return nil
			}
			slog.Error("failed to get queued delegator transaction", "error", err, "id", d.queuedTx.id)
			return err
		}
		if receipt.Reverted {
			d.queuedTx.id = thor.Bytes32{} // reset the ID if the transaction was reverted
			d.position = nil
			d.validationID = thor.Address{}
			d.stakingPeriodLength = 0
			return nil
		}
		d.queuedTx.receipt = receipt
		d.id = big.NewInt(0).SetBytes(receipt.Outputs[0].Events[0].Topics[2][:])
		d.status = StatusQueued
	}

	slog.Debug("delegator queued", "id", d.ID())
	return nil
}

func (d *DelegatorLifecycle) ProcessQueued(block uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.status < StatusQueued {
		return fmt.Errorf("cannot set activated block for delegator that is not queued: %s", d.ID())
	}
	if d.activatedBlock != 0 {
		return nil // already activated
	}
	delegation, err := d.stack.Staker().GetDelegationPeriodDetails(d.id)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to get delegation for ID %s", d.ID()))
	}
	if !delegation.Locked {
		return nil // not locked, no activation needed
	}
	validator, err := d.stack.Staker().GetValidatorPeriodDetails(d.validationID)
	if err != nil {
		slog.Error("failed to get validator for delegation", "error", err, "id", d.ID())
		return err
	}
	slog.Debug("delegation activated", "id", d.ID(), "block", block)

	d.status = StatusActive
	d.activatedBlock = validator.StartBlock + (delegation.StartPeriod * d.stakingPeriodLength)

	return nil
}

func (d *DelegatorLifecycle) ProcessActive(block uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.exitTx.receipt != nil {
		return nil
	}
	if d.status == StatusExitSignalled {
		slog.Warn("delegator already exit signalled", "id", d.ID())
		return nil
	}
	validation, err := d.stack.Staker().GetValidatorStatus(d.validationID)
	if err != nil {
		slog.Error("failed to get validator for delegation", "error", err, "id", d.ID())
		return err
	}
	lastActiveBlock := d.config.MinExitBlock(d.activatedBlock, d.stakingPeriodLength)
	if block < lastActiveBlock && validation.Status < builtin.StakerStatusExited {
		return nil
	}
	slog.Debug("signalling exit for delegator", "id", d.ID())

	sender := d.stack.Staker().SignalDelegationExit(d.id)

	if d.exitTx.id.IsZero() { // send the tx
		tx, err := d.stack.SendTransaction(sender, d.config.Account)
		if err != nil {

			slog.Error("failed to signal exit for delegator", "error", err, "id", d.ID())
			return err
		}
		d.exitTx.id = tx.ID()
	} else { // poll for it
		receipt, err := d.stack.Client().TransactionReceipt(&d.exitTx.id)
		if err != nil {
			if errors.Is(err, httpclient.ErrNotFound) {
				return nil
			}
			slog.Error("failed to get exit transaction receipt for delegator", "error", err, "id", d.ID())
			return err
		}
		if receipt.Reverted {
			slog.Warn("exit transaction for delegator was reverted", "id", d.ID())
			d.exitTx.id = thor.Bytes32{} // reset the ID if the transaction was reverted
			return nil
		}
		d.exitTx.receipt = receipt
		d.status = StatusExitSignalled
	}

	return nil
}

func (d *DelegatorLifecycle) ProcessExited(block uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.withdrawTw.receipt != nil {
		return nil
	}
	if d.exitTx.receipt == nil {
		return fmt.Errorf("cannot withdraw delegator that has not signalled exit: %s", d.ID())
	}
	if d.status != StatusExitSignalled {
		return fmt.Errorf("cannot withdraw delegator that is not exit signalled: %s", d.ID())
	}
	minWithdrawBlock := d.config.MinWithdrawBlock(d.exitTx.receipt.Meta.BlockNumber, d.stack.Config())
	if block < minWithdrawBlock {
		return nil
	}
	slog.Debug("withdrawing delegator", "id", d.ID())

	sender := d.stack.Staker().WithdrawDelegation(d.id)
	if d.withdrawTw.id.IsZero() { // send the tx
		tx, err := d.stack.SendTransaction(sender, d.config.Account)
		if err != nil {
			slog.Error("failed to withdraw delegator", "error", err, "id", d.ID())
			return err
		}
		d.withdrawTw.id = tx.ID()
	} else { // poll the receipt
		receipt, err := d.stack.Client().TransactionReceipt(&d.withdrawTw.id)
		if err != nil {
			if errors.Is(err, httpclient.ErrNotFound) {
				return nil
			}
			slog.Error("failed to get withdraw transaction receipt for delegator", "error", err, "id", d.ID())
			return err
		}
		if receipt.Reverted {
			slog.Warn("withdraw transaction for delegator was reverted", "id", d.ID())
			d.withdrawTw.id = thor.Bytes32{} // reset the ID if the transaction was reverted
			return nil
		}
		d.withdrawTw.receipt = receipt
		d.status = StatusWithdrawn
	}

	return nil
}
