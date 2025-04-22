package builtins_test

import (
	_ "embed"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vechain/draupnir/common"
	"github.com/vechain/draupnir/contracts"
	"github.com/vechain/hayabusa-e2e/builtins"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	devgenesis "github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/thor"
)

func TestStaker_AddValidator(t *testing.T) {
	staker, config := newTestSetup(t, 3)

	validator := devgenesis.DevAccounts()[0]
	receipt, _, err := staker.AddValidator(validator.Address, builtins.MinStake, config.MinStakingPeriod, true).Receipt(false)
	assert.NoError(t, err)

	events, err := staker.FilterValidatorQueued(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	assert.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, validator.Address, events[0].Endorsor)
	assert.Equal(t, builtins.MinStake, events[0].Stake)
	assert.Equal(t, config.MinStakingPeriod, events[0].Period)
	assert.Equal(t, true, events[0].AutoRenew)

	firstQueued, id, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id, events[0].ValidationID)
	assert.Equal(t, validator.Address, *firstQueued.Endorsor)
}

func TestStaker_FirstActive_Next(t *testing.T) {
	staker, _, _ := newTestSetupWithActiveValidators(t, 3, true, builtins.MinStake)

	firstActive, id, err := staker.FirstActive()
	assert.NoError(t, err)
	assert.False(t, id.IsZero())
	assert.True(t, firstActive.Exists())
	assert.True(t, firstActive.AutoRenew)
	assert.Equal(t, builtins.MinStake, firstActive.Stake)
	assert.Equal(t, builtins.MinStake, firstActive.Weight)
	assert.Equal(t, builtins.StatusActive, firstActive.Status)
	assert.False(t, firstActive.Endorsor.IsZero())

	next, id, err := staker.Next(id)
	assert.NoError(t, err)
	assert.False(t, id.IsZero())
	assert.True(t, next.Exists())
}

func TestStaker_TotalStake(t *testing.T) {
	amount := uint32(3)
	staker, _, _ := newTestSetupWithActiveValidators(t, amount, false, builtins.MinStake)
	totalStake, err := staker.TotalStake()
	assert.NoError(t, err)
	expectedStake := new(big.Int).Mul(builtins.MinStake, big.NewInt(int64(amount)))
	assert.Equal(t, expectedStake, totalStake)
}

func TestStaker_WithdrawValidator(t *testing.T) {
	staker, config, validators := newTestSetupWithActiveValidators(t, 3, false, builtins.MinStake)

	ticker := common.NewTicker(staker.Client())

	// wait for 1 staking period + cooldown period
	block := config.ForkBlock + config.TransitionPeriod + config.EpochLength + config.CooldownPeriod
	assert.NoError(t, ticker.WaitForBlock(block))

	// find the validator that has exited
	account, id, found, err := findExitedValidator(staker, validators)
	assert.NoError(t, err)
	if !found {
		t.Fatal("no validator found")
	}

	withdrawAmount, err := staker.GetWithdraw(id)
	assert.NoError(t, err)
	assert.Equal(t, withdrawAmount, builtins.MinStake)

	// withdraw the validator
	receipt, _, err := staker.Attach(account.PrivateKey).Withdraw(id).Receipt(false)
	assert.NoError(t, err)

	events, err := staker.FilterValidatorWithdrawn(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	assert.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, id, events[0].ValidationID)
	assert.Equal(t, account.Address, events[0].Endorsor)
	assert.Equal(t, builtins.MinStake, events[0].Stake)
}

func TestStaker_IncreaseStake(t *testing.T) {
	staker, config, validators := newTestSetupWithActiveValidators(t, 3, true, builtins.MinStake)

	ticker := common.NewTicker(staker.Client())

	// find the validator that has exited
	var account devgenesis.DevAccount
	var id thor.Bytes32
	for id, account = range validators {
	}

	// increase the stake
	receipt, _, err := staker.Attach(account.PrivateKey).IncreaseStake(id, builtins.MinStake).Receipt(false)
	assert.NoError(t, err)

	events, err := staker.FilterStakeIncreased(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	assert.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, id, events[0].ValidationID)
	assert.Equal(t, account.Address, events[0].Endorsor)
	assert.Equal(t, builtins.MinStake, events[0].Added)

	// wait for the next staking period
	block := config.ForkBlock + config.TransitionPeriod + config.EpochLength
	assert.NoError(t, ticker.WaitForBlock(block))

	// check the validator stake
	validator, err := staker.Get(id)
	assert.NoError(t, err)
	assert.Equal(t, validator.Stake, new(big.Int).Mul(builtins.MinStake, big.NewInt(2)))
}

func TestStaker_DecreaseStake(t *testing.T) {
	stake := new(big.Int).Mul(builtins.MinStake, big.NewInt(2))
	staker, config, validators := newTestSetupWithActiveValidators(t, 3, true, stake)
	ticker := common.NewTicker(staker.Client())

	var account devgenesis.DevAccount
	var id thor.Bytes32
	for id, account = range validators {
	}

	// decrease the stake
	receipt, _, err := staker.Attach(account.PrivateKey).DecreaseStake(id, builtins.MinStake).Receipt(false)
	assert.NoError(t, err)

	events, err := staker.FilterStakeDecreased(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	assert.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, id, events[0].ValidationID)
	assert.Equal(t, account.Address, events[0].Endorsor)
	assert.Equal(t, builtins.MinStake, events[0].Removed)

	// wait for the next staking period
	block := config.ForkBlock + config.TransitionPeriod + config.EpochLength
	assert.NoError(t, ticker.WaitForBlock(block))

	// check the validator stake
	validator, err := staker.Get(id)
	assert.NoError(t, err)
	assert.Equal(t, validator.Stake, builtins.MinStake)
}

func TestStaker_UpdateAutoRenew(t *testing.T) {
	staker, config, validators := newTestSetupWithActiveValidators(t, 3, false, builtins.MinStake)
	ticker := common.NewTicker(staker.Client())

	var account devgenesis.DevAccount
	var id thor.Bytes32
	for id, account = range validators {
	}

	// enable auto renew
	receipt, _, err := staker.Attach(account.PrivateKey).UpdateAutoRenew(id, true).Receipt(false)
	assert.NoError(t, err)

	events, err := staker.FilterValidatorUpdatedAutoRenew(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	assert.NoError(t, err)
	assert.Len(t, events, 1)
	assert.True(t, events[0].AutoRenew)
	assert.Equal(t, id, events[0].ValidationID)
	assert.Equal(t, account.Address, events[0].Endorsor)

	// wait for the next staking period
	block := config.ForkBlock + config.TransitionPeriod + config.EpochLength*2
	assert.NoError(t, ticker.WaitForBlock(block))

	// check the validator auto renew
	validator, err := staker.Get(id)
	assert.NoError(t, err)
	assert.True(t, validator.AutoRenew)

	// disable auto renew
	receipt, _, err = staker.Attach(account.PrivateKey).UpdateAutoRenew(id, false).Receipt(false)
	assert.NoError(t, err)

	events, err = staker.FilterValidatorUpdatedAutoRenew(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	assert.NoError(t, err)
	assert.Len(t, events, 1)
	assert.False(t, events[0].AutoRenew)
	assert.Equal(t, id, events[0].ValidationID)
	assert.Equal(t, account.Address, events[0].Endorsor)

	// wait for the next staking period
	block += config.EpochLength
	assert.NoError(t, ticker.WaitForBlock(block))

	// check the validator auto renew
	validator, err = staker.Get(id)
	assert.NoError(t, err)
	assert.False(t, validator.AutoRenew)
}

func TestStaker_AddDelegation(t *testing.T) {
	staker, config, validators := newTestSetupWithActiveValidators(t, 3, true, builtins.MinStake)
	ticker := common.NewTicker(staker.Client())

	var id thor.Bytes32
	for id, _ = range validators {
	}

	delegator := devgenesis.DevAccounts()[9]

	receipt, _, err := staker.AddDelegation(id, delegator.Address, builtins.MinStake, true, uint8(100)).Receipt(false)
	assert.NoError(t, err)
	events, err := staker.FilterDelegationAdded(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	assert.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, id, events[0].ValidationID)
	assert.Equal(t, delegator.Address, events[0].Delegator)
	assert.Equal(t, builtins.MinStake, events[0].Stake)
	assert.Equal(t, uint8(100), events[0].Multiplier)
	assert.Equal(t, true, events[0].AutoRenew)

	block := config.ForkBlock + config.TransitionPeriod + config.EpochLength
	assert.NoError(t, ticker.WaitForBlock(block))

	// check the delegator stake
	delegation, err := staker.GetDelegation(id, delegator.Address)
	assert.NoError(t, err)
	assert.Equal(t, delegation.Stake, builtins.MinStake)

	// multiplier of 100 means 1x weight, so the weight is the same as the stake
	expectedWeight := new(big.Int).Mul(builtins.MinStake, big.NewInt(2))

	validator, err := staker.Get(id)
	assert.NoError(t, err)
	assert.Equal(t, expectedWeight, validator.Weight)
}

func TestStaker_UpdateDelegatorAutoRenew(t *testing.T) {
	delegator := devgenesis.DevAccounts()[9]
	staker, config, validationID := newTestSetupWithActiveDelegator(t, 3, true, false, builtins.MinStake, delegator)
	ticker := common.NewTicker(staker.Client())

	delegation, err := staker.GetDelegation(validationID, delegator.Address)
	assert.NoError(t, err)
	assert.False(t, delegation.AutoRenew)

	receipt, _, err := staker.Attach(delegator.PrivateKey).UpdateDelegatorAutoRenew(validationID, delegator.Address, true).Receipt(false)
	assert.NoError(t, err)

	events, err := staker.FilterDelegationUpdatedAutoRenew(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	assert.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, validationID, events[0].ValidationID)
	assert.Equal(t, delegator.Address, events[0].Delegator)
	assert.Equal(t, true, events[0].AutoRenew)

	block := config.ForkBlock + config.TransitionPeriod + config.EpochLength*2
	assert.NoError(t, ticker.WaitForBlock(block))

	// check the delegator auto renew
	delegation, err = staker.GetDelegation(validationID, delegator.Address)
	assert.NoError(t, err)
	assert.True(t, delegation.AutoRenew)

	receipt, _, err = staker.Attach(delegator.PrivateKey).UpdateDelegatorAutoRenew(validationID, delegator.Address, false).Receipt(false)
	assert.NoError(t, err)

	events, err = staker.FilterDelegationUpdatedAutoRenew(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	assert.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, validationID, events[0].ValidationID)
	assert.Equal(t, delegator.Address, events[0].Delegator)
	assert.Equal(t, false, events[0].AutoRenew)

	block += config.EpochLength
	assert.NoError(t, ticker.WaitForBlock(block))

	// check the delegator auto renew
	delegation, err = staker.GetDelegation(validationID, delegator.Address)
	assert.NoError(t, err)
	assert.False(t, delegation.AutoRenew)

	block += config.EpochLength
	assert.NoError(t, ticker.WaitForBlock(block))

	receipt, _, err = staker.WithdrawDelegation(validationID, delegator.Address).Receipt(false)
	assert.NoError(t, err)

	withdraEvents, err := staker.FilterDelegationWithdrawn(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	assert.NoError(t, err)
	assert.Len(t, withdraEvents, 1)
	assert.Equal(t, validationID, withdraEvents[0].ValidationID)
	assert.Equal(t, delegator.Address, withdraEvents[0].Delegator)
	assert.Equal(t, builtins.MinStake, withdraEvents[0].Stake)
}

func newTestSetup(t *testing.T, maxBlockProposers uint32) (*builtins.Staker, *hayabusa.Config) {
	t.Helper()

	config := &hayabusa.Config{
		Nodes:             3,
		ForkBlock:         0,
		MaxBlockProposers: maxBlockProposers,
		TransitionPeriod:  6,
		CooldownPeriod:    3,
		EpochLength:       3,
		MinStakingPeriod:  3,
		MidStakingPeriod:  6,
		HighStakingPeriod: 12,
	}
	client, cancel, err := hayabusa.StartNetwork(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)

	staker := builtins.NewStaker(client, devgenesis.DevAccounts()[0].PrivateKey)

	if err := staker.WaitForFork(config.ForkBlock); err != nil {
		t.Fatalf("failed to wait for fork: %v", err)
	}

	return staker, config
}

func newTestSetupWithActiveValidators(t *testing.T, maxBlockProposers uint32, autoRenew bool, stake *big.Int) (*builtins.Staker, *hayabusa.Config, map[thor.Bytes32]devgenesis.DevAccount) {
	t.Helper()

	staker, config := newTestSetup(t, maxBlockProposers)
	accounts := make(map[thor.Address]devgenesis.DevAccount)
	validators := make(map[thor.Bytes32]devgenesis.DevAccount)

	senders := &contracts.Senders{}
	for i := 0; i < int(maxBlockProposers); i++ {
		validator := devgenesis.DevAccounts()[i]
		accounts[validator.Address] = validator
		sender := staker.Attach(validator.PrivateKey).AddValidator(validator.Address, stake, config.MinStakingPeriod, autoRenew)
		senders.Add(sender)
	}

	if _, _, err := senders.Send(false); err != nil {
		t.Fatal(err)
	}

	events, err := staker.FilterValidatorQueued(0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		validators[event.ValidationID] = accounts[event.Endorsor]
	}

	if err := staker.WaitForPOS(config.ForkBlock + config.TransitionPeriod); err != nil {
		t.Fatalf("failed to wait for PoS: %v", err)
	}

	return staker, config, validators
}

func newTestSetupWithActiveDelegator(t *testing.T, maxBlockProposers uint32, validatorAutoRenew, delegatorAutoRenew bool, stake *big.Int, delegator devgenesis.DevAccount) (*builtins.Staker, *hayabusa.Config, thor.Bytes32) {
	t.Helper()

	staker, config := newTestSetup(t, maxBlockProposers)
	accounts := make(map[thor.Address]devgenesis.DevAccount)
	validators := make(map[thor.Bytes32]devgenesis.DevAccount)

	senders := &contracts.Senders{}
	for i := 0; i < int(maxBlockProposers); i++ {
		validator := devgenesis.DevAccounts()[i]
		accounts[validator.Address] = validator
		sender := staker.Attach(validator.PrivateKey).AddValidator(validator.Address, stake, config.MinStakingPeriod, validatorAutoRenew)
		senders.Add(sender)
	}

	if _, _, err := senders.Send(false); err != nil {
		t.Fatal(err)
	}

	events, err := staker.FilterValidatorQueued(0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var validationID thor.Bytes32
	for _, event := range events {
		validationID = event.ValidationID
		validators[event.ValidationID] = accounts[event.Endorsor]
	}

	if err := staker.WaitForPOS(config.ForkBlock + config.TransitionPeriod); err != nil {
		t.Fatalf("failed to wait for PoS: %v", err)
	}

	_, _, err = staker.Attach(delegator.PrivateKey).AddDelegation(validationID, delegator.Address, builtins.MinStake, delegatorAutoRenew, uint8(100)).Receipt(false)
	assert.NoError(t, err)

	block := config.ForkBlock + config.TransitionPeriod + config.EpochLength
	assert.NoError(t, common.NewTicker(staker.Client()).WaitForBlock(block))

	return staker, config, validationID
}

func findExitedValidator(staker *builtins.Staker, validators map[thor.Bytes32]devgenesis.DevAccount) (devgenesis.DevAccount, thor.Bytes32, bool, error) {
	for id, account := range validators {
		validator, err := staker.Get(id)
		if err != nil {
			return devgenesis.DevAccount{}, thor.Bytes32{}, false, err
		}
		if validator.Status == builtins.StatusExited {
			return account, id, true, nil
		}
	}
	return devgenesis.DevAccount{}, thor.Bytes32{}, false, nil
}
