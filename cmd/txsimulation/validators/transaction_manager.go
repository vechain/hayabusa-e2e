package validators

import (
	"math/big"
	"strings"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
)

// TransactionManager - handles all transaction-related operations
type TransactionManager struct {
	stack   *stack.Stack
	account *hayabusa.NodePair

	queuedReceipt   *api.Receipt
	exitReceipt     *api.Receipt
	withdrawReceipt *api.Receipt
}

func NewTransactionManager(stack *stack.Stack, account *hayabusa.NodePair) *TransactionManager {
	return &TransactionManager{
		stack:   stack,
		account: account,
	}
}

func (tm *TransactionManager) QueuedReceipt() *api.Receipt {
	return tm.queuedReceipt
}

func (tm *TransactionManager) ExitReceipt() *api.Receipt {
	return tm.exitReceipt
}

func (tm *TransactionManager) WithdrawReceipt() *api.Receipt {
	return tm.withdrawReceipt
}

func (tm *TransactionManager) SetQueuedReceipt(receipt *api.Receipt) {
	tm.queuedReceipt = receipt
}

func (tm *TransactionManager) SetExitReceipt(receipt *api.Receipt) {
	tm.exitReceipt = receipt
}

func (tm *TransactionManager) QueueValidator(validatorID thor.Address, stake *big.Int, periodLength uint32) error {
	if tm.queuedReceipt != nil {
		return nil // Already queued
	}

	method := tm.stack.Staker().AddValidation(validatorID, stake, periodLength)
	receipt, err := tm.stack.SendTransactionAndWait(method, tm.account.Endorser)
	if err != nil {
		if strings.Contains(err.Error(), "validator already exists") {
			return err // Let caller handle this case
		}
		return err
	}

	tm.queuedReceipt = receipt
	return nil
}

func (tm *TransactionManager) SignalExit(validatorID thor.Address) error {
	if tm.exitReceipt != nil {
		return nil // Already signaled exit
	}

	method := tm.stack.Staker().SignalExit(validatorID)
	receipt, err := tm.stack.SendTransactionAndWait(method, tm.account.Endorser)
	tm.exitReceipt = receipt
	if err != nil {
		return err
	}

	return nil
}

func (tm *TransactionManager) WithdrawStake(validatorID thor.Address) error {
	if tm.withdrawReceipt != nil {
		return nil // Already withdrawn
	}

	method := tm.stack.Staker().WithdrawStake(validatorID)
	receipt, err := tm.stack.SendTransactionAndWait(method, tm.account.Endorser)
	if err != nil {
		return err
	}

	tm.withdrawReceipt = receipt
	return nil
}
