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
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/logdb"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func Test_StargateRewards(t *testing.T) {
	t.Parallel()
	staker, config, validationIDs := newDelegationSetup(t)

	expectedStake := new(big.Int).Mul(builtin.MinStake(), big.NewInt(int64(len(validationIDs))))
	stargateAddr := hayabusa.Stargate.Address()

	for _, validationID := range validationIDs { // evenly distribute delegations among validators
		senders := &utils.Senders{}
		for range 10 {
			sender := staker.AddDelegation(validationID, builtin.MinStake(), 200).Send().WithSigner(hayabusa.Stargate).WithOptions(testutil.TxOptions())
			senders.Add(sender)
			expectedStake = expectedStake.Add(expectedStake, builtin.MinStake())
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
	assert.Equal(t, builtin.MinStake(), big.NewInt(0).Sub(totals.TotalLockedStake, totals.DelegationsLockedStake))
	assert.Equal(t, big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(2)), big.NewInt(0).Sub(totals.TotalLockedWeight, totals.DelegationsLockedWeight))
	assert.Equal(t, big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(10)), totals.DelegationsLockedStake)
	assert.Equal(t, big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(20)), totals.DelegationsLockedWeight)
}

func Test_Delegations_Delegate1PeriodOnly(t *testing.T) {
	t.Parallel()
	staker, config, validationIDs := newDelegationSetup(t)
	ticker := utils.NewTicker(staker.Raw().Client())

	multiplier := uint8(100)
	receipt := testutil.Send(t, hayabusa.Stargate,
		staker.AddDelegation(validationIDs[0], builtin.MinStake(), multiplier))
	delegationID := receiptToID(receipt)
	delegation, err := staker.GetDelegation(delegationID)
	require.NoError(t, err)
	assert.Equal(t, builtin.MinStake(), delegation.Stake)
	assert.Equal(t, uint8(100), delegation.Multiplier)
	assert.False(t, delegation.Locked)
	require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))

	previousTotalStake, previousTotalWeight, err := staker.TotalStake()
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

	expectedWeight := big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(int64(multiplier)))
	expectedWeight = expectedWeight.Quo(expectedWeight, big.NewInt(100))
	expectedWeight = big.NewInt(0).Sub(previousTotalWeight, expectedWeight)
	assert.Equal(t, expectedWeight, currentTotalWeight,
		"Wrong weight after exit")
}

func Test_Delegations(t *testing.T) {
	staker, config, validationIDs := newDelegationSetup(t)
	ticker := utils.NewTicker(staker.Raw().Client())

	t.Run("Delegate update auto renew after first period", func(t *testing.T) {
		t.Parallel()

		// add the delegation
		receipt := testutil.Send(t, hayabusa.Stargate,
			staker.AddDelegation(validationIDs[0], builtin.MinStake(), 100))
		delegationID := receiptToID(receipt)
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
		delegationID := receiptToID(receipt)
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

		validator, err := staker.Get(validationIDs[3])
		require.NoError(t, err)
		validatorAccount := hayabusa.ValidatorAccounts[0]

		for _, acc := range hayabusa.ValidatorAccounts {
			if acc.Address().String() == validator.Address.String() {
				validatorAccount = acc
				break
			}
		}

		// add the delegation
		receipt := testutil.Send(t, hayabusa.Stargate,
			staker.AddDelegation(validationIDs[3], builtin.MinStake(), 100))
		require.NoError(t, err)
		delegationID1 := receiptToID(receipt)
		delegation1, err := staker.GetDelegation(delegationID1)
		require.NoError(t, err)
		assert.Equal(t, builtin.MinStake(), delegation1.Stake)
		assert.Equal(t, uint8(100), delegation1.Multiplier)
		assert.False(t, delegation1.Locked)

		// add the delegation
		receipt = testutil.Send(t, hayabusa.Stargate,
			staker.AddDelegation(validationIDs[3], builtin.MinStake(), 100))
		delegationID2 := receiptToID(receipt)
		delegation2, err := staker.GetDelegation(delegationID2)
		require.NoError(t, err)
		assert.Equal(t, builtin.MinStake(), delegation2.Stake)
		assert.Equal(t, uint8(100), delegation2.Multiplier)
		assert.False(t, delegation2.Locked)

		// wait for validators current period
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*1))
		receipt = testutil.Send(t, validatorAccount, staker.SignalExit(validationIDs[3]))

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
		delegationID := receiptToID(receipt)

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

		// Create first delegation
		firstStake := big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(2))
		receipt := testutil.Send(t, hayabusa.Stargate, staker.AddDelegation(validationIDs[5], firstStake, 100))
		firstDelegationID := receiptToID(receipt)

		// Create second delegation
		secondStake := big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(3))
		receipt = testutil.Send(t, hayabusa.Stargate, staker.AddDelegation(validationIDs[5], secondStake, 100))
		secondDelegationID := receiptToID(receipt)

		// Verify both delegations
		delegation, err := staker.GetDelegation(firstDelegationID)
		require.NoError(t, err)
		assert.Equal(t, firstStake, delegation.Stake)
		assert.False(t, delegation.Locked)

		delegation, err = staker.GetDelegation(secondDelegationID)
		require.NoError(t, err)
		assert.Equal(t, secondStake, delegation.Stake)
		assert.False(t, delegation.Locked)

		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))
		totalsBeforeWithdrawal, err := staker.GetValidationTotals(validationIDs[5])
		require.NoError(t, err)

		// Verify that the validator has the exact total stake from both delegations
		expectedTotalStake := big.NewInt(0).Add(firstStake, secondStake)
		assert.Equal(t, expectedTotalStake, totalsBeforeWithdrawal.DelegationsLockedStake,
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
		assert.Equal(t, secondStake, totalsAfterWithdrawal.DelegationsLockedStake,
			"Validator should have exactly the second delegation stake after withdrawal")
		assert.True(t, totalsAfterWithdrawal.DelegationsLockedWeight.Cmp(big.NewInt(0)) > 0,
			"Validator should have positive weight after withdrawal")
	})
}

func newDelegationSetup(t *testing.T) (*builtin.Staker, *hayabusa.Config, [6]thor.Address) {
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
	}
	network := hayabusa.NewNetwork(config, t.Context())
	t.Cleanup(network.Stop)
	require.NoError(t, network.Start())

	staker, err := builtin.NewStaker(network.ThorClient())
	if err != nil {
		t.Fatalf("failed to create staker: %v", err)
	}
	if err := utils.WaitForFork(staker, config.ForkBlock); err != nil {
		t.Fatalf("failed to wait for fork: %v", err)
	}

	validationIDs := [6]thor.Address{}
	senders := &utils.Senders{}

	for i := range validationIDs {
		account := hayabusa.ValidatorAccounts[i]
		sender := staker.AddValidation(account.Address(), builtin.MinStake(), config.MinStakingPeriod).
			Send().
			WithSigner(account).
			WithOptions(testutil.TxOptions())
		senders.Add(sender)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	if _, _, err := senders.Send(ctx); err != nil {
		t.Fatal(err)
	}
	if err := utils.WaitForPOS(staker, config.ForkBlock+config.TransitionPeriod); err != nil {
		t.Fatalf("failed to wait for PoS: %v", err)
	}
	events, err := staker.FilterValidatorQueued(nil, nil, logdb.ASC)
	if err != nil {
		t.Fatalf("failed to filter validator queued: %v", err)
	}
	for i, event := range events {
		validationIDs[i] = event.Node
	}
	return staker, config, validationIDs
}

func receiptToID(receipt *api.Receipt) *big.Int {
	// 0 is the event, 1 is the validation ID
	return new(big.Int).SetBytes(receipt.Outputs[0].Events[0].Topics[2][:])
}
