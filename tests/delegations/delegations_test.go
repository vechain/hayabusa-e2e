package delegations

import (
	"math/big"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/draupnir/common"
	"github.com/vechain/draupnir/contracts"
	"github.com/vechain/hayabusa-e2e/builtins"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/thor/v2/api/transactions"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
)

func Test_StargateRewards(t *testing.T) {
	// Setup
	staker, config, validationIDs := newDelegationSetup(t)

	expectedStake := new(big.Int).Mul(builtins.MinStake, big.NewInt(int64(len(validationIDs))))

	for _, validationID := range validationIDs { // evenly distribute delegations among validators
		senders := &contracts.Senders{}
		for range 10 {
			sender := staker.AddDelegation(validationID, builtins.MinStake, true, 200)
			senders.Add(sender)
			expectedStake = expectedStake.Add(expectedStake, builtins.MinStake)
		}
		_, _, err := senders.Send(false)
		require.NoError(t, err)
	}

	ticker := common.NewTicker(staker.Client())
	best, err := staker.Client().Block("best")
	require.NoError(t, err)
	require.NoError(t, ticker.WaitForBlock(best.Number+config.MinStakingPeriod))

	totalStake, totalWeight, err := staker.TotalStake()
	require.NoError(t, err)
	assert.Equal(t, expectedStake, totalStake)
	assert.Equal(t, big.NewInt(0).Mul(expectedStake, big.NewInt(2)), totalWeight)

	best, err = staker.Client().Block("best")
	require.NoError(t, err)

	// block N energy
	acc, err := staker.Client().Account(&hayabusa.Stargate.Address, thorclient.Revision(strconv.Itoa(int(best.Number))))
	require.NoError(t, err)
	blockNEnergy := (big.Int)(acc.Energy)

	assert.NoError(t, ticker.WaitForBlock(best.Number+1))

	// block N+1 energy
	acc, err = staker.Client().Account(&hayabusa.Stargate.Address, thorclient.Revision(strconv.Itoa(int(best.Number+1))))
	require.NoError(t, err)
	blockNPlus1Energy := (big.Int)(acc.Energy)

	// assert plus1 is greater than N
	assert.True(t, blockNPlus1Energy.Cmp(&blockNEnergy) > 0, "block N+1 energy should be greater than block N energy")
}

func Test_Delegations(t *testing.T) {
	staker, config, validationIDs := newDelegationSetup(t)
	ticker := common.NewTicker(staker.Client())

	t.Run("Delegate for 1 period only", func(t *testing.T) {
		t.Parallel()

		// add the delegation
		receipt, _, err := staker.AddDelegation(validationIDs[0], builtins.MinStake, false, 100).Receipt(false)
		require.NoError(t, err)
		delegationID := receiptToID(receipt)
		delegation, err := staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, builtins.MinStake, delegation.Stake)
		assert.Equal(t, uint8(100), delegation.Multiplier)
		assert.False(t, delegation.AutoRenew)

		// wait for validators current period + 1 staking period
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*2))

		// withdraw - should succeed since auto-renew is false
		receipt, _, err = staker.WithdrawDelegation(delegationID).Receipt(false)
		require.NoError(t, err)

		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.True(t, delegation.Stake.Sign() == 0)
	})

	t.Run("Immediate enable auto-renew", func(t *testing.T) {
		t.Parallel()

		// add the delegation
		receipt, _, err := staker.AddDelegation(validationIDs[1], builtins.MinStake, false, 100).Receipt(false)
		require.NoError(t, err)
		delegationID := receiptToID(receipt)
		delegation, err := staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, builtins.MinStake, delegation.Stake)
		assert.Equal(t, uint8(100), delegation.Multiplier)
		assert.False(t, delegation.AutoRenew)

		// immediately enable auto-renew
		receipt, _, err = staker.UpdateDelegationAutoRenew(delegationID, true).Receipt(false)
		require.NoError(t, err)
		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.True(t, delegation.AutoRenew)

		// wait for validators current period + 1 staking period
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*2))

		// withdraw - should fail since auto-renew is true
		receipt, _, err = staker.WithdrawDelegation(delegationID).Receipt(true)
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)
		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, builtins.MinStake, delegation.Stake)
	})

	t.Run("Delegated with auto-renew", func(t *testing.T) {
		t.Parallel()

		// add the delegation
		receipt, _, err := staker.AddDelegation(validationIDs[2], builtins.MinStake, true, 100).Receipt(false)
		require.NoError(t, err)
		delegationID := receiptToID(receipt)
		delegation, err := staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, builtins.MinStake, delegation.Stake)
		assert.Equal(t, uint8(100), delegation.Multiplier)
		assert.True(t, delegation.AutoRenew)

		// wait for validators current period + 1 staking period
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*2))

		// withdraw - should revert due to auto-renew
		receipt, _, err = staker.WithdrawDelegation(delegationID).Receipt(true)
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)
		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.Equal(t, builtins.MinStake, delegation.Stake)

		receipt, _, err = staker.UpdateDelegationAutoRenew(delegationID, false).Receipt(false)
		require.NoError(t, err)

		// wait for validators current period to end
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod))

		// withdraw - should succeed since auto-renew is false
		receipt, _, err = staker.WithdrawDelegation(delegationID).Receipt(false)
		require.NoError(t, err)
		delegation, err = staker.GetDelegation(delegationID)
		require.NoError(t, err)
		assert.True(t, delegation.Stake.Sign() == 0)
	})

	t.Run("Should not be able call with external account", func(t *testing.T) {
		t.Parallel()
		receipt, _, err := staker.AddDelegation(validationIDs[0], builtins.MinStake, false, 100).Receipt(false)
		require.NoError(t, err)
		delegationID := receiptToID(receipt)

		// external should not be able to add delegation
		receipt, _, err = staker.Attach(hayabusa.AdditionalAccounts[0].PrivateKey).AddDelegation(validationIDs[0], builtins.MinStake, false, 100).Receipt(true)
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)

		// external should not be able to toggle auto-renew
		receipt, _, err = staker.Attach(hayabusa.AdditionalAccounts[0].PrivateKey).UpdateDelegationAutoRenew(delegationID, true).Receipt(true)
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)

		// wait for delegation to end
		require.NoError(t, ticker.WaitForBlock(receipt.Meta.BlockNumber+config.MinStakingPeriod*2))

		// external should not be able to withdraw delegation
		receipt, _, err = staker.Attach(hayabusa.AdditionalAccounts[0].PrivateKey).WithdrawDelegation(delegationID).Receipt(true)
		require.NoError(t, err)
		assert.True(t, receipt.Reverted)
	})
}

func newDelegationSetup(t *testing.T) (*builtins.Staker, *hayabusa.Config, [6]thor.Bytes32) {
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

	staker := builtins.NewStaker(client, hayabusa.Stargate.PrivateKey)
	if err := staker.WaitForFork(config.ForkBlock); err != nil {
		t.Fatalf("failed to wait for fork: %v", err)
	}

	validationIDs := [6]thor.Bytes32{}
	senders := &contracts.Senders{}

	for i := range validationIDs {
		account := hayabusa.ValidatorAccounts[i]
		sender := staker.Attach(account.PrivateKey).AddValidator(account.Address, builtins.MinStake, config.MinStakingPeriod, true)
		senders.Add(sender)
	}

	if _, _, err := senders.Send(false); err != nil {
		t.Fatal(err)
	}
	if err := staker.WaitForPOS(config.ForkBlock + config.TransitionPeriod); err != nil {
		t.Fatalf("failed to wait for PoS: %v", err)
	}
	events, err := staker.FilterValidatorQueued(0, 1000)
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
