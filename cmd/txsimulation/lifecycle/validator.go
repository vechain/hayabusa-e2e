package lifecycle

import (
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/delegations"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/validators"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/builtin/staker/validation"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
)

type ValidatorLifecycle struct {
	ValidatorConfig
	validations *validators.Service
	delegations *delegations.PositionManager
	stack       *stack.Stack

	status Status
	id     thor.Address

	queuedReceipt   *api.Receipt // the receipt of the queued transaction
	exitReceipt     *api.Receipt // the receipt of the exit transaction
	withdrawReceipt *api.Receipt // the receipt of the withdraw transaction

	activatedBlock      uint32 // the block at which this lifecycle was activated
	stakingPeriodLength uint32 // the length of the staking period in blocks
	stakeIncreased      bool   // indicates if the stake as previously increased or decreased
	lastStakeUpdate     uint32 // the last block at which the stake was updated

	mu sync.Mutex
}

var (
	Eth     = big.NewInt(1e18) // 1 ETH in wei
	Million = big.NewInt(1e6)  // 1 million VET
)

func RandomStake() *big.Int {
	return RandomStakeBetween(25, 31) // average stake is currently 28m - https://vechainstats.com/vechain-nodes/#xnode-log
}

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

func NewValidatorLifecycle(
	config ValidatorConfig,
	validations *validators.Service,
	delegations *delegations.PositionManager,
	stack *stack.Stack,
	stakingPeriodLength uint32,
) *ValidatorLifecycle {
	return &ValidatorLifecycle{
		ValidatorConfig:     config,
		validations:         validations,
		delegations:         delegations,
		stack:               stack,
		stakingPeriodLength: stakingPeriodLength,
		id:                  config.Account.Node.Address(),
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

func (v *ValidatorLifecycle) Process(block uint32) error {
	validator, ok := v.validations.LookupAddress(v.id)
	if ok && (validator.Status == validation.StatusExit || validator.OfflineBlock != nil) {
		v.delegations.UnregisterValidator(v.id)
	}
	if ok && validator.Status == validation.StatusActive {
		v.delegations.RegisterValidator(v.id)
	}

	switch v.status {
	case StatusPending:
		return v.ProcessPending(block)
	case StatusQueued:
		return v.ProcessQueued(block)
	case StatusActive:
		return v.ProcessActive(block)
	case StatusExitSignalled:
		return v.ProcessExited(block)
	case StatusWithdrawn:

	}

	return nil
}

func (v *ValidatorLifecycle) ProcessPending(block uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.queuedReceipt != nil {
		return nil
	}

	if block < v.Config.QueueBlock(v.stack.Config()) {
		return nil
	}
	slog.Debug("queuing validator", "account", v.Account.Node.Address(), "block", block)

	existing, err := v.stack.Staker().GetValidation(v.Account.Node.Address())
	if err != nil {
		slog.Error("failed to check existing validator", "error", err, "account", v.Account.Node.Address())
		return err
	}
	if existing.Exists() {
		slog.Info("validator already exists, skipping queue", "account", v.Account.Node.Address())
		return v.setQueuedReceipt()
	}

	method := v.stack.Staker().AddValidation(v.Account.Node.Address(), RandomStake(), v.stakingPeriodLength)
	receipt, err := v.stack.SendTransactionAndWait(method, v.Account.Endorser)
	if err != nil {
		if strings.Contains(err.Error(), "validator already exists") {
			return v.setQueuedReceipt()
		}
		slog.Error("failed to queue validator", "error", err, "account", v.Account.Node.Address())
		return err
	}

	v.queuedReceipt = receipt
	v.status = StatusQueued

	return nil
}

func (v *ValidatorLifecycle) setQueuedReceipt() error {
	validator, ok := v.validations.LookupAddress(v.id)
	if !ok {
		slog.Warn("validator exists but not found in validations service", "id", v.id)
		v.status = StatusQueued
		return nil
	}

	switch validator.Status {
	case validation.StatusActive:
		slog.Debug("validator already active", "id", v.id, "account", v.Account.Node.Address())
		v.status = StatusActive
		v.activatedBlock = validator.StartBlock
	case validation.StatusQueued:
		slog.Debug("validator already queued", "id", v.id, "account", v.Account.Node.Address())
		v.status = StatusQueued
	case validation.StatusExit:
		slog.Debug("validator already exited", "id", v.id, "account", v.Account.Node.Address())
		v.status = StatusWithdrawn
	default:
		slog.Debug("validator exists with unknown status", "id", v.id, "status", validator.Status, "account", v.Account.Node.Address())
		v.status = StatusQueued
	}

	return nil
}

func (v *ValidatorLifecycle) ProcessQueued(block uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.queuedReceipt == nil && v.id.IsZero() {
		return errors.New("cannot set activated block for validator that has not been queued")
	}
	if v.activatedBlock != 0 {
		return nil
	}
	validator, ok := v.validations.LookupAddress(v.id)
	if !ok {
		slog.Warn("validator not found in validations service", "id", v.id, "account", v.Account.Node.Address())
		return nil
	}

	slog.Debug("checking validator status", "id", v.id, "status", validator.Status, "account", v.Account.Node.Address())

	if validator.Status == validation.StatusActive {
		slog.Debug("validator activated", "account", v.Account.Node.Address(), "block", block, "startBlock", validator.StartBlock)
		v.activatedBlock = validator.StartBlock
		v.status = StatusActive
	} else {
		slog.Debug("validator not yet active", "id", v.id, "status", validator.Status, "account", v.Account.Node.Address())
	}
	return nil
}

func (v *ValidatorLifecycle) ProcessActive(block uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.exitReceipt != nil {
		return nil
	}

	if v.status == StatusExitSignalled {
		return nil
	}

	activeValidators := v.validations.GetActiveCount()
	minValidators := uint32(1)

	if activeValidators <= minValidators {
		slog.Info("preventing validator exit to maintain minimum active validators",
			"activeValidators", activeValidators,
			"minValidators", minValidators,
			"validator", v.id)
		return v.stakeChange(block)
	}

	if block < v.Config.MinExitBlock(v.activatedBlock, v.stakingPeriodLength) {
		return v.stakeChange(block)
	}

	slog.Info("signaling validator exit", "validator", v.id, "activeValidators", activeValidators)
	method := v.stack.Staker().SignalExit(v.id)
	receipt, err := v.stack.SendTransactionAndWait(method, v.Account.Endorser)
	if receipt != nil {
		v.status = StatusExitSignalled
		v.exitReceipt = receipt
	}
	if err != nil {
		slog.Error("failed to disable auto-renew for validator", "error", err, "id", v.id)
		return err
	}
	v.status = StatusExitSignalled
	slog.Debug("validator exit signalled", "id", v.id, "account", v.Account.Endorser.Address())
	return nil
}

func (v *ValidatorLifecycle) stakeChange(block uint32) error {
	interval := v.StakeChangeInterval * v.stakingPeriodLength
	if v.lastStakeUpdate+interval > block {
		return nil
	}
	var sender *bind.MethodBuilder
	if v.stakeIncreased {
		sender = v.stack.Staker().DecreaseStake(v.Account.Node.Address(), RandomStakeBetween(3, 5))
	} else {
		sender = v.stack.Staker().IncreaseStake(v.Account.Node.Address(), RandomStakeBetween(3, 5))
	}
	v.lastStakeUpdate = block
	v.stakeIncreased = !v.stakeIncreased
	_, err := v.stack.SendTransaction(sender, v.Account.Endorser)
	return err
}

func (v *ValidatorLifecycle) ProcessExited(block uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.withdrawReceipt != nil {
		return nil
	}

	if v.status != StatusExitSignalled || v.exitReceipt == nil {
		return errors.New("cannot withdraw validator that has not signalled exit")
	}
	minWithdrawBlock := v.Config.MinWithdrawBlock(v.exitReceipt.Meta.BlockNumber, v.stack.Config())
	if v.activatedBlock != 0 {
		minWithdrawBlock += v.stack.Config().CooldownPeriod
	}
	if block < minWithdrawBlock {
		return nil
	}
	method := v.stack.Staker().WithdrawStake(v.id)
	receipt, err := v.stack.SendTransactionAndWait(method, v.Account.Endorser)
	if err != nil {
		slog.Error("failed to withdraw validator", "error", err, "id", v.id)
		return err
	}

	v.status = StatusWithdrawn
	v.withdrawReceipt = receipt

	slog.Debug("validator withdrawn", "id", v.id, "account", v.Account.Endorser.Address())
	return nil
}
