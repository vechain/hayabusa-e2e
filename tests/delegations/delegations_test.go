package delegations

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vechain/draupnir/common"
	"github.com/vechain/draupnir/contracts"
	"github.com/vechain/hayabusa-e2e/builtins"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/thor/v2/thor"
)

func Test_AddDelegation(t *testing.T) {
	// Setup
	staker, _, validationIDs := newDelegationSetup(t)

	stake := new(big.Int).Mul(builtins.MinStake, big.NewInt(10))
	_, _, err := staker.AddDelegation(validationIDs[0], stake, true, 200).Receipt(true)
	require.NoError(t, err)

	ticker := common.NewTicker(staker.Client())
	go func() {
		t.Logf("stargate address: %s", hayabusa.Stargate.Address)
		stargateAcc, err := staker.Client().Account(&hayabusa.Stargate.Address)
		require.NoError(t, err)
		balance := (big.Int)(stargateAcc.Energy)
		require.NoError(t, err)
		start := big.NewInt(0).Set(&balance)

		friendly := new(big.Int).Quo(start, big.NewInt(1e18)) // wei to ETH
		t.Logf("stargate initial energy: %s", friendly)

		for {
			// get the energy at the current block
			block, err := ticker.Wait(20 * time.Second)
			require.NoError(t, err)
			stargateAcc, err := staker.Client().Account(&hayabusa.Stargate.Address)
			require.NoError(t, err)
			newBalance := (big.Int)(stargateAcc.Energy)
			require.NoError(t, err)
			current := big.NewInt(0).Set(&newBalance)

			// get the total stake in the contract
			totalLocked, err := staker.TotalStake()
			require.NoError(t, err)
			totalLocked = totalLocked.Div(totalLocked, big.NewInt(1e18)) // wei to ETH
			totalLocked = totalLocked.Div(totalLocked, big.NewInt(1e6))  // divide by 1m

			// diff in VTHO from start
			diff := new(big.Int).Sub(current, start)
			diff = diff.Div(diff, big.NewInt(1e18)) // wei to ETH

			// 1 VTHO = $0.002643
			rate := new(big.Float).SetFloat64(0.002643)
			diffUSD := new(big.Float).Mul(new(big.Float).SetInt(diff), rate)

			usd, _ := diffUSD.Int(big.NewInt(0))

			t.Logf("stargate generated energy @ block %d: VTHO=%s ($%s), staked=%s(millions)", block.Number, diff, usd, totalLocked)
		}
	}()

	time.Sleep(10000 * time.Second)
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
