package validators

import (
	"log/slog"
	"math"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

// ValidatorStateManager - handles state transitions only
type ValidatorStateManager struct {
	validatorID    thor.Address
	status         lifecycle.Status
	activatedBlock uint32
	firstProcessed bool
	stack          *stack.Stack
}

func NewValidatorStateManager(validatorID thor.Address, stack *stack.Stack) *ValidatorStateManager {
	return &ValidatorStateManager{
		validatorID: validatorID,
		status:      lifecycle.StatusPending,
		stack:       stack,
	}
}

func (sm *ValidatorStateManager) Status() lifecycle.Status {
	return sm.status
}

func (sm *ValidatorStateManager) ActivatedBlock() uint32 {
	return sm.activatedBlock
}

func (sm *ValidatorStateManager) RefreshState(eventHandler *ValidatorEventHandler, txManager *TransactionManager) error {
	if !sm.firstProcessed {
		sm.firstProcessed = true
		slog.Info("starting validator lifecycle", "account", sm.validatorID)
		return sm.performInitialStateCheck(eventHandler, txManager)
	}
	return nil
}

func (sm *ValidatorStateManager) TransitionTo(newStatus lifecycle.Status, activatedBlock uint32) {
	sm.status = newStatus
	if activatedBlock > 0 {
		sm.activatedBlock = activatedBlock
	}
}

func (sm *ValidatorStateManager) performInitialStateCheck(eventHandler *ValidatorEventHandler, txManager *TransactionManager) error {
	existing, err := eventHandler.CheckValidatorStatus()
	if err != nil {
		slog.Error("failed to check existing validator", "error", err, "account", sm.validatorID)
		return err
	}
	if existing.Exists() {
		slog.Info("validator already exists, checking for queued receipt", "account", sm.validatorID)
		if err := sm.HandleExistingValidator(eventHandler, txManager); err != nil {
			return err
		}
	}
	if existing.Status == builtin.StakerStatusActive {
		periodDetails, err := sm.stack.Staker().GetValidationPeriodDetails(sm.validatorID)
		if err != nil {
			slog.Error("failed to get staking period info for existing validator", "error", err, "account", sm.validatorID)
			return err
		}
		slog.Info("validator is already active", "account", sm.validatorID, "startBlock", periodDetails.StartBlock)
		sm.activatedBlock = periodDetails.StartBlock
		sm.status = lifecycle.StatusActive
	}

	if txManager.QueuedReceipt() != nil {
		slog.Info("checking if validator is active", "account", sm.validatorID)
		if err := sm.CheckAlreadyExited(eventHandler); err != nil {
			return err
		}
	}

	return nil
}

func (sm *ValidatorStateManager) HandleExistingValidator(eventHandler *ValidatorEventHandler, txManager *TransactionManager) error {
	periodDetails, err := sm.stack.Staker().GetValidationPeriodDetails(sm.validatorID)
	if err != nil {
		slog.Warn("failed to get staking period info for existing validator", "id", sm.validatorID)
		return nil
	}

	sm.activatedBlock = periodDetails.StartBlock
	sm.status = lifecycle.StatusQueued

	// Find the queued receipt
	receipt, err := eventHandler.FindQueuedReceipt()
	if err != nil {
		slog.Warn("failed to find queued receipt", "error", err, "id", sm.validatorID)
		return nil
	}

	txManager.SetQueuedReceipt(receipt)
	return nil
}

func (sm *ValidatorStateManager) CheckAlreadyExited(eventHandler *ValidatorEventHandler) error {
	validator, err := eventHandler.CheckValidatorStatus()
	if err != nil {
		slog.Error("failed to get validator info", "error", err, "account", sm.validatorID)
		return err
	}
	if !validator.Exists() {
		slog.Info("validator does not exist, marking as withdrawn", "account", sm.validatorID)
		sm.status = lifecycle.StatusWithdrawn
		return nil
	}
	periodDetails, err := sm.stack.Staker().GetValidationPeriodDetails(sm.validatorID)
	if err != nil {
		slog.Error("failed to get validator period details", "error", err, "account", sm.validatorID)
		return err
	}
	if validator.Status == builtin.StakerStatusExited || periodDetails.ExitBlock < math.MaxUint32 {
		slog.Info("validator has already exited, marking as withdrawn", "account", sm.validatorID)
		sm.status = lifecycle.StatusWithdrawn
		return nil
	}

	// Check for existing exit receipt
	exitReceipt, err := eventHandler.FindExitReceipt()
	if err != nil {
		slog.Error("failed to find exit receipt", "error", err, "account", sm.validatorID)
		return err
	}
	if exitReceipt != nil {
		sm.status = lifecycle.StatusExitSignalled
		// Set the exit receipt in transaction manager would need to be exposed
	}

	return nil
}
