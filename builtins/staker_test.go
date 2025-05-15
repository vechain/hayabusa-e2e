package builtins_test

import (
	_ "embed"

	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vechain/draupnir/contracts"
	"github.com/vechain/hayabusa-e2e/builtins"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	devgenesis "github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/thor"
)

func TestStaker(t *testing.T) {
	mbp := uint32(3)
	config := &hayabusa.Config{
		Nodes:             6,
		ForkBlock:         0,
		MaxBlockProposers: mbp,
		TransitionPeriod:  2,
		CooldownPeriod:    2,
		EpochLength:       2,
		MinStakingPeriod:  2,
		MidStakingPeriod:  8,
		HighStakingPeriod: 16,
	}
	client, _, cancel, err := hayabusa.StartNetwork(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)

	staker := builtins.NewStaker(client, hayabusa.ValidatorAccounts[0].PrivateKey)

	if err := staker.WaitForFork(config.ForkBlock); err != nil {
		t.Fatalf("failed to wait for fork: %v", err)
	}

	// add validators
	senders := &contracts.Senders{}
	for i := range mbp {
		validator := hayabusa.ValidatorAccounts[i]
		sender := staker.Attach(validator.PrivateKey).AddValidator(validator.Address, builtins.MinStake, config.MinStakingPeriod, true)
		senders.Add(sender)
	}
	if _, _, err := senders.Send(false); err != nil {
		t.Fatal(err)
	}
	if err := staker.WaitForPOS(config.ForkBlock + config.TransitionPeriod + 1); err != nil {
		t.Fatalf("failed to wait for PoS: %v", err)
	}

	firstActive, firstID, err := staker.FirstActive()
	require.NoError(t, err)

	t.Run("TotalStake", func(t *testing.T) {
		totalStake, err := staker.TotalStake()
		require.NoError(t, err)
		expectedStake := new(big.Int).Mul(builtins.MinStake, big.NewInt(int64(mbp)))
		require.Equal(t, expectedStake, totalStake)
	})

	t.Run("Get", func(t *testing.T) {
		validator, err := staker.Get(firstID)
		require.NoError(t, err)
		require.False(t, validator.Master.IsZero())
		require.False(t, validator.Endorsor.IsZero())
		require.Equal(t, builtins.StatusActive, validator.Status)
		require.True(t, firstActive.AutoRenew)
		require.Equal(t, validator.Stake, builtins.MinStake)
		require.Equal(t, validator.Weight, big.NewInt(0).Mul(builtins.MinStake, big.NewInt(2)))
	})

	t.Run("FirstActive", func(t *testing.T) {
		require.False(t, firstID.IsZero())
		require.True(t, firstActive.Exists())
		require.True(t, firstActive.AutoRenew)
		require.Equal(t, builtins.MinStake, firstActive.Stake)
		require.Equal(t, big.NewInt(0).Mul(builtins.MinStake, big.NewInt(2)), firstActive.Weight)
		require.Equal(t, builtins.StatusActive, firstActive.Status)
		require.False(t, firstActive.Endorsor.IsZero())

		next, id, err := staker.Next(firstID)
		require.NoError(t, err)
		require.False(t, id.IsZero())
		require.True(t, next.Exists())
	})

	t.Run("Next", func(t *testing.T) {
		next, id, err := staker.Next(firstID)
		require.NoError(t, err)
		require.False(t, id.IsZero())
		require.True(t, next.Exists())
		require.Equal(t, builtins.StatusActive, next.Status)
		require.Equal(t, builtins.MinStake, next.Stake)
		require.Equal(t, big.NewInt(0).Mul(builtins.MinStake, big.NewInt(2)), next.Weight)
		require.True(t, next.AutoRenew)
		require.False(t, next.Endorsor.IsZero())
	})

	var queuedID thor.Bytes32
	var validator devgenesis.DevAccount

	t.Run("AddValidator", func(t *testing.T) {
		validator = hayabusa.ValidatorAccounts[5]
		receipt, _, err := staker.Attach(validator.PrivateKey).AddValidator(validator.Address, builtins.MinStake, config.MinStakingPeriod, false).Receipt(false)
		require.NoError(t, err)

		events, err := staker.FilterValidatorQueued(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.Equal(t, validator.Address, events[0].Endorsor)
		require.Equal(t, builtins.MinStake, events[0].Stake)
		require.False(t, events[0].AutoRenew)
		queuedID = events[0].ValidationID
	})

	t.Run("FirstQueued", func(t *testing.T) {
		firstQueued, id, err := staker.FirstQueued()
		require.NoError(t, err)
		require.False(t, id.IsZero())
		require.True(t, firstQueued.Exists())
		require.Equal(t, queuedID, id)
		require.Equal(t, 0, firstQueued.Stake.Sign())
		require.Equal(t, builtins.StatusQueued, firstQueued.Status)
		require.False(t, firstQueued.Endorsor.IsZero())
	})

	t.Run("IncreaseStake", func(t *testing.T) {
		receipt, _, err := staker.Attach(validator.PrivateKey).IncreaseStake(queuedID, builtins.MinStake).Receipt(false)
		require.NoError(t, err)

		events, err := staker.FilterStakeIncreased(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.Equal(t, queuedID, events[0].ValidationID)
		require.Equal(t, validator.Address, events[0].Endorsor)
		require.Equal(t, builtins.MinStake, events[0].Added)
	})

	t.Run("DecreaseStake", func(t *testing.T) {
		receipt, _, err := staker.Attach(validator.PrivateKey).DecreaseStake(queuedID, builtins.MinStake).Receipt(false)
		require.NoError(t, err)

		events, err := staker.FilterStakeDecreased(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.Equal(t, queuedID, events[0].ValidationID)
		require.Equal(t, validator.Address, events[0].Endorsor)
		require.Equal(t, builtins.MinStake, events[0].Removed)
	})

	t.Run("Enable AutoRenew", func(t *testing.T) {
		receipt, _, err := staker.Attach(validator.PrivateKey).UpdateAutoRenew(queuedID, true).Receipt(false)
		require.NoError(t, err)

		events, err := staker.FilterValidatorUpdatedAutoRenew(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.Equal(t, queuedID, events[0].ValidationID)
		require.Equal(t, validator.Address, events[0].Endorsor)
		require.True(t, events[0].AutoRenew)

		validator, err := staker.Get(queuedID)
		require.NoError(t, err)
		require.True(t, validator.AutoRenew)
	})

	t.Run("Disable AutoRenew", func(t *testing.T) {
		receipt, _, err := staker.Attach(validator.PrivateKey).UpdateAutoRenew(queuedID, false).Receipt(false)
		require.NoError(t, err)

		events, err := staker.FilterValidatorUpdatedAutoRenew(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.Equal(t, queuedID, events[0].ValidationID)
		require.Equal(t, validator.Address, events[0].Endorsor)
		require.False(t, events[0].AutoRenew)

		validator, err := staker.Get(queuedID)
		require.NoError(t, err)
		require.False(t, validator.AutoRenew)
	})

	var delegationID thor.Bytes32

	t.Run("AddDelegation", func(t *testing.T) {
		receipt, _, err := staker.Attach(hayabusa.Stargate.PrivateKey).AddDelegation(queuedID, builtins.MinStake, false, uint8(100)).Receipt(false)
		require.NoError(t, err)

		events, err := staker.FilterDelegationAdded(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.Equal(t, builtins.MinStake, events[0].Stake)
		require.Equal(t, uint8(100), events[0].Multiplier)
		require.False(t, events[0].AutoRenew)
		require.False(t, events[0].DelegationID.IsZero())

		delegationID = events[0].DelegationID
	})

	t.Run("GetDelegation", func(t *testing.T) {
		delegation, err := staker.GetDelegation(delegationID)
		require.NoError(t, err)
		require.Equal(t, builtins.MinStake, delegation.Stake)
		require.Equal(t, uint8(100), delegation.Multiplier)
		require.Equal(t, false, delegation.AutoRenew)
		require.Equal(t, queuedID, delegation.ValidationID)
	})

	t.Run("UpdateDelegationAutoRenew", func(t *testing.T) {
		// enable auto renew
		receipt, _, err := staker.Attach(hayabusa.Stargate.PrivateKey).UpdateDelegationAutoRenew(delegationID, true).Receipt(false)
		require.NoError(t, err)

		events, err := staker.FilterDelegationUpdatedAutoRenew(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.Equal(t, delegationID, events[0].DelegationID)
		require.Equal(t, true, events[0].AutoRenew)

		delegation, err := staker.GetDelegation(delegationID)
		require.NoError(t, err)
		require.Equal(t, true, delegation.AutoRenew)

		// disable auto renew
		receipt, _, err = staker.Attach(hayabusa.Stargate.PrivateKey).UpdateDelegationAutoRenew(delegationID, false).Receipt(false)
		require.NoError(t, err)

		events, err = staker.FilterDelegationUpdatedAutoRenew(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.Equal(t, delegationID, events[0].DelegationID)
		require.Equal(t, false, events[0].AutoRenew)

		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		require.Equal(t, false, delegation.AutoRenew)
	})

	t.Run("Withdraw", func(t *testing.T) {
		withdrawAmount, err := staker.GetWithdraw(queuedID)
		require.NoError(t, err)
		require.Equal(t, builtins.MinStake, withdrawAmount)

		// withdraw the validator
		receipt, _, err := staker.Attach(validator.PrivateKey).Withdraw(queuedID).Receipt(false)
		require.NoError(t, err)

		events, err := staker.FilterValidatorWithdrawn(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.Equal(t, queuedID, events[0].ValidationID)
		require.Equal(t, validator.Address, events[0].Endorsor)
		// stake was increased earlier
		require.Equal(t, new(big.Int).Mul(builtins.MinStake, big.NewInt(2)), events[0].Stake)
	})

	t.Run("Withdraw Delegation", func(t *testing.T) {
		delegation, err := staker.GetDelegation(delegationID)
		require.NoError(t, err)
		require.Equal(t, builtins.MinStake, delegation.Stake)

		// withdraw the delegation
		receipt, _, err := staker.Attach(hayabusa.Stargate.PrivateKey).WithdrawDelegation(delegationID).Receipt(false)
		require.NoError(t, err)

		events, err := staker.FilterDelegationWithdrawn(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.Equal(t, delegationID, events[0].DelegationID)
		require.Equal(t, builtins.MinStake, events[0].Stake)
	})
}
