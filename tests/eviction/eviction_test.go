package eviction

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func TestHayabusaEviction(t *testing.T) {
	config, client, network := testutil.SetupTestNetworkWithEpochAndBlockInterval(t, 3, 2, 2)

	validator2Node := network.NodeConfigs()[1]

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]
	validator4 := hayabusa.ValidatorAccounts[3]

	sequence := testutil.NewTxSequence(t)

	staker := testutil.SetupStakerAndWaitForFork(t, client, config)

	active, queued, err := staker.GetValidatorsNum()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(0).String(), active.String())
	assert.Equal(t, big.NewInt(0).String(), queued.String())

	stake := big.NewInt(1e18)
	stake = new(big.Int).Mul(stake, big.NewInt(1e6))
	stake = new(big.Int).Mul(stake, big.NewInt(26))
	id1 := testutil.AddValidator(sequence, staker, validator1, config.MinStakingPeriod)
	active, queued, err = staker.GetValidatorsNum()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(0).String(), active.String())
	assert.Equal(t, big.NewInt(1).String(), queued.String())

	id2 := testutil.AddValidator(sequence, staker, validator2, config.MinStakingPeriod)
	id3 := testutil.AddValidator(sequence, staker, validator3, config.MinStakingPeriod)
	id4 := testutil.AddValidator(sequence, staker, validator4, config.MinStakingPeriod)

	assert.Equal(t, validator1.Node.Address(), id1)
	assert.Equal(t, validator2.Node.Address(), id2)
	assert.Equal(t, validator3.Node.Address(), id3)
	assert.Equal(t, validator4.Node.Address(), id4)

	active, queued, err = staker.GetValidatorsNum()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(0).String(), active.String())
	assert.Equal(t, big.NewInt(4).String(), queued.String())

	_, validatorID, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Queued validator OK")

	block := config.ForkBlock + config.TransitionPeriod
	require.NoError(t, utils.WaitForPOS(staker, block))

	_, validatorID, err = staker.FirstActive()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Validator is active")

	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id4, builtin.StakerStatusQueued, block)

	active, queued, err = staker.GetValidatorsNum()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(3).String(), active.String())
	assert.Equal(t, big.NewInt(1).String(), queued.String())
	t.Log("✅ - Three validators are activated one is queued")

	require.NoError(t, network.NodeLifecycles()[validator2Node.GetID()].Stop())

	err = utils.WaitForCondition(staker.Raw().Client(), config.ForkBlock+config.TransitionPeriod+config.MinStakingPeriod, func() (bool, error) {
		valStatus, err := staker.GetValidatorStatus(id2)
		if err != nil {
			return false, err
		}
		return !valStatus.Online, nil
	})

	offlineBlock := config.ForkBlock + config.TransitionPeriod
	exitBlock := offlineBlock + config.EpochLength + config.ValidatorEvictionThreshold + 1
	ticker := utils.NewTicker(staker.Raw().Client())
	t.Log("✅ waiting for block", exitBlock)
	require.NoError(t, ticker.WaitForBlock(exitBlock))
	t.Log("✅ waiting done")

	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, exitBlock+10)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusExited, exitBlock+10)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, exitBlock+10)
	assertValidatorStatus(t, staker, id4, builtin.StakerStatusActive, exitBlock+10)

	t.Log("✅ offline validator exited, queued validator accepted in leader group")
}

func assertValidatorStatus(t *testing.T, staker *builtin.Staker, validatorID thor.Address, expectedStatus builtin.StakerStatus, waitForBlock uint32) {
	assert.NoError(t, utils.NewTicker(staker.Raw().Client()).WaitForBlock(waitForBlock))
	validator, err := staker.GetValidatorStatus(validatorID)
	assert.NoError(t, err)
	assert.Equal(t, expectedStatus, validator.Status)
}
