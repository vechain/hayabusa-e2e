package delegations

import (
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/contract"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/xnodes"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/builtin/staker/validation"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/httpclient"
)

// Constants for error messages and transaction handling
const (
	// Error messages for delegation operations
	errValidationNotQueuedOrActive = "validation is not queued or active"
	errDelegationHasEnded          = "delegation has ended"
	errDelegationNotStarted        = "delegation has not started yet"

	// ETH conversion constant
	ethToWei = 1e18
)

// transaction represents a blockchain transaction with its receipt and polling state
type transaction struct {
	id           thor.Bytes32
	receipt      *api.Receipt
	pollAttempts uint32
}

// reset clears the transaction state for reuse
func (t *transaction) reset() {
	t.id = thor.Bytes32{}
	t.receipt = nil
	t.pollAttempts = 0
}

// DelegatorLifecycle manages the complete lifecycle of a delegation from pending to withdrawn
type DelegatorLifecycle struct {
	// Configuration and dependencies
	config          Config
	xnodes          *xnodes.PositionManager
	stack           *stack.Stack
	contractService *contract.Service

	// State management
	status              lifecycle.Status
	activatedBlock      uint32
	stakingPeriodLength uint32
	id                  *big.Int
	startPeriod         uint32

	// Transaction tracking
	queuedTx   transaction // tracks the queuing transaction
	exitTx     transaction // tracks the exit signal transaction
	withdrawTx transaction // tracks the withdrawal transaction

	mu sync.Mutex
}

// NewDelegatorLifecycle creates a new delegator lifecycle instance
func NewDelegatorLifecycle(
	config Config,
	xnodes *xnodes.PositionManager,
	contractService *contract.Service,
	stack *stack.Stack,
) *DelegatorLifecycle {
	return &DelegatorLifecycle{
		config:          config,
		xnodes:          xnodes,
		contractService: contractService,
		stack:           stack,
		status:          lifecycle.StatusPending,
	}
}

var _ lifecycle.Lifecycle = (*DelegatorLifecycle)(nil)

// Type returns the lifecycle type (delegator)
func (d *DelegatorLifecycle) Type() lifecycle.Type {
	return lifecycle.TypeDelegator
}

// Status returns the current status of the delegator lifecycle
func (d *DelegatorLifecycle) Status() lifecycle.Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status
}

// Info returns comprehensive information about the delegator lifecycle
func (d *DelegatorLifecycle) Info() *lifecycle.Info {
	d.mu.Lock()
	defer d.mu.Unlock()

	return &lifecycle.Info{
		Type:            d.Type(),
		ID:              d.ID(),
		Status:          d.status,
		QueuedReceipt:   d.queuedTx.receipt,
		ActivatedBlock:  d.activatedBlock,
		WithdrawReceipt: d.withdrawTx.receipt,
		ExitReceipt:     d.exitTx.receipt,
		ValidationID:    d.config.ValidationID,
	}
}

// ID returns the string representation of the delegation ID
func (d *DelegatorLifecycle) ID() string {
	if d.id == nil {
		return ""
	}
	return d.id.String()
}

// Process handles the delegation lifecycle based on current status and block
func (d *DelegatorLifecycle) Process(block uint32) error {
	// Ensure cleanup when delegation is complete
	defer d.cleanupIfComplete()

	switch d.status {
	case lifecycle.StatusPending:
		return d.processPendingState(block)
	case lifecycle.StatusQueued:
		return d.processQueuedState(block)
	case lifecycle.StatusActive:
		return d.processActiveState(block)
	case lifecycle.StatusExitSignalled:
		return d.processExitedState(block)
	case lifecycle.StatusWithdrawn:
		// No processing needed for withdrawn delegations
		return nil
	}

	return nil
}

// cleanupIfComplete unregisters the delegator from xnodes when lifecycle is complete
func (d *DelegatorLifecycle) cleanupIfComplete() {
	if d.status == lifecycle.StatusExitSignalled || d.status == lifecycle.StatusWithdrawn {
		d.xnodes.UnregisterDelegator(d.config.PositionID)
	}
}

// processPendingState handles the pending state - queuing the delegation
func (d *DelegatorLifecycle) processPendingState(block uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Skip if already processed or too early
	if d.queuedTx.receipt != nil || block < d.config.QueueBlock(d.stack.Config()) {
		return nil
	}

	// Validate the target validation
	validator, err := d.getValidatorInfo()
	if err != nil {
		return err
	}

	// Check if validation is still available for delegation
	if d.isValidationUnavailable(validator) {
		d.status = lifecycle.StatusWithdrawn
		return nil
	}

	return d.queueDelegation(validator)
}

// processQueuedState handles the queued state - waiting for activation
func (d *DelegatorLifecycle) processQueuedState(block uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Skip if not queued or already activated
	if d.status < lifecycle.StatusQueued || d.activatedBlock != 0 {
		return nil
	}

	validator, err := d.getValidatorInfo()
	if err != nil {
		return err
	}

	return d.checkForActivation(validator)
}

// processActiveState handles the active state - signaling exit when appropriate
func (d *DelegatorLifecycle) processActiveState(block uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Skip if already exit signalled or receipt exists
	if d.status == lifecycle.StatusExitSignalled || d.exitTx.receipt != nil {
		return nil
	}

	validator, err := d.getValidatorInfo()
	if err != nil {
		return err
	}

	// Check if it's time to exit
	if !d.shouldSignalExit(block, validator) {
		return nil
	}

	return d.signalExit()
}

// processExitedState handles the exit signalled state - withdrawing when possible
func (d *DelegatorLifecycle) processExitedState(block uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Skip if already withdrawn or no exit transaction
	if d.withdrawTx.receipt != nil || !d.canWithdraw() {
		return nil
	}

	// Check if enough time has passed since exit
	minWithdrawBlock := d.config.MinWithdrawBlock(d.exitTx.receipt.Meta.BlockNumber, d.stack.Config())
	if block < minWithdrawBlock {
		return nil
	}

	return d.withdrawDelegation()
}

// Helper methods for cleaner code organization

// getValidatorInfo retrieves validator information from contract service
func (d *DelegatorLifecycle) getValidatorInfo() (*validation.Validation, error) {
	validator, ok := d.contractService.LookupAddress(d.config.ValidationID)
	if !ok || validator == nil {
		return nil, fmt.Errorf("failed to get validator info for validation %s", d.config.ValidationID)
	}
	return validator, nil
}

// isValidationUnavailable checks if validation is no longer available for delegation
func (d *DelegatorLifecycle) isValidationUnavailable(validator *validation.Validation) bool {
	return validator.Status == validation.StatusExit || validator.ExitBlock != nil
}

// queueDelegation creates and sends the delegation queuing transaction
func (d *DelegatorLifecycle) queueDelegation(validator *validation.Validation) error {
	d.stakingPeriodLength = validator.Period

	// Calculate stake amount in wei
	stake := new(big.Int).Mul(d.config.Position.Stake, big.NewInt(ethToWei))

	// Create delegation transaction
	sender := d.stack.Staker().AddDelegation(d.config.ValidationID, stake, d.config.Position.WeightMultiplier)

	receipt, err := d.sendOrPoll(sender, &d.queuedTx, errValidationNotQueuedOrActive)
	if err != nil {
		slog.Error("failed to queue delegator", "error", err, "id", d.ID())
		return err
	}

	if receipt != nil {
		if err := d.handleQueueReceipt(receipt); err != nil {
			return err
		}
	}

	slog.Debug("delegator queued", "id", d.ID())
	return nil
}

// handleQueueReceipt processes the queuing transaction receipt
func (d *DelegatorLifecycle) handleQueueReceipt(receipt *api.Receipt) error {
	if receipt.Reverted {
		d.status = lifecycle.StatusWithdrawn
		return nil
	}

	// Extract delegation ID from transaction logs
	delegationID := new(big.Int).SetBytes(receipt.Outputs[0].Events[0].Topics[2][:])

	// Get delegation details
	delegation, err := d.stack.Staker().GetDelegationPeriodDetails(delegationID)
	if err != nil {
		slog.Error("failed to get delegation period details", "error", err, "id", delegationID)
		return errors.Wrap(err, fmt.Sprintf("failed to get delegation period details for ID %s", delegationID))
	}

	// Update state
	d.queuedTx.receipt = receipt
	d.startPeriod = delegation.StartPeriod
	d.id = delegationID
	d.status = lifecycle.StatusQueued

	return nil
}

// checkForActivation determines if the delegation should be activated
func (d *DelegatorLifecycle) checkForActivation(validator *validation.Validation) error {
	validatorCurrentPeriod := validator.CompleteIterations - 1

	if d.startPeriod >= validatorCurrentPeriod {
		d.status = lifecycle.StatusActive
		d.activatedBlock = validator.StartBlock + (d.startPeriod * d.stakingPeriodLength)
		slog.Debug("delegation activated", "id", d.ID(), "block", d.activatedBlock)
	}

	return nil
}

// shouldSignalExit determines if it's time to signal exit for the delegation
func (d *DelegatorLifecycle) shouldSignalExit(block uint32, validator *validation.Validation) bool {
	lastActiveBlock := d.config.MinExitBlock(d.activatedBlock, d.stakingPeriodLength)
	return block >= lastActiveBlock || validator.Status >= validation.StatusExit
}

// signalExit creates and sends the exit signal transaction
func (d *DelegatorLifecycle) signalExit() error {
	slog.Debug("signalling exit for delegator", "id", d.ID())

	sender := d.stack.Staker().SignalDelegationExit(d.id)
	receipt, err := d.sendOrPoll(sender, &d.exitTx, errDelegationHasEnded, errDelegationNotStarted)

	if err != nil {
		slog.Error("failed to signal exit for delegator", "error", err, "id", d.ID())
		return err
	}

	if receipt != nil {
		d.status = lifecycle.StatusExitSignalled
		d.exitTx.receipt = receipt
	}

	return nil
}

// canWithdraw checks if the delegation can be withdrawn
func (d *DelegatorLifecycle) canWithdraw() bool {
	return d.exitTx.receipt != nil && d.status == lifecycle.StatusExitSignalled
}

// withdrawDelegation creates and sends the withdrawal transaction
func (d *DelegatorLifecycle) withdrawDelegation() error {
	if !d.canWithdraw() {
		return fmt.Errorf("cannot withdraw delegator that has not signalled exit: %s", d.ID())
	}

	slog.Debug("withdrawing delegator", "id", d.ID())

	sender := d.stack.Staker().WithdrawDelegation(d.id)
	receipt, err := d.sendOrPoll(sender, &d.withdrawTx)

	if err != nil {
		slog.Error("failed to withdraw delegator", "error", err, "id", d.ID())
		return err
	}

	if receipt != nil {
		d.status = lifecycle.StatusWithdrawn
		d.withdrawTx.receipt = receipt
	}

	return nil
}

// sendOrPoll handles sending a transaction or polling for its receipt
func (d *DelegatorLifecycle) sendOrPoll(
	sender *bind.MethodBuilder,
	tx *transaction,
	allowedReverts ...string,
) (*api.Receipt, error) {
	// Send transaction if not already sent
	if tx.id.IsZero() {
		return d.sendTransaction(sender, tx)
	}

	// Poll for receipt if transaction already sent
	return d.pollTransactionReceipt(tx, sender, allowedReverts)
}

// sendTransaction sends a new transaction
func (d *DelegatorLifecycle) sendTransaction(sender *bind.MethodBuilder, tx *transaction) (*api.Receipt, error) {
	trx, err := d.stack.SendTransaction(sender, d.config.Account)
	if err != nil {
		return nil, err
	}
	tx.id = trx.ID()
	return nil, nil
}

// pollTransactionReceipt polls for a transaction receipt with retry logic
func (d *DelegatorLifecycle) pollTransactionReceipt(
	tx *transaction,
	sender *bind.MethodBuilder,
	allowedReverts []string,
) (*api.Receipt, error) {
	receipt, err := d.stack.Client().TransactionReceipt(&tx.id)
	if err != nil {
		return d.handleReceiptError(tx, err)
	}

	return d.processReceipt(receipt, tx, sender, allowedReverts)
}

// handleReceiptError handles errors when fetching transaction receipts
func (d *DelegatorLifecycle) handleReceiptError(tx *transaction, err error) (*api.Receipt, error) {
	// Check if we've exceeded max polling attempts
	if tx.pollAttempts > *d.stack.DefaultExpiration() {
		slog.Warn("exceeded max polling attempts for transaction", "id", tx.id)
		tx.reset()
		return nil, nil
	}

	tx.pollAttempts++

	// Handle "not found" errors (transaction still pending)
	if errors.Is(err, httpclient.ErrNotFound) {
		return nil, nil
	}

	slog.Error("failed to get transaction receipt", "error", err, "id", tx.id)
	return nil, err
}

// processReceipt processes a successfully retrieved receipt
func (d *DelegatorLifecycle) processReceipt(
	receipt *api.Receipt,
	tx *transaction,
	sender *bind.MethodBuilder,
	allowedReverts []string,
) (*api.Receipt, error) {
	if !receipt.Reverted {
		return receipt, nil
	}

	// Handle reverted transaction
	revertErr := utils.DebugRevert(sender, receipt)
	if d.isAllowedRevert(revertErr, allowedReverts) {
		return receipt, nil
	}

	slog.Warn("transaction was reverted", "id", tx.id, "error", revertErr)
	tx.reset()
	return nil, nil
}

// isAllowedRevert checks if a revert error is in the allowed list
func (d *DelegatorLifecycle) isAllowedRevert(revertErr error, allowedReverts []string) bool {
	if revertErr == nil {
		return false
	}

	errorMsg := revertErr.Error()
	for _, allowed := range allowedReverts {
		if strings.Contains(errorMsg, allowed) {
			return true
		}
	}
	return false
}
