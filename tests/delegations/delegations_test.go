package delegations

import (
	"math/big"
	"testing"
	"time"

	"github.com/vechain/draupnir/contracts"
	"github.com/vechain/hayabusa-e2e/builtins"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/thor/v2/thor"
)

func Test_AddDelegation(t *testing.T) {
	// Setup
	staker, _, validationIDs := newDelegationSetup(t)

	totalStake := new(big.Int).Mul(builtins.MinStake, big.NewInt(int64(len(validationIDs))))

	for _, validationID := range validationIDs {
		senders := &contracts.Senders{}
		for range 16 {
			sender := staker.Attach(hayabusa.Stargate.PrivateKey).AddDelegation(validationID, builtins.MinStake, true, 200)
			senders.Add(sender)
			totalStake = totalStake.Add(totalStake, builtins.MinStake)
		}
		if _, _, err := senders.Send(false); err != nil {
			t.Fatalf("failed to send delegation transactions: %v", err)
		}
	}

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
