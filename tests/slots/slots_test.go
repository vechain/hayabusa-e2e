package slots

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"github.com/vechain/draupnir/common"
	"github.com/vechain/hayabusa-e2e/builtins"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/thor/v2/thor"
)

func Test_MissedSlot(t *testing.T) {
	config := &hayabusa.Config{
		Nodes:             3,
		MaxBlockProposers: 3,
		ForkBlock:         0,
		TransitionPeriod:  4,
		EpochLength:       2,
		CooldownPeriod:    2,
		MinStakingPeriod:  4,
		MidStakingPeriod:  12,
		HighStakingPeriod: 259200,
	}
	client, network, cancel, err := hayabusa.StartNetwork(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)
	staker := builtins.NewStaker(client, hayabusa.ValidatorAccounts[0].PrivateKey)

	validator1 := network.Details().NetworkCfg.Nodes[0]
	validator2 := network.Details().NetworkCfg.Nodes[1]
	validator3 := network.Details().NetworkCfg.Nodes[2]

	mustAddValidator := func(hexKey string, stake *big.Int) thor.Bytes32 {
		key, err := crypto.HexToECDSA(hexKey)
		require.NoError(t, err)
		address := thor.Address(crypto.PubkeyToAddress(key.PublicKey))
		receipt, _, err := staker.Attach(key).AddValidator(address, stake, config.MinStakingPeriod, true).Receipt(false)
		require.NoError(t, err)
		return receipt.Outputs[0].Events[0].Topics[3]
	}

	// add 2 min stake validators
	stake := new(big.Int).Set(builtins.MinStake)
	mustAddValidator(validator2.GetKey(), stake)
	mustAddValidator(validator3.GetKey(), stake)

	// x16 stake
	stake = stake.Mul(stake, big.NewInt(16))
	validationID := mustAddValidator(validator1.GetKey(), stake)

	// wait for PoS
	block := config.ForkBlock + config.TransitionPeriod
	ticker := common.NewTicker(staker.Client())
	require.NoError(t, ticker.WaitForBlock(block))

	// wait for a missed slot
	prev, err := ticker.Wait(25 * time.Second)
	require.NoError(t, err)
	// shut the validator down
	require.NoError(t, network.Nodes()[validator1.GetID()].Stop())

	missedSlot := false
	// (16 / 18) ^ 60 = 0.00085% chance of this failing
	for range 60 {
		best, err := ticker.Wait(5 * time.Minute)
		require.NoError(t, err)
		if best.Timestamp-prev.Timestamp > 15 {
			missedSlot = true
			break
		}
		prev = best
	}
	require.True(t, missedSlot, "missed slot not detected")
	t.Log("✅ - missed slot detected")

	validation, err := staker.Get(validationID)
	require.NoError(t, err)
	require.False(t, validation.Online)

	// start the validator again
	require.NoError(t, network.Nodes()[validator1.GetID()].Start())

	// wait for the validator to be back online
	online := false
	for range 20 {
		best, err := ticker.Wait(25 * time.Second)
		require.NoError(t, err)
		if best.Signer == *validation.Master {
			online = true
			break
		}
	}

	validation, err = staker.Get(validationID)
	require.NoError(t, err)
	require.True(t, validation.Online)
	require.True(t, online, "validator not back online")
	t.Log("✅ - validator back online")
}
