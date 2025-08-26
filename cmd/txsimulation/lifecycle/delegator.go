package lifecycle

import (
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/delegations"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/validators"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/builtin/staker/validation"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/httpclient"
)

type transaction struct {
	id      thor.Bytes32
	receipt *api.Receipt
}

type DelegatorLifecycle struct {
	config      DelegatorConfig
	delegations *delegations.PositionManager
	stack       *stack.Stack
	validations *validators.Service

	status              Status
	queuedTx            transaction // the receipt of the queued transaction
	exitTx              transaction // the receipt of the exit transaction
	withdrawTw          transaction // the receipt of the withdraw transaction
	activatedBlock      uint32      // the block at which this lifecycle was activated
	stakingPeriodLength uint32
	id                  *big.Int
	startPeriod         uint32

	mu sync.Mutex
}

func NewDelegatorLifecycle(
	config DelegatorConfig,
	delegations *delegations.PositionManager,
	validations *validators.Service,
	stack *stack.Stack,
) *DelegatorLifecycle {
	return &DelegatorLifecycle{
		config:      config,
		delegations: delegations,
		validations: validations,
		stack:       stack,
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
		ValidationID:    d.config.ValidationID,
	}
}

func (d *DelegatorLifecycle) ID() string {
	if d.id == nil {
		return ""
	}
	return d.id.String()
}

func (d *DelegatorLifecycle) Process(block uint32) error {
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

	validator, ok := d.validations.LookupAddress(d.config.ValidationID)
	if !ok || validator == nil {
		slog.Error("failed to get staking period info for validation", "id", d.config.ValidationID)
		return errors.New("failed to get staking period info for validation")
	}
	if validator.Status == validation.StatusExit || validator.ExitBlock != nil {
		d.status = StatusWithdrawn
		return nil
	}

	d.stakingPeriodLength = validator.Period

	eth := big.NewInt(1e18)
	stake := big.NewInt(0).Mul(d.config.Position.Stake, eth)
	sender := d.stack.Staker().AddDelegation(d.config.ValidationID, stake, d.config.Position.Multiplier)

	receipt, err := d.sendOrPoll(sender, &d.queuedTx.id, "validation is not queued or active")
	if err != nil {
		slog.Error("failed to queue delegator", "error", err, "id", d.ID())
		return err
	}
	if receipt != nil {
		if receipt.Reverted { // `validation is not queued or active`
			d.status = StatusWithdrawn
			return nil
		}

		id := big.NewInt(0).SetBytes(receipt.Outputs[0].Events[0].Topics[2][:])
		delegation, err := d.stack.Staker().GetDelegationPeriodDetails(id)
		if err != nil {
			slog.Error("failed to get delegation period details for queued delegator", "error", err, "id", d.ID())
			return errors.Wrap(err, fmt.Sprintf("failed to get delegation period details for ID %s", d.ID()))
		}

		d.queuedTx.receipt = receipt
		d.startPeriod = delegation.StartPeriod
		d.id = id
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
	validator, ok := d.validations.LookupAddress(d.config.ValidationID)
	if !ok {
		slog.Error("failed to get validator for delegation", "id", d.ID())
		return fmt.Errorf("failed to get validator for delegation %s", d.ID())
	}
	slog.Debug("delegation activated", "id", d.ID(), "block", block)
	validatorCurrent := validator.CompleteIterations - 1
	if d.startPeriod >= validatorCurrent {
		d.status = StatusActive
		d.activatedBlock = validator.StartBlock + (d.startPeriod * d.stakingPeriodLength)
	}

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
	validator, ok := d.validations.LookupAddress(d.config.ValidationID)
	if !ok {
		slog.Error("failed to get validator for delegation", "id", d.ID())
		return fmt.Errorf("failed to get validator for delegation %s", d.ID())
	}
	lastActiveBlock := d.config.MinExitBlock(d.activatedBlock, d.stakingPeriodLength)
	if block < lastActiveBlock && validator.Status < validation.StatusExit {
		return nil
	}
	slog.Debug("signalling exit for delegator", "id", d.ID())

	sender := d.stack.Staker().SignalDelegationExit(d.id)
	receipt, err := d.sendOrPoll(sender, &d.exitTx.id, "delegation has ended", "delegation has not started yet")
	if err != nil {
		slog.Error("failed to signal exit for delegator", "error", err, "id", d.ID())
		return err
	}
	if receipt != nil {
		d.status = StatusExitSignalled
		d.exitTx.receipt = receipt
		d.delegations.UnregisterDelegator(d.config.Position, d.config.ValidationID)
	}

	return nil
}

func (d *DelegatorLifecycle) ProcessExited(block uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.withdrawTw.receipt != nil {
		return nil
	}
	if d.exitTx.receipt == nil || d.status != StatusExitSignalled {
		return fmt.Errorf("cannot withdraw delegator that has not signalled exit: %s", d.ID())
	}
	minWithdrawBlock := d.config.MinWithdrawBlock(d.exitTx.receipt.Meta.BlockNumber, d.stack.Config())
	if block < minWithdrawBlock {
		return nil
	}
	slog.Debug("withdrawing delegator", "id", d.ID())

	sender := d.stack.Staker().WithdrawDelegation(d.id)
	receipt, err := d.sendOrPoll(sender, &d.withdrawTw.id)
	if err != nil {
		slog.Error("failed to withdraw delegator", "error", err, "id", d.ID())
		return err
	}
	if receipt != nil {
		d.status = StatusWithdrawn
		d.withdrawTw.receipt = receipt
	}

	return nil
}

func (d *DelegatorLifecycle) sendOrPoll(
	sender *bind.MethodBuilder,
	txID *thor.Bytes32,
	allowedReverts ...string,
) (*api.Receipt, error) {
	if txID.IsZero() {
		trx, err := d.stack.SendTransaction(sender, d.config.Account)
		if err != nil {
			return nil, err
		}
		*txID = trx.ID()
		return nil, nil
	}

	receipt, err := d.stack.Client().TransactionReceipt(txID)
	if err != nil {
		if errors.Is(err, httpclient.ErrNotFound) {
			return nil, nil
		}
		slog.Error("failed to get transaction receipt", "error", err, "id", txID)
		return nil, err
	}

	if receipt.Reverted {
		revertErr := utils.DebugRevert(sender, receipt)
		if revertErr != nil {
			for _, allowed := range allowedReverts {
				if strings.Contains(revertErr.Error(), allowed) {
					return receipt, nil
				}
			}
		}
		slog.Warn("transaction was reverted", "id", txID, "err", revertErr)
		*txID = thor.Bytes32{} // reset txID to indicate that the transaction was reverted
		return nil, nil
	}

	return receipt, nil
}
