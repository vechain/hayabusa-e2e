package delegations

import (
	"context"
	"github.com/vechain/hayabusa-e2e/testutil"
	"math/big"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/api/transactions"
	"github.com/vechain/thor/v2/logdb"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func Test_StargateRewards(t *testing.T) {
	// Setup
	staker, config, validationIDs := newDelegationSetup(t)

	expectedStake := new(big.Int).Mul(builtin.MinStake(), big.NewInt(int64(len(validationIDs))))
	stargateAddr := hayabusa.Stargate.Address()

	for _, validationID := range validationIDs { // evenly distribute delegations among validators
		senders := &bind.Senders{}
		for range 10 {
			sender := staker.AddDelegation(hayabusa.Stargate, validationID, builtin.MinStake(), true, 200)
			senders.Add(sender)
			expectedStake = expectedStake.Add(expectedStake, builtin.MinStake())
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _, err := senders.Send(ctx, &bind.TxOptions{})
		cancel()
		require.NoError(t, err)
	}

	ticker := utils.NewTicker(staker.Raw().Client())
	best, err := staker.Raw().Client().GetBlock("best")
	require.NoError(t, err)
	require.NoError(t, ticker.WaitForBlock(best.Number+config.MinStakingPeriod))

	totalStake, totalWeight, err := staker.TotalStake()
	require.NoError(t, err)
	assert.Equal(t, expectedStake, totalStake)
	assert.Equal(t, big.NewInt(0).Mul(expectedStake, big.NewInt(2)), totalWeight)

	best, err = staker.Raw().Client().GetBlock("best")
	require.NoError(t, err)

	// block N energy
	acc, err := staker.Raw().Client().GetAccount(&stargateAddr, strconv.Itoa(int(best.Number)))
	require.NoError(t, err)
	blockNEnergy := (big.Int)(acc.Energy)

	assert.NoError(t, ticker.WaitForBlock(best.Number+1))

	// block N+1 energy
	acc, err = staker.Raw().Client().GetAccount(&stargateAddr, strconv.Itoa(int(best.Number+1)))
	require.NoError(t, err)
	blockNPlus1Energy := (big.Int)(acc.Energy)

	// assert plus1 is greater than N
	assert.True(t, blockNPlus1Energy.Cmp(&blockNEnergy) > 0, "block N+1 energy should be greater than block N energy")
}

func Test_Delegations(t *testing.T) {
	staker, config, validationIDs := newDelegationSetup(t)
	ticker := utils.NewTicker(staker.Raw().Client())

	t.Run("Delegate for 1 period only", func(t *testing.T) {
		t.Parallel()

		// add the delegation
		receipt, _, err := staker.AddDelegation(hayabusa.Stargate, validationIDs[0], builtin.MinStake(), false, 100).Receipt(testutil.TxContext(t), &bind.TxOptions{})
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
		receipt, _, err = staker.WithdrawDelegation(hayabusa.Stargate, delegationID).Receipt(testutil.TxContext(t), &bind.TxOptions{})
		require.NoError(t, err)

		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.True(t, delegation.Stake.Sign() == 0)
	})

	t.Run("Immediate enable auto-renew", func(t *testing.T) {
		t.Parallel()

		// add the delegation
		receipt, _, err := staker.AddDelegation(hayabusa.Stargate, validationIDs[1], builtin.MinStake(), false, 100).Receipt(testutil.TxContext(t), &bind.TxOptions{})
		require.NoError(t, err)
		delegationID := receiptToID(receipt)
		delegation, err := staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, builtin.MinStake(), delegation.Stake)
		assert.Equal(t, uint8(100), delegation.Multiplier)
		assert.False(t, delegation.AutoRenew)

		// immediately enable auto-renew
		receipt, _, err = staker.UpdateDelegationAutoRenew(hayabusa.Stargate, delegationID, true).Receipt(testutil.TxContext(t), &bind.TxOptions{})
		require.NoError(t, err)
		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.True(t, delegation.AutoRenew)

		// wait for validators current period + 1 staking period
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*2))

		// withdraw - should fail since auto-renew is true
		receipt, _, err = staker.WithdrawDelegation(hayabusa.Stargate, delegationID).Receipt(testutil.TxContext(t), &bind.TxOptions{})
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)
		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, builtin.MinStake(), delegation.Stake)
	})

	t.Run("Delegated with auto-renew", func(t *testing.T) {
		t.Parallel()

		// add the delegation
		receipt, _, err := staker.AddDelegation(hayabusa.Stargate, validationIDs[2], builtin.MinStake(), true, 100).Receipt(testutil.TxContext(t), &bind.TxOptions{})
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
		receipt, _, err = staker.WithdrawDelegation(hayabusa.Stargate, delegationID).Receipt(testutil.TxContext(t), &bind.TxOptions{})
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)
		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, builtin.MinStake(), delegation.Stake)

		receipt, _, err = staker.UpdateDelegationAutoRenew(hayabusa.Stargate, delegationID, false).Receipt(testutil.TxContext(t), &bind.TxOptions{})
		require.NoError(t, err)

		// wait for validators current period to end
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))

		// withdraw - should succeed since auto-renew is false
		receipt, _, err = staker.WithdrawDelegation(hayabusa.Stargate, delegationID).Receipt(testutil.TxContext(t), &bind.TxOptions{})
		require.NoError(t, err)
		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.True(t, delegation.Stake.Sign() == 0)
	})

	t.Run("Should not be able call with external account", func(t *testing.T) {
		t.Parallel()
		receipt, _, err := staker.AddDelegation(hayabusa.Stargate, validationIDs[0], builtin.MinStake(), false, 100).Receipt(testutil.TxContext(t), &bind.TxOptions{})
		require.NoError(t, err)
		delegationID := receiptToID(receipt)

		// external should not be able to add delegation
		receipt, _, err = staker.AddDelegation(hayabusa.AdditionalAccounts[0], validationIDs[0], builtin.MinStake(), false, 100).Receipt(testutil.TxContext(t), &bind.TxOptions{})
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)

		// external should not be able to toggle auto-renew
		receipt, _, err = staker.UpdateDelegationAutoRenew(hayabusa.AdditionalAccounts[0], delegationID, true).Receipt(testutil.TxContext(t), &bind.TxOptions{})
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)

		// wait for delegation to end
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*2))

		// external should not be able to withdraw delegation
		receipt, _, err = staker.WithdrawDelegation(hayabusa.AdditionalAccounts[0], delegationID).Receipt(testutil.TxContext(t), &bind.TxOptions{})
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)
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
	client, _, cancel, err := hayabusa.StartNetwork(config)
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
	senders := &bind.Senders{}

	for i := range validationIDs {
		account := hayabusa.ValidatorAccounts[i]
		sender := staker.AddValidator(account, account.Address(), builtin.MinStake(), config.MinStakingPeriod, true)
		senders.Add(sender)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	if _, _, err := senders.Send(ctx, &bind.TxOptions{}); err != nil {
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
