package delegations

import (
	"context"
	"math/big"
	"strconv"
	"testing"
	"time"

	"github.com/vechain/thor/v2/thorclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/api/transactions"
	"github.com/vechain/thor/v2/logdb"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func Test_StargateRewards(t *testing.T) {
	// Setup
	staker, config, validationIDs := newDelegationSetup(t)

	expectedStake := new(big.Int).Mul(builtin.MinStake(), big.NewInt(int64(len(validationIDs))))
	stargateAddr := hayabusa.Stargate.Address()

	for _, validationID := range validationIDs { // evenly distribute delegations among validators
		senders := &utils.Senders{}
		for range 10 {
			sender := staker.AddDelegation(validationID, builtin.MinStake(), true, 200).Send().WithSigner(hayabusa.Stargate).WithOptions(testutil.TxOptions())
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
	blockNEnergy := (big.Int)(acc.Energy)

	assert.NoError(t, ticker.WaitForBlock(best.Number+1))

	// block N+1 energy
	acc, err = staker.Raw().Client().Account(&stargateAddr, thorclient.Revision(strconv.Itoa(int(best.Number+1))))
	require.NoError(t, err)
	blockNPlus1Energy := (big.Int)(acc.Energy)

	// assert plus1 is greater than N
	assert.True(t, blockNPlus1Energy.Cmp(&blockNEnergy) > 0, "block N+1 energy should be greater than block N energy")

	totals, err := staker.GetValidatorsTotals(validationIDs[0])
	require.NoError(t, err)
	assert.Equal(t, builtin.MinStake(), big.NewInt(0).Sub(totals.TotalLockedStake, totals.DelegationsLockedStake))
	assert.Equal(t, big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(2)), big.NewInt(0).Sub(totals.TotalLockedWeight, totals.DelegationsLockedWeight))
	assert.Equal(t, big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(10)), totals.DelegationsLockedStake)
	assert.Equal(t, big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(20)), totals.DelegationsLockedWeight)
}

func Test_Delegations(t *testing.T) {
	staker, config, validationIDs := newDelegationSetup(t)
	ticker := utils.NewTicker(staker.Raw().Client())

	t.Run("Delegate for 1 period only", func(t *testing.T) {
		t.Parallel()

		// add the delegation
		receipt, _, err := staker.AddDelegation(validationIDs[0], builtin.MinStake(), false, 100).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		delegationID := receiptToID(receipt)
		delegation, err := staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, builtin.MinStake(), delegation.Stake)
		assert.Equal(t, uint8(100), delegation.Multiplier)
		assert.False(t, delegation.AutoRenew)

		// wait for validators current period + 1 staking period
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*2))

		// withdraw - should succeed since auto-renew is false
		receipt, _, err = staker.WithdrawDelegation(delegationID).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		require.False(t, receipt.Reverted)

		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.True(t, delegation.Stake.Sign() == 0)
	})

	t.Run("Immediate enable auto-renew", func(t *testing.T) {
		t.Parallel()

		// add the delegation
		receipt, _, err := staker.AddDelegation(validationIDs[1], builtin.MinStake(), false, 100).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		delegationID := receiptToID(receipt)
		delegation, err := staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, builtin.MinStake(), delegation.Stake)
		assert.Equal(t, uint8(100), delegation.Multiplier)
		assert.False(t, delegation.AutoRenew)

		// immediately enable auto-renew
		receipt, _, err = staker.UpdateDelegationAutoRenew(delegationID, true).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.True(t, delegation.AutoRenew)

		// wait for validators current period + 1 staking period
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*2))

		// withdraw - should fail since auto-renew is true
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
	})

	t.Run("Delegated with auto-renew", func(t *testing.T) {
		t.Parallel()

		// add the delegation
		receipt, _, err := staker.AddDelegation(validationIDs[2], builtin.MinStake(), true, 100).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		delegationID := receiptToID(receipt)
		delegation, err := staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, builtin.MinStake(), delegation.Stake)
		assert.Equal(t, uint8(100), delegation.Multiplier)
		assert.True(t, delegation.AutoRenew)

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

		receipt, _, err = staker.UpdateDelegationAutoRenew(delegationID, false).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)

		// wait for validators current period to end
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))

		// withdraw - should succeed since auto-renew is false
		receipt, _, err = staker.WithdrawDelegation(delegationID).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		require.False(t, receipt.Reverted)
		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.True(t, delegation.Stake.Sign() == 0)
	})

	t.Run("Should not be able call with external account", func(t *testing.T) {
		t.Parallel()
		receipt, _, err := staker.AddDelegation(validationIDs[0], builtin.MinStake(), false, 100).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		delegationID := receiptToID(receipt)

		// external should not be able to add delegation
		receipt, _, err = staker.AddDelegation(validationIDs[0], builtin.MinStake(), false, 100).
			Send().
			WithSigner(hayabusa.AdditionalAccounts[0]).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)

		// external should not be able to toggle auto-renew
		receipt, _, err = staker.UpdateDelegationAutoRenew(delegationID, true).
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

	t.Run("Active delegator can increase and decrease their stake and get reflected in validator totals", func(t *testing.T) {
		t.Parallel()

		// Create first delegation
		firstStake := big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(2))
		receipt, _, err := staker.AddDelegation(validationIDs[5], firstStake, true, 100).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		firstDelegationID := receiptToID(receipt)

		// Create second delegation (additional stake)
		secondStake := big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(3))
		receipt, _, err = staker.AddDelegation(validationIDs[5], secondStake, true, 100).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		secondDelegationID := receiptToID(receipt)

		// Verify both delegations
		delegation, err := staker.GetDelegation(firstDelegationID)
		require.NoError(t, err)
		assert.Equal(t, firstStake, delegation.Stake)
		assert.True(t, delegation.AutoRenew)

		delegation, err = staker.GetDelegation(secondDelegationID)
		require.NoError(t, err)
		assert.Equal(t, secondStake, delegation.Stake)
		assert.True(t, delegation.AutoRenew)

		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*4))
		totalsBeforeWithdrawal, err := staker.GetValidatorsTotals(validationIDs[5])
		require.NoError(t, err)
		
		// Verify that the validator has the exact total stake from both delegations
		expectedTotalStake := big.NewInt(0).Add(firstStake, secondStake)
		assert.Equal(t, expectedTotalStake, totalsBeforeWithdrawal.DelegationsLockedStake,
			"Validator should have exact total stake from both delegations before withdrawal")

		receipt, _, err = staker.UpdateDelegationAutoRenew(firstDelegationID, false).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))

		// Withdraw only the first delegation (partial decrease)
		receipt, _, err = staker.WithdrawDelegation(firstDelegationID).
			Send().
			WithSigner(hayabusa.Stargate).
			WithOptions(testutil.TxOptions()).
			SubmitAndConfirm(testutil.TxContext(t))
		require.NoError(t, err)
		require.False(t, receipt.Reverted, "Withdrawal should succeed")

		// Wait for withdrawal to be processed and reflected in validator totals
		best, err := staker.Raw().Client().Block("best")
		require.NoError(t, err)
		require.NoError(t, ticker.WaitForBlock(best.Number+config.MinStakingPeriod))

		// Verify that the validator has exactly the second delegation stake after withdrawal
		totalsAfterWithdrawal, err := staker.GetValidatorsTotals(validationIDs[5])
		require.NoError(t, err)
		assert.Equal(t, secondStake, totalsAfterWithdrawal.DelegationsLockedStake,
			"Validator should have exactly the second delegation stake after withdrawal")
		assert.True(t, totalsAfterWithdrawal.DelegationsLockedWeight.Cmp(big.NewInt(0)) > 0,
			"Validator should have positive weight after withdrawal")
	})
}

func newDelegationSetup(t *testing.T) (*builtin.Staker, *hayabusa.Config, [6]thor.Bytes32) {
	t.Helper()
	config := &hayabusa.Config{
		Nodes:             6,
		MaxBlockProposers: 6,
		ForkBlock:         0,
		TransitionPeriod:  4,
		EpochLength:       2,
		CooldownPeriod:    2,
		MinStakingPeriod:  4,
		MidStakingPeriod:  12,
		HighStakingPeriod: 259200,
	}
	client, _, cancel, err := hayabusa.StartNetwork(t, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)

	staker, err := builtin.NewStaker(client)
	if err != nil {
		t.Fatalf("failed to create staker: %v", err)
	}
	if err := utils.WaitForFork(staker, config.ForkBlock); err != nil {
		t.Fatalf("failed to wait for fork: %v", err)
	}

	validationIDs := [6]thor.Bytes32{}
	senders := &utils.Senders{}

	for i := range validationIDs {
		account := hayabusa.ValidatorAccounts[i]
		sender := staker.AddValidator(account.Address(), builtin.MinStake(), config.MinStakingPeriod, true).
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
		validationIDs[i] = event.ValidationID
	}
	return staker, config, validationIDs
}

func receiptToID(receipt *transactions.Receipt) thor.Bytes32 {
	// 0 is the event, 1 is the validation ID
	return receipt.Outputs[0].Events[0].Topics[2]
}
