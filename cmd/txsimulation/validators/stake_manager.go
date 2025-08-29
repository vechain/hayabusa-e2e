package validators

import (
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
)

// StakeManager - handles stake-related operations only
type StakeManager struct {
	stack               *stack.Stack
	validatorID         thor.Address
	changeInterval      uint32
	stakingPeriodLength uint32

	lastStakeUpdate uint32
	stakeIncreased  bool
}

func NewStakeManager(stack *stack.Stack, validatorID thor.Address, changeInterval, stakingPeriodLength uint32) *StakeManager {
	return &StakeManager{
		stack:               stack,
		validatorID:         validatorID,
		changeInterval:      changeInterval,
		stakingPeriodLength: stakingPeriodLength,
	}
}

func (sm *StakeManager) StakingPeriodLength() uint32 {
	return sm.stakingPeriodLength
}

func (sm *StakeManager) ShouldChangeStake(currentBlock uint32) bool {
	interval := sm.changeInterval * sm.stakingPeriodLength
	return sm.lastStakeUpdate+interval <= currentBlock
}

func (sm *StakeManager) ChangeStake(currentBlock uint32, account *hayabusa.NodePair) error {
	if !sm.ShouldChangeStake(currentBlock) {
		return nil
	}

	var method *bind.MethodBuilder
	if sm.stakeIncreased {
		method = sm.stack.Staker().DecreaseStake(sm.validatorID, RandomStakeBetween(3, 5))
	} else {
		method = sm.stack.Staker().IncreaseStake(sm.validatorID, RandomStakeBetween(3, 5))
	}

	sm.lastStakeUpdate = currentBlock
	sm.stakeIncreased = !sm.stakeIncreased

	_, err := sm.stack.SendTransaction(method, account.Endorser)
	return err
}
