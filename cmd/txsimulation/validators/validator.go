package validators

import (
	"errors"
	"log/slog"
	"math/big"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/contract"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/xnodes"
	"github.com/vechain/thor/v2/builtin/staker/validation"
	"github.com/vechain/thor/v2/thor"
)

var (
	Eth     = big.NewInt(1e18) // 1 ETH in wei
	Million = big.NewInt(1e6)  // 1 million VET
)

// RandomStakeBetween generates a random number between min and max.
// It will be scaled to millions of VET.
func RandomStakeBetween(min, max uint8) *big.Int {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	maxStake := big.NewInt(0).Mul(big.NewInt(int64(max)), Million)
	minStake := big.NewInt(0).Mul(big.NewInt(int64(min)), Million)

	rangeStake := new(big.Int).Sub(maxStake, minStake)
	randomOffset := new(big.Int).Rand(rng, rangeStake)
	stake := new(big.Int).Add(minStake, randomOffset)
	stake = stake.Mul(stake, Eth) // Convert to wei
	return stake
}

func RandomStake() *big.Int {
	return RandomStakeBetween(25, 31) // average stake is currently 28m
}

// ValidatorLifecycle - main coordinator with minimal state
type ValidatorLifecycle struct {
	config Config
	id     thor.Address

	// Composed services - each handles one concern
	stateManager    *StateManager
	txManager       *TransactionManager
	stakeManager    *StakeManager
	eventHandler    *EventHandler
	contractService *contract.Service
	xnodes          *xnodes.PositionManager

	mu sync.RWMutex
}

func NewValidatorLifecycle(
	config Config,
	contractService *contract.Service,
	xnodes *xnodes.PositionManager,
	stack *stack.Stack,
	stakingPeriodLength uint32,
) *ValidatorLifecycle {
	id := config.Account.Node.Address()

	return &ValidatorLifecycle{
		config:          config,
		id:              id,
		contractService: contractService,
		xnodes:          xnodes,
		stateManager:    NewStateManager(id, stack),
		txManager:       NewTransactionManager(stack, config.Account),
		stakeManager:    NewStakeManager(stack, id, config.StakeChangeInterval, stakingPeriodLength),
		eventHandler:    NewEventHandler(stack, id),
	}
}

// Implement Lifecycle interface
var _ lifecycle.Lifecycle = (*ValidatorLifecycle)(nil)

func (v *ValidatorLifecycle) ID() string {
	if v.id.IsZero() {
		return "n/a"
	}
	return v.id.String()
}

func (v *ValidatorLifecycle) Type() lifecycle.Type {
	return lifecycle.TypeValidator
}

func (v *ValidatorLifecycle) Status() lifecycle.Status {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.stateManager.Status()
}

func (v *ValidatorLifecycle) Info() *lifecycle.Info {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return &lifecycle.Info{
		Type:            v.Type(),
		ID:              v.ID(),
		Status:          v.stateManager.Status(),
		QueuedReceipt:   v.txManager.QueuedReceipt(),
		ActivatedBlock:  v.stateManager.ActivatedBlock(),
		WithdrawReceipt: v.txManager.WithdrawReceipt(),
		ExitReceipt:     v.txManager.ExitReceipt(),
		ValidationID:    v.id,
	}
}

func (v *ValidatorLifecycle) Process(block uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if err := v.stateManager.RefreshState(v.eventHandler, v.txManager); err != nil {
		return err
	}

	v.handleDelegationRegistration()

	return v.processCurrentState(block)
}

func (v *ValidatorLifecycle) handleDelegationRegistration() {
	validator, ok := v.contractService.LookupAddress(v.id)
	if ok && (validator.Status == validation.StatusExit || validator.OfflineBlock != nil) {
		v.xnodes.UnregisterValidator(v.id)
	}
	if ok && validator.Status == validation.StatusActive {
		v.xnodes.RegisterValidator(v.id)
	}
}

func (v *ValidatorLifecycle) processCurrentState(block uint32) error {
	switch v.stateManager.Status() {
	case lifecycle.StatusPending:
		return v.processPending(block)
	case lifecycle.StatusQueued:
		return v.processQueued(block)
	case lifecycle.StatusActive:
		return v.processActive(block)
	case lifecycle.StatusExitSignalled:
		return v.processExitSignalled(block)
	default:
		return nil
	}
}

func (v *ValidatorLifecycle) processPending(block uint32) error {
	if v.txManager.QueuedReceipt() != nil {
		return nil
	}

	if block < v.config.QueueBlock(v.stateManager.stack.Config()) {
		return nil
	}

	slog.Debug("queuing validator", "account", v.config.Account.Node.Address(), "block", block)

	existing, err := v.eventHandler.CheckValidatorStatus()
	if err != nil {
		slog.Error("failed to check existing validator", "error", err, "account", v.config.Account.Node.Address())
		return err
	}
	if existing.Exists() {
		slog.Info("validator already exists, skipping queue", "account", v.config.Account.Node.Address())
		return v.stateManager.HandleExistingValidator(v.eventHandler, v.txManager)
	}

	err = v.txManager.QueueValidator(v.id, RandomStake(), v.stakeManager.StakingPeriodLength())
	if err != nil {
		if strings.Contains(err.Error(), "validator already exists") {
			return v.stateManager.HandleExistingValidator(v.eventHandler, v.txManager)
		}
		slog.Error("failed to queue validator", "error", err, "account", v.config.Account.Node.Address())
		return err
	}

	v.stateManager.TransitionTo(lifecycle.StatusQueued, 0)
	return nil
}

func (v *ValidatorLifecycle) processQueued(block uint32) error {
	if v.txManager.QueuedReceipt() == nil && v.id.IsZero() {
		return errors.New("cannot set activated block for validator that has not been queued")
	}
	if v.stateManager.ActivatedBlock() != 0 {
		return nil
	}

	validator, ok := v.contractService.LookupAddress(v.id)
	if !ok {
		slog.Warn("validator not found in validations service", "id", v.id, "account", v.config.Account.Node.Address())
		return v.stateManager.CheckAlreadyExited(v.eventHandler)
	}

	slog.Debug("checking validator status", "id", v.id, "status", validator.Status, "account", v.config.Account.Node.Address())

	if validator.Status == validation.StatusActive {
		slog.Debug("validator activated", "account", v.config.Account.Node.Address(), "block", block, "startBlock", validator.StartBlock)
		v.stateManager.TransitionTo(lifecycle.StatusActive, validator.StartBlock)
	} else {
		slog.Debug("validator not yet active", "id", v.id, "status", validator.Status, "account", v.config.Account.Node.Address())
	}
	return nil
}

func (v *ValidatorLifecycle) processActive(block uint32) error {
	if v.txManager.ExitReceipt() != nil {
		return nil
	}

	if v.stateManager.Status() == lifecycle.StatusExitSignalled {
		return nil
	}

	activeValidators := v.contractService.GetActiveCount()
	minValidators := uint32(1)

	if activeValidators <= minValidators {
		slog.Info("preventing validator exit to maintain minimum active validators",
			"activeValidators", activeValidators,
			"minValidators", minValidators,
			"validator", v.id)
		return nil
	}

	if block < v.config.MinExitBlock(v.stateManager.ActivatedBlock(), v.stakeManager.StakingPeriodLength()) {
		return v.stakeManager.ChangeStake(block, v.config.Account)
	}

	slog.Info("signaling validator exit", "validator", v.id, "activeValidators", activeValidators)
	err := v.txManager.SignalExit(v.id)
	if err != nil {
		slog.Error("failed to signal exit for validator", "error", err, "id", v.id)
		return err
	}

	v.stateManager.TransitionTo(lifecycle.StatusExitSignalled, 0)
	slog.Debug("validator exit signalled", "id", v.id, "account", v.config.Account.Endorser.Address())
	return nil
}

func (v *ValidatorLifecycle) processExitSignalled(block uint32) error {
	if v.txManager.WithdrawReceipt() != nil {
		return nil
	}

	if v.stateManager.Status() != lifecycle.StatusExitSignalled || v.txManager.ExitReceipt() == nil {
		return errors.New("cannot withdraw validator that has not signalled exit")
	}

	exitBlock := v.txManager.ExitReceipt().Meta.BlockNumber
	minWithdrawBlock := v.config.MinWithdrawBlock(exitBlock, v.stateManager.stack.Config())
	if v.stateManager.ActivatedBlock() != 0 {
		minWithdrawBlock += v.stateManager.stack.Config().CooldownPeriod
	}
	if block < minWithdrawBlock {
		return nil
	}

	err := v.txManager.WithdrawStake(v.id)
	if err != nil {
		slog.Error("failed to withdraw validator", "error", err, "id", v.id)
		return err
	}

	v.stateManager.TransitionTo(lifecycle.StatusWithdrawn, 0)
	slog.Debug("validator withdrawn", "id", v.id, "account", v.config.Account.Endorser.Address())
	return nil
}
