package slots

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/vechain/networkhub/utils/common"
	"github.com/vechain/thor/v2/thorclient"

	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/thor"
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
	network, err := hayabusa.NewNetwork(config, t.Context())
	require.NoError(t, err)
	t.Cleanup(network.Stop)
	require.NoError(t, network.Start())

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]

	node1Config := network.NodeConfigs()[0]
	node2Config := network.NodeConfigs()[1]
	client := thorclient.New(fmt.Sprintf("http://%s", node2Config.GetAPIAddr()))

	staker, err := builtin.NewStaker(client)
	require.NoError(t, err)

	mustAddValidator := func(nodePair *hayabusa.NodePair, stake *big.Int) thor.Address {
		receipt := testutil.Send(t, nodePair.Endorser, staker.AddValidation(nodePair.Node.Address(), stake, config.MinStakingPeriod))
		id := receipt.Outputs[0].Events[0].Topics[1]
		return thor.BytesToAddress(id.Bytes())
	}

	// add 2 min stake validators
	stake := new(big.Int).Set(builtin.MinStake())
	mustAddValidator(validator2, stake)
	mustAddValidator(validator3, stake)

	// x16 stake
	stake = stake.Mul(stake, big.NewInt(16))
	validationID := mustAddValidator(validator1, stake)

	// wait for PoS
	block := config.ForkBlock + config.TransitionPeriod
	require.NoError(t, utils.WaitForPOS(t.Context(), staker, block))
	ticker := common.NewTicker(staker.Raw().Client())

	// wait for a missed slot
	prev, err := ticker.Wait(35 * time.Second)
	require.NoError(t, err)
	// shut the validator down
	require.NoError(t, network.NodeLifecycles()[node1Config.GetID()].Stop())

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

	validation, err := staker.GetValidation(validationID)
	require.NoError(t, err)
	require.False(t, validation.IsOnline())

	// start the validator again
	require.NoError(t, network.NodeLifecycles()[node1Config.GetID()].Start())

	// wait for the validator to be back online
	err = ticker.WaitForCondition(time.Minute*1, func() (bool, error) {
		validation, err := staker.GetValidation(validationID)
		require.NoError(t, err)
		t.Logf("⚠️ - waiting for validator %s to be online, status: %v", validationID.String(), validation.Status)
		return validation.IsOnline(), nil
	})
	if err != nil {
		return testutil.StakerStatusUnknownError{ValidationID: validationID.String()}
	}

	validation, err = staker.GetValidation(validationID)
	require.NoError(t, err)
	require.True(t, validation.IsOnline())
	t.Log("✅ - validator back online")

	return nil
}
