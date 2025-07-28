package slots

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func Test_MissedSlot(t *testing.T) {
	testutil.RunFlakyTest(t, func() error {
		return runTestMissedSlot(t)
	})
}

func runTestMissedSlot(t *testing.T) error {
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
		Name:              t.Name(),
	}
	hayabusa := hayabusa.NewNetwork(config, t.Context())
	client, network, err := hayabusa.Start()
	if err != nil {
		t.Fatal(err)
	}

	staker, err := builtin.NewStaker(client)
	require.NoError(t, err)

	validator1 := network.Config().Nodes[0]
	validator2 := network.Config().Nodes[1]
	validator3 := network.Config().Nodes[2]

	mustAddValidator := func(hexKey string, stake *big.Int) thor.Address {
		key, err := crypto.HexToECDSA(hexKey)
		require.NoError(t, err)
		signer := (*bind.PrivateKeySigner)(key)
		address := thor.Address(crypto.PubkeyToAddress(key.PublicKey))
		receipt := testutil.Send(t, signer, staker.AddValidator(address, stake, config.MinStakingPeriod))
		id := receipt.Outputs[0].Events[0].Topics[2]
		return thor.BytesToAddress(id.Bytes())
	}

	// add 2 min stake validators
	stake := new(big.Int).Set(builtin.MinStake())
	mustAddValidator(validator2.GetKey(), stake)
	mustAddValidator(validator3.GetKey(), stake)

	// x16 stake
	stake = stake.Mul(stake, big.NewInt(16))
	validationID := mustAddValidator(validator1.GetKey(), stake)

	// wait for PoS
	block := config.ForkBlock + config.TransitionPeriod
	ticker := utils.NewTicker(staker.Raw().Client())
	require.NoError(t, ticker.WaitForBlock(block))

	// wait for a missed slot
	prev, err := ticker.Wait(35 * time.Second)
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
	err = ticker.WaitForCondition(time.Minute*1, func() (bool, error) {
		validation, err := staker.Get(validationID)
		require.NoError(t, err)
		t.Logf("⚠️ - waiting for validator %s to be online, status: %v", validationID.String(), validation.Status)
		return validation.Online, nil
	})
	if err != nil {
		return testutil.StakerStatusUnknownError{ValidationID: validationID.String()}
	}

	validation, err = staker.Get(validationID)
	require.NoError(t, err)
	require.True(t, validation.Online)
	t.Log("✅ - validator back online")

	return nil
}
