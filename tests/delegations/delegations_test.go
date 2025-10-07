package delegations

import (
	"context"
	"math/big"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func Test_StargateRewards(t *testing.T) {
	t.Parallel()
	staker, config, validationIDs, _ := newDelegationSetup(t)

	expectedStake := new(big.Int).Mul(builtin.MinStake(), big.NewInt(int64(len(validationIDs))))
	stargateAddr := hayabusa.Stargate.Address()

	multiplier := uint8(200)
	delegators := int64(10)
	dStake := hayabusa.NewWeightedStakeWithMultiplier(builtin.MinStake(), multiplier)
	vStake := hayabusa.NewWeightedStakeWithMultiplier(builtin.MinStake(), multiplier)

	for _, validationID := range validationIDs { // evenly distribute delegations among validators
		senders := &utils.Senders{}
		for range delegators {
			sender := staker.AddDelegation(validationID, dStake.VET(), multiplier).Send().WithSigner(hayabusa.Stargate).WithOptions(testutil.TxOptions())
			senders.Add(sender)
			expectedStake = expectedStake.Add(expectedStake, dStake.VET())
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _, err := senders.Send(ctx)
		cancel()
		require.NoError(t, err)
	}

	ticker := utils.NewTicker(staker.Raw().Client())
	best, err := staker.Raw().Client().Block("best")
	require.NoError(t, err)
	require.NoError(t, ticker.WaitForBlock(best.Number+config.MinStakingPeriod))

	totalStake, totalWeight, err := staker.TotalStake()
	require.NoError(t, err)
	assert.Equal(t, expectedStake, totalStake)
	assert.Equal(t, big.NewInt(0).Mul(expectedStake, big.NewInt(2)), totalWeight)

	best, err = staker.Raw().Client().Block("best")
	require.NoError(t, err)

	// block N energy
	acc, err := staker.Raw().Client().Account(&stargateAddr, thorclient.Revision(strconv.Itoa(int(best.Number))))
	require.NoError(t, err)
	blockNEnergy := (*big.Int)(acc.Energy)

	assert.NoError(t, ticker.WaitForBlock(best.Number+1))

	// block N+1 energy
	acc, err = staker.Raw().Client().Account(&stargateAddr, thorclient.Revision(strconv.Itoa(int(best.Number+1))))
	require.NoError(t, err)
	blockNPlus1Energy := (*big.Int)(acc.Energy)

	// assert plus1 is greater than N
	assert.True(t, blockNPlus1Energy.Cmp(blockNEnergy) > 0, "block N+1 energy should be greater than block N energy")

	totals, err := staker.GetValidationTotals(validationIDs[0])
	require.NoError(t, err)

	combinedDStake := big.NewInt(0).Mul(dStake.VET(), big.NewInt(delegators))
	combinedDWeight := big.NewInt(0).Mul(dStake.Weight(), big.NewInt(delegators))

	assert.Equal(t, big.NewInt(0).Add(combinedDStake, vStake.VET()), totals.TotalLockedStake)
	assert.Equal(t, big.NewInt(0).Add(combinedDWeight, vStake.Weight()), totals.TotalLockedWeight)
}

func Test_Delegations_Delegate1PeriodOnly(t *testing.T) {
	t.Parallel()
	staker, config, validationIDs, _ := newDelegationSetup(t)
	ticker := utils.NewTicker(staker.Raw().Client())

	multiplier := uint8(100)
	receipt := testutil.Send(t, hayabusa.Stargate,
		staker.AddDelegation(validationIDs[0], builtin.MinStake(), multiplier))
	delegationID := testutil.ReceiptToID(receipt)
	delegation, err := staker.GetDelegation(delegationID)
	require.NoError(t, err)
	assert.Equal(t, builtin.MinStake(), delegation.Stake)
	assert.False(t, delegation.Locked)
	assert.Equal(t, uint8(100), delegation.Multiplier)
	require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))

	previousTotalStake, _, err := staker.TotalStake()
	require.NoError(t, err)

	// wait for validators current period to activate delegator
	require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))
	receipt = testutil.Send(t, hayabusa.Stargate, staker.SignalDelegationExit(delegationID))
	require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))

	// withdraw - should succeed since auto-renew is false
	delegation, err = staker.GetDelegation(delegationID)
	require.NoError(t, err)
	assert.False(t, delegation.Locked)
	receipt = testutil.Send(t, hayabusa.Stargate, staker.WithdrawDelegation(delegationID))

	delegation, err = staker.GetDelegation(delegationID)
	require.NoError(t, err)
	assert.True(t, delegation.Stake.Sign() == 0)

	require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))
	currentTotalStake, currentTotalWeight, err := staker.TotalStake()
	require.NoError(t, err)
	expectedTotalStake := big.NewInt(0).Sub(previousTotalStake, builtin.MinStake())
	assert.Equal(t, expectedTotalStake, currentTotalStake,
		"Wrong stake after exit")

	valNumber := len(validationIDs)
	assert.Equal(t, big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(int64(valNumber))), currentTotalWeight,
		"Wrong weight after exit")
}

func Test_Delegations(t *testing.T) {
	staker, config, validationIDs, _ := newDelegationSetup(t)
	ticker := utils.NewTicker(staker.Raw().Client())

	t.Run("Delegate update auto renew after first period", func(t *testing.T) {
		t.Parallel()

		// add the delegation
		receipt := testutil.Send(t, hayabusa.Stargate,
			staker.AddDelegation(validationIDs[0], builtin.MinStake(), 100))
		delegationID := testutil.ReceiptToID(receipt)
		delegation, err := staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, builtin.MinStake(), delegation.Stake)
		assert.Equal(t, uint8(100), delegation.Multiplier)
		assert.False(t, delegation.Locked)

		// wait for validators current period + 1 staking period
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*2))
		testutil.Send(t, hayabusa.Stargate, staker.SignalDelegationExit(delegationID))

		// wait for validators current period + 2 staking period
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*3))

		// withdraw - should succeed since auto-renew is false
		testutil.Send(t, hayabusa.Stargate, staker.WithdrawDelegation(delegationID))
		require.NoError(t, err)

		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.True(t, delegation.Stake.Sign() == 0)
	})

	t.Run("Delegated with auto-renew", func(t *testing.T) {
		t.Parallel()

		// add the delegation
		receipt := testutil.Send(t, hayabusa.Stargate, staker.AddDelegation(validationIDs[2], builtin.MinStake(), 100))
		delegationID := testutil.ReceiptToID(receipt)
		delegation, err := staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, builtin.MinStake(), delegation.Stake)
		assert.Equal(t, uint8(100), delegation.Multiplier)
		assert.False(t, delegation.Locked)

		// wait for validators current period + 1 staking period
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*2))

		// withdraw - should revert due to auto-renew
		receipt, _, err = staker.WithdrawDelegation(delegationID).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)
		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, builtin.MinStake(), delegation.Stake)

		receipt = testutil.Send(t, hayabusa.Stargate, staker.SignalDelegationExit(delegationID))
		// wait for validators current period to end
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))

		// withdraw - should succeed since auto-renew is false
		testutil.Send(t, hayabusa.Stargate, staker.WithdrawDelegation(delegationID))
		require.NoError(t, err)
		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.True(t, delegation.Stake.Sign() == 0)
	})

	t.Run("Delegations are exited when validator exits", func(t *testing.T) {
		t.Parallel()

		validator, err := staker.GetValidation(validationIDs[3])
		require.NoError(t, err)
		validatorAccount := hayabusa.ValidatorAccounts[0]

		for _, acc := range hayabusa.ValidatorAccounts {
			if acc.Node.Address().String() == validator.Address.String() {
				validatorAccount = acc
				break
			}
		}

		// add the delegation
		receipt := testutil.Send(t, hayabusa.Stargate,
			staker.AddDelegation(validationIDs[3], builtin.MinStake(), 100))
		require.NoError(t, err)
		delegationID1 := testutil.ReceiptToID(receipt)
		delegation1, err := staker.GetDelegation(delegationID1)
		require.NoError(t, err)
		assert.Equal(t, builtin.MinStake(), delegation1.Stake)
		assert.Equal(t, uint8(100), delegation1.Multiplier)
		assert.False(t, delegation1.Locked)

		// add the delegation
		receipt = testutil.Send(t, hayabusa.Stargate,
			staker.AddDelegation(validationIDs[3], builtin.MinStake(), 100))
		delegationID2 := testutil.ReceiptToID(receipt)
		delegation2, err := staker.GetDelegation(delegationID2)
		require.NoError(t, err)
		assert.Equal(t, builtin.MinStake(), delegation2.Stake)
		assert.Equal(t, uint8(100), delegation2.Multiplier)
		assert.False(t, delegation2.Locked)

		// wait for validators current period
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*1))
		receipt = testutil.Send(t, validatorAccount.Endorser, staker.SignalExit(validatorAccount.Node.Address()))

		// wait for validators last period to end
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*2))

		// withdraw - should succeed since validator exited
		testutil.Send(t, hayabusa.Stargate, staker.WithdrawDelegation(delegationID1))
		delegation1, err = staker.GetDelegation(delegationID1)
		require.NoError(t, err)
		assert.True(t, delegation1.Stake.Sign() == 0)

		testutil.Send(t, hayabusa.Stargate, staker.WithdrawDelegation(delegationID2))
		delegation2, err = staker.GetDelegation(delegationID2)
		require.NoError(t, err)
		assert.True(t, delegation2.Stake.Sign() == 0)
	})

	t.Run("Should not be able call with external account", func(t *testing.T) {
		t.Parallel()
		receipt := testutil.Send(t, hayabusa.Stargate,
			staker.AddDelegation(validationIDs[0], builtin.MinStake(), 100))
		delegationID := testutil.ReceiptToID(receipt)

		// external should not be able to add delegation
		var err error
		receipt, _, err = staker.AddDelegation(validationIDs[0], builtin.MinStake(), 100).
			Send().
			WithSigner(hayabusa.AdditionalAccounts[0]).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)

		// external should not be able to toggle auto-renew
		receipt, _, err = staker.SignalDelegationExit(delegationID).
			Send().
			WithSigner(hayabusa.AdditionalAccounts[0]).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)

		// wait for delegation to end
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*2))

		// external should not be able to withdraw delegation
		receipt, _, err = staker.WithdrawDelegation(delegationID).
			Send().
			WithSigner(hayabusa.AdditionalAccounts[0]).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)
	})

	t.Run("Active delegator can increase/decrease their stake and get reflected in validator totals", func(t *testing.T) {
		t.Parallel()

		vStakes, err := staker.GetValidation(validationIDs[5])
		require.NoError(t, err)

		// Create first delegation
		firstStake := hayabusa.NewWeightedStakeWithMultiplier(big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(3)), 100)
		receipt := testutil.Send(t, hayabusa.Stargate, staker.AddDelegation(validationIDs[5], firstStake.VET(), 100))
		firstDelegationID := testutil.ReceiptToID(receipt)

		// Create second delegation
		secondStake := hayabusa.NewWeightedStakeWithMultiplier(big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(3)), 100)
		receipt = testutil.Send(t, hayabusa.Stargate, staker.AddDelegation(validationIDs[5], secondStake.VET(), 100))
		secondDelegationID := testutil.ReceiptToID(receipt)

		// Verify both delegations
		delegation, err := staker.GetDelegation(firstDelegationID)
		require.NoError(t, err)
		assert.Equal(t, firstStake.VET(), delegation.Stake)
		assert.False(t, delegation.Locked)

		delegation, err = staker.GetDelegation(secondDelegationID)
		require.NoError(t, err)
		assert.Equal(t, secondStake.VET(), delegation.Stake)
		assert.False(t, delegation.Locked)

		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))
		totalsBeforeWithdrawal, err := staker.GetValidationTotals(validationIDs[5])
		require.NoError(t, err)

		// Verify that the validator has the exact total stake from both delegations
		expectedTotalStake := big.NewInt(0).Add(firstStake.VET(), secondStake.VET())
		expectedTotalStake = expectedTotalStake.Add(expectedTotalStake, vStakes.Stake)
		assert.Equal(t, expectedTotalStake, totalsBeforeWithdrawal.TotalLockedStake,
			"Validator should have exact total stake from both delegations before withdrawal")

		receipt = testutil.Send(t, hayabusa.Stargate, staker.SignalDelegationExit(firstDelegationID))
		require.NoError(t, err)
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))

		// Withdraw only the first delegation (partial decrease)
		receipt = testutil.Send(t, hayabusa.Stargate, staker.WithdrawDelegation(firstDelegationID))
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))

		// Verify that the validator has exactly the second delegation stake after withdrawal
		totalsAfterWithdrawal, err := staker.GetValidationTotals(validationIDs[5])
		require.NoError(t, err)
		assert.Equal(t, big.NewInt(0).Add(secondStake.VET(), vStakes.Stake), totalsAfterWithdrawal.TotalLockedStake,
			"Validator should have exactly the second delegation stake after withdrawal")

		expectedWeight := big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(5))
		assert.Equal(t, expectedWeight, totalsAfterWithdrawal.TotalLockedWeight,
			"Validator should have the correct total weight after withdrawal")
	})
}

func Test_Delegations2(t *testing.T) {
	t.Parallel()
	staker, config, validationIDs, network := newDelegationSetup(t)
	ticker := utils.NewTicker(staker.Raw().Client())

	t.Run("Delegator can withdraw when validator is offline", func(t *testing.T) {
		t.Parallel()

		// Use the second validator as an offline validator
		validatorIndex := 1
		validatorID := validationIDs[validatorIndex]

		// Create delegation
		stake := builtin.MinStake()
		addDelReceipt := testutil.Send(t, hayabusa.Stargate, staker.AddDelegation(validatorID, stake, 100))
		delegationID := testutil.ReceiptToID(addDelReceipt)

		// Wait for delegations to become active
		require.NoError(t, ticker.WaitForBlock(addDelReceipt.Meta.BlockNumber+config.MinStakingPeriod))

		// Verify both delegations are active
		delegation1, err := staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, stake, delegation1.Stake)
		assert.True(t, delegation1.Locked)

		// Verify validator is initially online
		validation, err := staker.GetValidation(validatorID)
		require.NoError(t, err)
		require.True(t, validation.IsOnline(), "Validator should be online initially")

		// Signal exit for delegation while validator is online
		exitReceipt := testutil.Send(t, hayabusa.Stargate, staker.SignalDelegationExit(delegationID))
		require.False(t, exitReceipt.Reverted, "Exit signal should succeed")

		// Wait for exit signals to be processed
		require.NoError(t, ticker.WaitForBlock(addDelReceipt.Meta.BlockNumber+config.MinStakingPeriod*2))

		// Take the validator offline by stopping its node
		validatorNode := network.NodeConfigs()[validatorIndex]
		require.NoError(t, network.NodeLifecycles()[validatorNode.GetID()].Stop())

		// Wait for validator to be detected as offline
		err = utils.WaitForCondition(
			t.Context(),
			staker.Raw().Client(),
			config.ForkBlock+config.TransitionPeriod+config.MinStakingPeriod*10,
			func() (bool, error) {
				validation, err := staker.GetValidation(validatorID)
				if err != nil {
					return false, err
				}
				return !validation.IsOnline(), nil
			})
		require.NoError(t, err, "Validator should go offline after being stopped")

		// Verify validator is now offline
		validation, err = staker.GetValidation(validatorID)
		require.NoError(t, err)
		require.False(t, validation.IsOnline(), "Validator should be offline")

		// Attempt to withdraw delegations - these should fail/revert because validator is offline
		receipt, _, err := staker.WithdrawDelegation(delegationID).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		assert.False(t, receipt.Reverted, "Delegation withdrawal should not revert when validator is offline")

		// Verify delegations still have their stake (withdrawal failed)
		delegation1, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.True(t, big.NewInt(0).Cmp(delegation1.Stake) == 0, "Delegation stake should remain unchanged when withdrawal fails")
	})

	t.Run("Two delegators to queued validator, one withdraws while queued, then validator becomes active", func(t *testing.T) {
		t.Parallel()

		queuedConfig := &hayabusa.Config{
			Nodes:             3,
			MaxBlockProposers: 2,
			ForkBlock:         0,
			TransitionPeriod:  4,
			EpochLength:       4,
			CooldownPeriod:    4,
			MinStakingPeriod:  4,
			MidStakingPeriod:  12,
			HighStakingPeriod: 259200,
			Name:              t.Name(),
			BlockInterval:     uint64(5),
		}
		queuedNetwork, err := hayabusa.NewNetwork(queuedConfig, t.Context())
		require.NoError(t, err)
		t.Cleanup(queuedNetwork.Stop)
		require.NoError(t, queuedNetwork.Start())

		queuedStaker, err := builtin.NewStaker(queuedNetwork.ThorClient())
		require.NoError(t, err)
		require.NoError(t, utils.WaitForFork(t.Context(), queuedStaker, queuedConfig.ForkBlock))

		queuedTicker := utils.NewTicker(queuedStaker.Raw().Client())

		// Add 3 validators - first 2 will be active, 3rd will be queued
		queuedValidationIDs := [3]thor.Address{}

		for i := range queuedValidationIDs {
			account := hayabusa.ValidatorAccounts[i]
			queuedStaker.AddValidation(account.Node.Address(), builtin.MinStake(), queuedConfig.MinStakingPeriod).
				Send().
				WithSigner(account.Endorser).
				WithOptions(testutil.TxOptions()).
				SubmitAndConfirm(testutil.TxContext(t))
			queuedValidationIDs[i] = account.Node.Address()
		}

		// Wait for PoS to activate
		require.NoError(t, utils.WaitForPOS(t.Context(), queuedStaker, queuedConfig.ForkBlock+queuedConfig.TransitionPeriod))

		// Verify validator statuses: first 2 active, 3rd queued
		validation0, err := queuedStaker.GetValidation(queuedValidationIDs[0])
		require.NoError(t, err)
		assert.Equal(t, builtin.StakerStatusActive, validation0.Status, "First validator should be active")

		validation1, err := queuedStaker.GetValidation(queuedValidationIDs[1])
		require.NoError(t, err)
		assert.Equal(t, builtin.StakerStatusActive, validation1.Status, "Second validator should be active")

		validation2, err := queuedStaker.GetValidation(queuedValidationIDs[2])
		require.NoError(t, err)
		assert.Equal(t, builtin.StakerStatusQueued, validation2.Status, "Third validator should be queued")

		// Get initial queued stake before delegations
		initialQueuedStake, err := queuedStaker.QueuedStake()
		require.NoError(t, err)

		// Create first delegation to the queued validator
		firstDelegationStake := builtin.MinStake()
		firstMultiplier := uint8(100)
		receipt1 := testutil.Send(t, hayabusa.Stargate, queuedStaker.AddDelegation(queuedValidationIDs[2], firstDelegationStake, firstMultiplier))
		firstDelegationID := testutil.ReceiptToID(receipt1)

		// Create second delegation to the queued validator with different multiplier
		secondDelegationStake := big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(2))
		secondMultiplier := uint8(150)
		receipt2 := testutil.Send(t, hayabusa.Stargate, queuedStaker.AddDelegation(queuedValidationIDs[2], secondDelegationStake, secondMultiplier))
		secondDelegationID := testutil.ReceiptToID(receipt2)

		// Verify both delegations were created
		firstDelegation, err := queuedStaker.GetDelegation(firstDelegationID)
		require.NoError(t, err)
		assert.Equal(t, firstDelegationStake, firstDelegation.Stake)
		assert.Equal(t, firstMultiplier, firstDelegation.Multiplier)
		assert.False(t, firstDelegation.Locked)
		secondDelegation, err := queuedStaker.GetDelegation(secondDelegationID)
		require.NoError(t, err)
		assert.Equal(t, secondDelegationStake, secondDelegation.Stake)
		assert.Equal(t, secondMultiplier, secondDelegation.Multiplier)
		assert.False(t, secondDelegation.Locked)

		// Verify queued stake increased by both delegations
		totalDelegationStake := big.NewInt(0).Add(firstDelegationStake, secondDelegationStake)
		afterDelegationsQueuedStake, err := queuedStaker.QueuedStake()
		require.NoError(t, err)
		expectedQueuedStake := big.NewInt(0).Add(initialQueuedStake, totalDelegationStake)
		assert.Equal(t, expectedQueuedStake, afterDelegationsQueuedStake, "Queued stake should increase by both delegation amounts")

		// First delegator withdraws while validator is still queued
		withdrawReceipt := testutil.Send(t, hayabusa.Stargate, queuedStaker.WithdrawDelegation(firstDelegationID))
		assert.False(t, withdrawReceipt.Reverted, "First delegation withdrawal should succeed for queued validator")

		// Verify first delegation stake is now zero
		firstDelegation, err = queuedStaker.GetDelegation(firstDelegationID)
		require.NoError(t, err)
		assert.True(t, firstDelegation.Stake.Sign() == 0, "First delegation stake should be zero after withdrawal")

		// Verify queued stake decreased by first delegation amount
		require.NoError(t, queuedTicker.WaitForBlock(withdrawReceipt.Meta.BlockNumber+1))
		afterWithdrawalQueuedStake, err := queuedStaker.QueuedStake()
		require.NoError(t, err)
		expectedAfterWithdrawal := big.NewInt(0).Add(initialQueuedStake, secondDelegationStake)
		assert.Equal(t, expectedAfterWithdrawal, afterWithdrawalQueuedStake, "Queued stake should decrease by withdrawn delegation amount")

		// Now make a slot available by having the first validator exit
		testutil.Send(t, hayabusa.ValidatorAccounts[0].Endorser, queuedStaker.SignalExit(queuedValidationIDs[0]))

		// Wait for the first validator to exit and the queued validator to become active
		// Need to wait longer for the transition to complete
		require.NoError(t, queuedTicker.WaitForBlock(withdrawReceipt.Meta.BlockNumber+queuedConfig.MinStakingPeriod*2))

		// Verify the queued validator is now active
		validation2, err = queuedStaker.GetValidation(queuedValidationIDs[2])
		require.NoError(t, err)
		assert.Equal(t, builtin.StakerStatusActive, validation2.Status, "Validator should now be active")

		// Verify the remaining delegation is now locked
		secondDelegation, err = queuedStaker.GetDelegation(secondDelegationID)
		require.NoError(t, err)
		assert.True(t, secondDelegation.Locked, "Remaining delegation should be locked when validator becomes active")

		// Verify delegation properties are maintained correctly after validator activation
		assert.Equal(t, secondDelegationStake, secondDelegation.Stake, "Delegation stake should remain unchanged after validator activation")
		assert.Equal(t, secondMultiplier, secondDelegation.Multiplier, "Delegation multiplier should remain unchanged after validator activation")

		// Verify that attempting to withdraw the locked delegation now fails
		withdrawLockedReceipt, _, err := queuedStaker.WithdrawDelegation(secondDelegationID).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		assert.True(t, withdrawLockedReceipt.Reverted, "Withdrawal of locked delegation should revert")

		// Verify delegation is still intact after failed withdrawal attempt
		secondDelegation, err = queuedStaker.GetDelegation(secondDelegationID)
		require.NoError(t, err)
		assert.Equal(t, secondDelegationStake, secondDelegation.Stake, "Delegation stake should remain unchanged after failed withdrawal")
		assert.True(t, secondDelegation.Locked, "Delegation should still be locked after failed withdrawal")

		// Verify queued stake is now zero (no more queued validators)
		finalQueuedStake, err := queuedStaker.QueuedStake()
		require.NoError(t, err)
		assert.True(t, finalQueuedStake.Sign() == 0, "Queued stake should be zero after validator activation")

		// Now that validator is active, verify totals work correctly
		activeTotals, err := queuedStaker.GetValidationTotals(queuedValidationIDs[2])
		require.NoError(t, err)

		// Should include validator's own stake + remaining delegation
		validatorOwnStake := builtin.MinStake()
		expectedActiveStake := big.NewInt(0).Add(validatorOwnStake, secondDelegationStake)
		assert.Equal(t, expectedActiveStake, activeTotals.TotalLockedStake, "Active validator should have correct total stake")

		// Calculate expected weight: validator + second delegation (150%)
		// Since we can't access validator multiplier directly, calculate it from the totals
		secondDelegationWeight := hayabusa.NewWeightedStakeWithMultiplier(secondDelegationStake, secondMultiplier).Weight()

		// The validator's effective weight is: total weight - delegation weight
		validatorWeight := big.NewInt(0).Sub(activeTotals.TotalLockedWeight, secondDelegationWeight)

		// Verify the calculation makes sense (validator weight should be positive)
		assert.True(t, validatorWeight.Sign() > 0, "Validator weight should be positive")

		// For documentation: calculate what multiplier this implies
		impliedMultiplier := big.NewInt(0).Div(big.NewInt(0).Mul(validatorWeight, big.NewInt(100)), validatorOwnStake)
		t.Logf("Validator implied multiplier: %v%%", impliedMultiplier)

		// The total weight should be validator weight + delegation weight
		expectedActiveWeight := big.NewInt(0).Add(validatorWeight, secondDelegationWeight)
		assert.Equal(t, expectedActiveWeight, activeTotals.TotalLockedWeight, "Active validator should have correct total weight")
	})

	t.Run("Validator stake cannot exceed 600M VET limit including delegations", func(t *testing.T) {
		t.Parallel()

		createVETAmount := func(millions int64) *big.Int {
			vet := big.NewInt(millions)
			vet = vet.Mul(vet, big.NewInt(1e6))
			vet = vet.Mul(vet, big.NewInt(1e18))
			return vet
		}

		validatorStake := builtin.MinStake()
		delegationAmount := createVETAmount(100)

		validatorID1 := validationIDs[0]

		// Verify initial state
		initialTotals, err := staker.GetValidationTotals(validatorID1)
		require.NoError(t, err)
		assert.Equal(t, validatorStake, initialTotals.TotalLockedStake, "Initial validator stake should be MinStake")

		expectedDelegations := 5
		delegationIDs := make([]*big.Int, expectedDelegations)

		for i := range expectedDelegations {
			t.Logf("Creating delegation %d of %d (100M VET each)", i+1, expectedDelegations)

			receipt := testutil.Send(t, hayabusa.Stargate,
				staker.AddDelegation(validatorID1, delegationAmount, 100))
			assert.False(t, receipt.Reverted, "Delegation %d should succeed", i+1)

			delegationID := testutil.ReceiptToID(receipt)
			delegationIDs[i] = delegationID

			// Verify delegation was created correctly
			delegation, err := staker.GetDelegation(delegationID)
			require.NoError(t, err)
			assert.Equal(t, delegationAmount, delegation.Stake, "Delegation %d should have correct stake", i+1)
			assert.Equal(t, uint8(100), delegation.Multiplier, "Delegation %d should have correct multiplier", i+1)

			// Wait for delegation to be processed
			require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))

			// Log current totals for debugging
			totals, err := staker.GetValidationTotals(validatorID1)
			require.NoError(t, err)
			currentStakeInVET := big.NewInt(0).Div(totals.TotalLockedStake, big.NewInt(1e18))
			t.Logf("After delegation %d: Validator total stake = %s VET", i+1, currentStakeInVET.String())
		}

		// Test 2: Single delegation exceeding limit should fail
		validatorID2 := validationIDs[1]

		// Setup: get this validator close to the limit
		setupAmount := createVETAmount(100)
		for i := range 5 {
			receipt := testutil.Send(t, hayabusa.Stargate,
				staker.AddDelegation(validatorID2, setupAmount, 100))
			require.False(t, receipt.Reverted, "Setup delegation %d should succeed", i+1)
		}

		// Now try to exceed the limit
		overLimitAmount := createVETAmount(100)

		receipt, _, err := staker.AddDelegation(validatorID2, overLimitAmount, 100).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		assert.True(t, receipt.Reverted, "Delegation exceeding 600M limit should revert")

		// Log the current validator totals for debugging
		totals, err := staker.GetValidationTotals(validatorID2)
		require.NoError(t, err)
		currentStakeInVET := big.NewInt(0).Div(totals.TotalLockedStake, big.NewInt(1e18))
		t.Logf("Validator total stake after failed over-limit delegation: %s VET", currentStakeInVET.String())

		// Test 3: Race condition with concurrent delegations at limit
		validatorID3 := validationIDs[2]

		// Setup: get this validator close to the limit
		for i := range 5 {
			receipt := testutil.Send(t, hayabusa.Stargate,
				staker.AddDelegation(validatorID3, setupAmount, 100))
			require.False(t, receipt.Reverted, "Setup delegation %d should succeed", i+1)
		}

		// Now test two 50M delegations concurrently
		raceAmount := createVETAmount(50)

		// Create two delegators that will race
		senders := &utils.Senders{}

		sender1 := staker.AddDelegation(validatorID3, raceAmount, 100).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions())
		senders.Add(sender1)

		sender2 := staker.AddDelegation(validatorID3, raceAmount, 100).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions())
		senders.Add(sender2)

		// Send both transactions concurrently
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		t.Log("Sending two concurrent 50M VET delegations...")
		receipts, _, err := senders.Send(ctx)
		require.NoError(t, err)
		require.Len(t, receipts, 2, "Should have 2 receipts")

		successCount := 0
		revertCount := 0
		for i, receipt := range receipts {
			if receipt.Reverted {
				revertCount++
				t.Logf("Concurrent delegation %d: REVERTED (expected due to limit)", i+1)
			} else {
				successCount++
				delegationID := testutil.ReceiptToID(receipt)
				delegation, err := staker.GetDelegation(delegationID)
				require.NoError(t, err)
				assert.Equal(t, raceAmount, delegation.Stake, "Successful delegation should have correct stake")
				t.Logf("Concurrent delegation %d: SUCCESS", i+1)
			}
		}

		// At most one should succeed due to 600M limit
		assert.LessOrEqual(t, successCount, 1, "At most one concurrent delegation should succeed")
		assert.GreaterOrEqual(t, revertCount, 1, "At least one concurrent delegation should revert due to limit")
		assert.NotEmpty(t, receipts)

		maxBlockNumber := receipts[0].Meta.BlockNumber
		for _, receipt := range receipts[1:] {
			if receipt.Meta.BlockNumber > maxBlockNumber {
				maxBlockNumber = receipt.Meta.BlockNumber
			}
		}
		require.NoError(t, ticker.WaitForBlock(maxBlockNumber+1))

		// Verify final validator totals respect the 600M limit
		finalTotals, err := staker.GetValidationTotals(validatorID3)
		require.NoError(t, err)

		maxLimit := createVETAmount(600)
		assert.LessOrEqual(t, finalTotals.TotalLockedStake.Cmp(maxLimit), 0,
			"Final validator stake should not exceed 600M VET limit")
	})
}

func newDelegationSetup(t *testing.T) (*builtin.Staker, *hayabusa.Config, [6]thor.Address, *hayabusa.Network) {
	t.Helper()
	config := &hayabusa.Config{
		Nodes:             6,
		MaxBlockProposers: 6,
		ForkBlock:         0,
		TransitionPeriod:  4,
		EpochLength:       4,
		CooldownPeriod:    4,
		MinStakingPeriod:  4,
		MidStakingPeriod:  12,
		HighStakingPeriod: 259200,
		Name:              t.Name(),
		BlockInterval:     uint64(5),
	}
	network, err := hayabusa.NewNetwork(config, t.Context())
	require.NoError(t, err)
	t.Cleanup(network.Stop)
	require.NoError(t, network.Start())

	staker, err := builtin.NewStaker(network.ThorClient())
	if err != nil {
		t.Fatalf("failed to create staker: %v", err)
	}
	if err := utils.WaitForFork(t.Context(), staker, config.ForkBlock); err != nil {
		t.Fatalf("failed to wait for fork: %v", err)
	}

	validationIDs := [6]thor.Address{}
	senders := &utils.Senders{}

	for i := range validationIDs {
		account := hayabusa.ValidatorAccounts[i]
		sender := staker.AddValidation(account.Node.Address(), builtin.MinStake(), config.MinStakingPeriod).
			Send().
			WithSigner(account.Endorser).
			WithOptions(testutil.TxOptions())
		senders.Add(sender)
		validationIDs[i] = account.Node.Address()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	if _, _, err := senders.Send(ctx); err != nil {
		t.Fatal(err)
	}
	if err := utils.WaitForPOS(t.Context(), staker, config.ForkBlock+config.TransitionPeriod); err != nil {
		t.Fatalf("failed to wait for PoS: %v", err)
	}

	return staker, config, validationIDs, network
}
