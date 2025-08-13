package validations

import (
	"log/slog"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func TestHayabusaAddNonPoAValidator(t *testing.T) {
	t.Parallel()
	testutil.RunFlakyTest(t, func() error {
		return runTestHayabusaAddNonPoAValidator(t)
	})
}

func runTestHayabusaAddNonPoAValidator(t *testing.T) error {
	config, client := setupTestNetwork(t, 3)

	validator1NonPoA := hayabusa.AdditionalAccounts[0]
	validator1PoA := hayabusa.ValidatorAccounts[0]
	validator2PoA := hayabusa.ValidatorAccounts[1]

	staker := setupStakerAndWaitForFork(t, client, config)
	sequence := testutil.NewTxSequence(t)

	stake := calculateValidatorStake()
	firstStake := new(big.Int).Mul(stake, big.NewInt(2))

	receipt, _, err := staker.AddValidation(validator1NonPoA.Address(), firstStake, config.MinStakingPeriod).
		Send().
		WithOptions(testutil.TxOptions()).
		WithSigner(validator1NonPoA).
		SubmitAndConfirm(testutil.TxContext(t))
	assert.NoError(t, err)
	assert.True(t, receipt.Reverted)
	t.Log("✅ - Not a PoA candidate refused to join")

	id1 := addValidator(sequence, staker, validator1PoA, config.MinStakingPeriod)

	firstQueued, _, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, firstQueued.Endorsor, validator1PoA.Node.Address())
	t.Log("✅ - Queued validator OK", "id", id1.String())

	id2 := addValidator(sequence, staker, validator2PoA, config.MinStakingPeriod)

	assertValidatorStatus(t, staker, id2, builtin.StakerStatusQueued, config.ForkBlock)

	t.Log("✅ - Queued validator OK", "id", id2.String())

	block := config.ForkBlock + config.TransitionPeriod
	if err := assertValidatorStatusUnknown(t, staker, id1, builtin.StakerStatusActive, block); err != nil {
		return err
	}
	if err := assertValidatorStatusUnknown(t, staker, id2, builtin.StakerStatusActive, block); err != nil {
		return err
	}

	validator := hayabusa.MustCreateNodePair(hayabusa.AdditionalAccounts[10])
	id3 := addValidator(sequence, staker, validator, config.MinStakingPeriod)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusQueued, block)
	t.Log("✅ - Not a PoA candidate joined")

	t.Log("✅ - All 3 validators joined")

	return nil
}

func TestHayabusaNoForkThenJoinLater(t *testing.T) {
	t.Parallel()
	config, client := setupTestNetwork(t, 3)

	sequence := testutil.NewTxSequence(t)
	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]

	staker := setupStakerAndWaitForFork(t, client, config)

	id1 := addValidator(sequence, staker, validator1, config.MinStakingPeriod)
	assertMatchingValidators(t, staker, id1, validator1.Node.Address())

	firstQueued, _, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, firstQueued.Endorsor, validator1.Node.Address())
	t.Log("✅ - Queued validator OK")

	block := config.ForkBlock + config.TransitionPeriod
	ticker := utils.NewTicker(client)
	require.NoError(t, ticker.WaitForBlock(block))

	_, validatorID, err := staker.FirstActive()
	assert.ErrorContains(t, err, "no active validator")
	assert.Equal(t, thor.Address{}, validatorID)
	t.Log("✅ - Validator is not activated since min validator threshold is not met")

	id2 := addValidator(sequence, staker, validator2, config.MinStakingPeriod)
	assertMatchingValidators(t, staker, id2, validator2.Node.Address())

	block += config.TransitionPeriod
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	t.Log("✅ - Both validators are activated")

	testutil.Send(t, validator1.Endorser, staker.SignalExit(id1))

	block += config.MinStakingPeriod
	id3 := addValidator(sequence, staker, validator3, config.MinStakingPeriod)
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)

	t.Log("✅ - All three validators are activated")
}

func TestHayabusaFullFlowJoinQueuedCooldownExit(t *testing.T) {
	t.Parallel()
	config, client := setupTestNetwork(t, 3)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]
	sequence := testutil.NewTxSequence(t)

	staker := setupStakerAndWaitForFork(t, client, config)
	ticker := utils.NewTicker(client)

	id1 := addValidator(sequence, staker, validator1, config.MinStakingPeriod)
	id2 := addValidator(sequence, staker, validator2, config.MinStakingPeriod)
	id3 := addValidator(sequence, staker, validator3, config.MinStakingPeriod)

	assertMatchingValidators(t, staker, id1, validator1.Node.Address())
	assertMatchingValidators(t, staker, id2, validator2.Node.Address())
	assertMatchingValidators(t, staker, id3, validator3.Node.Address())

	_, validatorID, err := staker.FirstQueued()
	assert.NoError(t, err)

	assert.Equal(t, id1.String(), validatorID.String())
	t.Log("✅ - Queued validator OK")

	block := config.ForkBlock + config.TransitionPeriod
	require.NoError(t, ticker.WaitForBlock(block))

	_, validatorID, err = staker.FirstActive()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Active validator OK")

	// assert validators are active
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)

	testutil.Send(t, validator1.Endorser, staker.SignalExit(id1))
	testutil.Send(t, validator2.Endorser, staker.SignalExit(id2))

	retrievedValidator2, retrievedValidator2Id, err := staker.Next(id1)
	assert.NoError(t, err)
	assert.Equal(t, id2, retrievedValidator2Id)
	assert.Equal(t, validator2.Node.Address().String(), retrievedValidator2.Endorsor.String())
	assert.Equal(t, validator2.Node.Address().String(), retrievedValidator2.Address.String())

	retrievedValidator3, retrievedValidator3Id, err := staker.Next(id2)
	assert.NoError(t, err)
	assert.Equal(t, id3, retrievedValidator3Id)
	assert.Equal(t, validator3.Node.Address().String(), retrievedValidator3.Endorsor.String())
	assert.Equal(t, validator3.Node.Address().String(), retrievedValidator3.Address.String())

	retrievedValidator4, retrievedValidator4Id, err := staker.Next(id3)
	assert.Error(t, err, "no next validator")
	assert.Nil(t, retrievedValidator4)
	assert.Equal(t, thor.Address{}.String(), retrievedValidator4Id.String())

	// assert validators staking periods
	assertValidatorStakingPeriod(t, staker, id1, config.MinStakingPeriod)
	assertValidatorStakingPeriod(t, staker, id2, config.MinStakingPeriod)
	assertValidatorStakingPeriod(t, staker, id3, config.MinStakingPeriod)

	t.Log("✅ - All three validators are activated")

	// assert validators are on cooldown
	block += config.MinStakingPeriod
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)
	assertTotalStakeAndWeight(t, staker, 2)

	t.Log("✅ - Non-AutoRenew validators are on cooldown")

	// assert 1 validator has exited
	block += config.EpochLength
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)
	assertTotalStakeAndWeight(t, staker, 1)

	t.Log("✅ - One validator has exited")

	// assert 1 validator remains
	block += config.EpochLength
	require.NoError(t, ticker.WaitForBlock(block))
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)
	assertTotalStakeAndWeight(t, staker, 1)

	t.Log("✅ - Second validator exited")

	validatorWithdraw(t, staker, validator1.Endorser, id1)
}

func TestHayabusaQueuedAndThenEnter(t *testing.T) {
	t.Parallel()
	config, client := setupTestNetwork(t, 3)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]
	validator4 := hayabusa.ValidatorAccounts[3]
	validator5 := hayabusa.ValidatorAccounts[4]

	sequence := testutil.NewTxSequence(t)

	staker := setupStakerAndWaitForFork(t, client, config)

	active, queued, err := staker.GetValidatorsNum()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(0).String(), active.String())
	assert.Equal(t, big.NewInt(0).String(), queued.String())

	stake := big.NewInt(1e18)
	stake = new(big.Int).Mul(stake, big.NewInt(1e6))
	stake = new(big.Int).Mul(stake, big.NewInt(26))
	id1 := addValidator(sequence, staker, validator1, config.MinStakingPeriod)
	active, queued, err = staker.GetValidatorsNum()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(0).String(), active.String())
	assert.Equal(t, big.NewInt(1).String(), queued.String())

	id2 := addValidator(sequence, staker, validator2, config.MinStakingPeriod)
	id3 := addValidator(sequence, staker, validator3, config.MinStakingPeriod)
	id4 := addValidator(sequence, staker, validator4, config.MinStakingPeriod)

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

	assertTotalStakeAndWeight(t, staker, 3)
	assertQueuedStakeAndWeight(t, staker, 1)

	id5 := addValidatorWithStake(sequence, staker, validator5, stake, config.MinStakingPeriod)
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id4, builtin.StakerStatusQueued, block)
	assertValidatorStatus(t, staker, id5, builtin.StakerStatusQueued, block)

	active, queued, err = staker.GetValidatorsNum()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(3).String(), active.String())
	assert.Equal(t, big.NewInt(2).String(), queued.String())

	assertTotalStakeAndWeight(t, staker, 3)

	queued, queuedWeight, err := staker.QueuedStake()
	assert.NoError(t, err)

	queuedStk := new(big.Int).Add(calculateValidatorStake(), stake)
	assert.Equal(t, queuedStk, queued)
	assert.Equal(t, new(big.Int).Mul(queuedStk, big.NewInt(2)), queuedWeight)

	_, validatorID, err = staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id4, validatorID)
	t.Log("✅ - Three validators are activated, 2 are queued, queue order has changed based on weight")

	receipt := testutil.Send(t, validator3.Endorser, staker.SignalExit(id3))
	assert.Equal(t, staker.Raw().Address().String(), receipt.Outputs[0].Events[0].Address.String())
	assert.Equal(t, validator3.Node.Address().Bytes(), receipt.Outputs[0].Events[0].Topics[1].Bytes()[12:])
	addr := receipt.Outputs[0].Events[0].Topics[1]
	assert.Equal(t, id3, thor.BytesToAddress(addr.Bytes()))

	t.Log("✅ - AutoRenew updated")

	block += config.MinStakingPeriod
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id4, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id5, builtin.StakerStatusQueued, block)

	active, queued, err = staker.GetValidatorsNum()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(3).String(), active.String())
	assert.Equal(t, big.NewInt(1).String(), queued.String())

	_, validationID, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id5, validationID)

	t.Log("✅ - Three validators are activated, 2 are queued, queue order has changed based on weight")

	block += config.MinStakingPeriod
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id4, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id5, builtin.StakerStatusQueued, block)

	active, queued, err = staker.GetValidatorsNum()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(3).String(), active.String())
	assert.Equal(t, big.NewInt(1).String(), queued.String())

	t.Log("✅ - Three validators are active one is queued and one has exited")
}

func TestHayabusaValidatorStakeChanges(t *testing.T) {
	t.Parallel()
	config, client := setupTestNetwork(t, 3)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]
	validator4 := hayabusa.ValidatorAccounts[3]
	sequence := testutil.NewTxSequence(t)

	staker := setupStakerAndWaitForFork(t, client, config)

	id1 := addValidator(sequence, staker, validator1, config.MinStakingPeriod)
	id2 := addValidator(sequence, staker, validator2, config.MinStakingPeriod)
	id3 := addValidator(sequence, staker, validator3, config.MinStakingPeriod)
	id4 := addValidator(sequence, staker, validator4, config.MinStakingPeriod)

	_, validatorID, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Queued validator OK")

	block := waitForPoSAndAssertFirstActive(t, staker, config, id1)

	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id4, builtin.StakerStatusQueued, block)
	t.Log("✅ - Three validators are activated one is queued")

	assertTotalStakeAndWeight(t, staker, 3)
	assertQueuedStakeAndWeight(t, staker, 1)

	// validator 1 increases the stake
	increase := big.NewInt(1e18)
	increase = big.NewInt(0).Mul(increase, big.NewInt(1e6))
	increase = big.NewInt(0).Mul(increase, big.NewInt(5))
	receipt := testutil.Send(t, validator1.Endorser, staker.IncreaseStake(id1, increase))
	assert.Equal(t, staker.Raw().Address().String(), receipt.Outputs[0].Events[0].Address.String())
	assert.Equal(t, validator1.Node.Address().Bytes(), receipt.Outputs[0].Events[0].Topics[1].Bytes()[12:])
	id := receipt.Outputs[0].Events[0].Topics[1]
	assert.Equal(t, id1, thor.BytesToAddress(id.Bytes()))

	t.Log("✅ - Validator 1 stake increased tx sent")

	// Total stake and weight should not have changed
	validatorStake := calculateValidatorStake()
	total, totalWeight, err := staker.TotalStake()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(0).Mul(validatorStake, big.NewInt(3)), total)
	assert.Equal(t, big.NewInt(0).Mul(validatorStake, big.NewInt(6)), totalWeight)
	queued, queuedWeight, err := staker.QueuedStake()
	assert.NoError(t, err)
	// the pending vet increases the queued stake
	assert.Equal(t, big.NewInt(0).Add(validatorStake, increase), queued)
	assert.Equal(t, big.NewInt(0).Mul(big.NewInt(0).Add(validatorStake, increase), big.NewInt(2)), queuedWeight)

	block += config.MinStakingPeriod
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id4, builtin.StakerStatusQueued, block)

	total, totalWeight, err = staker.TotalStake()
	assert.NoError(t, err)
	expectedTotal := big.NewInt(0).Mul(validatorStake, big.NewInt(3))
	expectedTotalWeight := big.NewInt(0).Mul(validatorStake, big.NewInt(6))
	increaseWeight := big.NewInt(0).Mul(increase, big.NewInt(2))
	assert.Equal(t, expectedTotal.Add(expectedTotal, increase), total)
	assert.Equal(t, expectedTotalWeight.Add(expectedTotalWeight, increaseWeight), totalWeight)
	assertQueuedStakeAndWeight(t, staker, 1)

	t.Log("✅ - Validator 1 stake increased")

	// validator 1 increases the stake
	decrease := big.NewInt(1e18)
	decrease = big.NewInt(0).Mul(decrease, big.NewInt(1e6))
	decrease = big.NewInt(0).Mul(decrease, big.NewInt(3))
	testutil.Send(t, validator1.Endorser, staker.DecreaseStake(id1, decrease))
	assert.Equal(t, staker.Raw().Address().String(), receipt.Outputs[0].Events[0].Address.String())
	assert.Equal(t, validator1.Node.Address().Bytes(), receipt.Outputs[0].Events[0].Topics[1].Bytes()[12:])
	address := receipt.Outputs[0].Events[0].Topics[1]
	assert.Equal(t, id1, thor.BytesToAddress(address.Bytes()))

	t.Log("✅ - Validator 1 stake decrease tx sent")

	// Total stake and weight should not have changed
	validatorStake = calculateValidatorStake()
	total, totalWeight, err = staker.TotalStake()
	assert.NoError(t, err)
	threeStake := big.NewInt(0).Mul(validatorStake, big.NewInt(3))
	assert.Equal(t, big.NewInt(0).Add(threeStake, increase), total)
	assert.Equal(t, big.NewInt(0).Mul(big.NewInt(0).Add(threeStake, increase), big.NewInt(2)), totalWeight)
	queued, queuedWeight, err = staker.QueuedStake()
	assert.NoError(t, err)
	// the queued stake should not have changed
	assert.Equal(t, validatorStake, queued)
	assert.Equal(t, big.NewInt(0).Mul(validatorStake, big.NewInt(2)), queuedWeight)

	t.Log("✅ - Validator 1 stake decreased")
	block += config.MinStakingPeriod
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id4, builtin.StakerStatusQueued, block)

	total, totalWeight, err = staker.TotalStake()
	assert.NoError(t, err)
	expectedTotal = big.NewInt(0).Mul(validatorStake, big.NewInt(3))
	expectedTotal = big.NewInt(0).Add(expectedTotal, increase)
	expectedTotal = big.NewInt(0).Sub(expectedTotal, decrease)
	expectedTotalWeight = big.NewInt(0).Mul(validatorStake, big.NewInt(6))
	expectedTotalWeight = big.NewInt(0).Add(expectedTotalWeight, big.NewInt(0).Mul(increase, big.NewInt(2)))
	expectedTotalWeight = big.NewInt(0).Sub(expectedTotalWeight, big.NewInt(0).Mul(decrease, big.NewInt(2)))
	assert.Equal(t, expectedTotal, total)
	assert.Equal(t, expectedTotalWeight, totalWeight)
	assertQueuedStakeAndWeight(t, staker, 1)

	queued, queuedWeight, err = staker.QueuedStake()
	assert.NoError(t, err)
	// the queued stake should not have changed
	assert.Equal(t, validatorStake, queued)
	assert.Equal(t, big.NewInt(0).Mul(validatorStake, big.NewInt(2)), queuedWeight)

	validatorWithdraw(t, staker, validator1.Endorser, id1)

	t.Log("✅ - Validator 1 stake decreased")
}

func TestHayabusaQueuedWeightDecreasedWhenValidatorExits(t *testing.T) {
	t.Parallel()
	config, client := setupTestNetwork(t, 2)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]
	sequence := testutil.NewTxSequence(t)

	staker := setupStakerAndWaitForFork(t, client, config)

	id1 := addValidator(sequence, staker, validator1, config.MinStakingPeriod)
	id2 := addValidator(sequence, staker, validator2, config.MinStakingPeriod)
	id3 := addValidator(sequence, staker, validator3, config.MinStakingPeriod)

	_, validatorID, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Queued validators OK")

	block := waitForPoSAndAssertFirstActive(t, staker, config, id1)

	testutil.Send(t, validator1.Endorser, staker.SignalExit(id1))
	testutil.Send(t, validator2.Endorser, staker.SignalExit(id2))

	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusQueued, block)

	block += config.MinStakingPeriod
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)

	testutil.Send(t, validator3.Endorser, staker.SignalExit(id3))

	queued, queuedWeight, err := staker.QueuedStake()
	assert.NoError(t, err)
	assert.True(t, queued.Cmp(new(big.Int)) == 0)
	assert.True(t, queuedWeight.Cmp(new(big.Int)) == 0)
	t.Log("✅ - Queued stake is decreased for the staked amount, queued weight is decreased for the 2x value of staked amount")

	block += config.EpochLength
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)

	queued, queuedWeight, err = staker.QueuedStake()
	assert.NoError(t, err)
	assert.True(t, queued.Cmp(new(big.Int)) == 0)
	assert.True(t, queuedWeight.Cmp(new(big.Int)) == 0)
	t.Log("✅ - All non-autoRenew validators have exited, queue is empty")
}

func TestHayabusaQueuedWeightDecreasedWhenValidatorSelectedForLeaderGroup(t *testing.T) {
	t.Parallel()
	config, client := setupTestNetwork(t, 3)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	sequence := testutil.NewTxSequence(t)

	staker := setupStakerAndWaitForFork(t, client, config)

	id1 := addValidator(sequence, staker, validator1, config.MinStakingPeriod)
	id2 := addValidator(sequence, staker, validator2, config.MinStakingPeriod)

	_, validatorID, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Queued validators OK")

	block := waitForPoSAndAssertFirstActive(t, staker, config, id1)

	validator3 := hayabusa.ValidatorAccounts[2]
	id3 := addValidator(sequence, staker, validator3, config.MinStakingPeriod)

	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusQueued, block)
	t.Log("✅ - Initial state verified: 2 active, 1 queued")

	initialQueued, initialQueuedWeight, err := staker.QueuedStake()
	assert.NoError(t, err)
	initialTotal, initialTotalWeight, err := staker.TotalStake()
	assert.NoError(t, err)

	validatorStake := calculateValidatorStake()
	expectedInitialQueued := validatorStake
	expectedInitialQueuedWeight := new(big.Int).Mul(validatorStake, big.NewInt(2))
	expectedInitialTotal := new(big.Int).Mul(validatorStake, big.NewInt(2))
	expectedInitialTotalWeight := new(big.Int).Mul(validatorStake, big.NewInt(4))

	assert.Equal(t, expectedInitialQueued, initialQueued)
	assert.Equal(t, expectedInitialQueuedWeight, initialQueuedWeight)
	assert.Equal(t, expectedInitialTotal, initialTotal)
	assert.Equal(t, expectedInitialTotalWeight, initialTotalWeight)

	block += config.MinStakingPeriod
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)
	t.Log("✅ - Validator is removed from the queue by being selected in the leader group")

	finalQueued, finalQueuedWeight, err := staker.QueuedStake()
	assert.NoError(t, err)
	assert.True(t, big.NewInt(0).Cmp(finalQueued) == 0)
	t.Log("✅ - Queued stake is decreased for the staked amount")
	assert.True(t, big.NewInt(0).Cmp(finalQueuedWeight) == 0)
	t.Log("✅ - Queued weight is decreased for the 2x value of staked amount")

	finalTotal, finalTotalWeight, err := staker.TotalStake()
	assert.NoError(t, err)
	expectedFinalTotal := new(big.Int).Mul(validatorStake, big.NewInt(3))
	expectedFinalTotalWeight := new(big.Int).Mul(validatorStake, big.NewInt(6))
	assert.Equal(t, expectedFinalTotal, finalTotal)
	t.Log("✅ - Total stake is increased for the value of stake")
	assert.Equal(t, expectedFinalTotalWeight, finalTotalWeight)
	t.Log("✅ - Total weight is increased for the 2x value of staked amount")
}

func TestHayabusaQueuedStakeAndWeightChangesWhenDelegator(t *testing.T) {
	t.Parallel()
	config, client := setupTestNetwork(t, 1)

	staker := setupStakerAndWaitForFork(t, client, config)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	sequence := testutil.NewTxSequence(t)

	id1 := addValidator(sequence, staker, validator1, config.MinStakingPeriod)
	id2 := addValidator(sequence, staker, validator2, config.MinStakingPeriod)

	_, validatorID, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Queued validators OK")

	block := waitForPoSAndAssertFirstActive(t, staker, config, id1)

	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusQueued, block)

	initialQueued, initialQueuedWeight, err := staker.QueuedStake()
	assert.NoError(t, err)
	expectedInitialQueued := calculateValidatorStake()
	expectedInitialQueuedWeight := new(big.Int).Mul(expectedInitialQueued, big.NewInt(2))
	assert.Equal(t, expectedInitialQueued, initialQueued)
	assert.Equal(t, expectedInitialQueuedWeight, initialQueuedWeight)
	t.Log("✅ - Initial queued stake and weight verified")

	delegatorStake := new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1e6))
	delegatorStake = new(big.Int).Mul(delegatorStake, big.NewInt(10))

	// Add delegator
	receipt := testutil.Send(t, hayabusa.Stargate, staker.AddDelegation(id2, delegatorStake, 100))
	t.Log("✅ - New delegator added to queued validator")

	// Get delegation ID from receipt
	delegationID := new(big.Int).SetBytes(receipt.Outputs[0].Events[0].Topics[2].Bytes())

	finalQueued, finalQueuedWeight, err := staker.QueuedStake()

	assert.NoError(t, err)
	expectedFinalQueued := new(big.Int).Add(expectedInitialQueued, delegatorStake)
	// The multiplier formula divides by 100 so the weight is just the stake
	expectedFinalQueuedWeight := new(big.Int).Add(initialQueuedWeight, delegatorStake)
	assert.Equal(t, expectedFinalQueued, finalQueued)
	t.Log("✅ - Queued stake is increased for the staked amount")
	assert.Equal(t, expectedFinalQueuedWeight, finalQueuedWeight)
	t.Log("✅ - Queued weight is increased for value of delegators stake")

	// Remove delegator
	_ = testutil.Send(t, hayabusa.Stargate, staker.WithdrawDelegation(delegationID))
	delegation, err := staker.GetDelegationStake(delegationID)
	assert.NoError(t, err)
	assert.True(t, delegation.Stake.Sign() == 0)
	t.Log("✅ - Delegator removed from queued validator")

	afterRemovalQueued, afterRemovalQueuedWeight, err := staker.QueuedStake()
	assert.NoError(t, err)
	assert.Equal(t, initialQueued, afterRemovalQueued)
	t.Log("✅ - Queued stake is decreased after delegator removal")
	assert.Equal(t, initialQueuedWeight, afterRemovalQueuedWeight)
	t.Log("✅ - Queued weight is decreased after delegator removal")
}

func TestHayabusaTotalStakeDecreased(t *testing.T) {
	t.Parallel()
	testutil.RunFlakyTest(t, func() error {
		return runTestHayabusaTotalStakeDecreased(t)
	})
}

func runTestHayabusaTotalStakeDecreased(t *testing.T) error {
	config, client := setupTestNetwork(t, 3)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	sequence := testutil.NewTxSequence(t)

	staker := setupStakerAndWaitForFork(t, client, config)

	stake := big.NewInt(1e18)
	stake.Mul(stake, big.NewInt(1e6))
	stake.Mul(stake, big.NewInt(26))
	id1 := addValidator(sequence, staker, validator1, config.MinStakingPeriod)
	id2 := addValidator(sequence, staker, validator2, config.MinStakingPeriod)

	_, validatorID, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Queued validator OK")

	block := waitForPoSAndAssertFirstActive(t, staker, config, id1)

	testutil.Send(t, validator1.Endorser, staker.SignalExit(id1))

	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)

	assertTotalStakeAndWeight(t, staker, 2)

	block += config.MinStakingPeriod
	if err := assertValidatorStatusUnknown(t, staker, id1, builtin.StakerStatusExited, block); err != nil {
		return err
	}
	if err := assertValidatorStatusUnknown(t, staker, id2, builtin.StakerStatusActive, block); err != nil {
		return err
	}

	assertTotalStakeAndWeight(t, staker, 1)

	return nil
}

func TestBeneficiary(t *testing.T) {
	config, client := setupTestNetwork(t, 2)

	staker := setupStakerAndWaitForFork(t, client, config)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	beneficiary := datagen.RandAddress()

	sequence := testutil.NewTxSequence(t)
	id1 := addValidator(sequence, staker, validator1, config.MinStakingPeriod)
	id2 := addValidator(sequence, staker, validator2, config.MinStakingPeriod)
	sequence.Send(validator1.Endorser, staker.SetBeneficiary(id1, beneficiary))
	sequence.Send(validator2.Endorser, staker.SetBeneficiary(id2, beneficiary))

	assert.NoError(t, utils.WaitForPOS(staker, config.ForkBlock+config.TransitionPeriod))

	blockRewards := hayabusa.GetExpectedReward(big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(2)))
	ticker := utils.NewTicker(client)
	block, err := ticker.Wait(time.Second * 20)
	require.NoError(t, err)

	assert.Equal(t, block.Beneficiary, beneficiary, "Block beneficiary should match the set beneficiary")

	beneficiaryBeforeBlock, err := staker.Raw().Client().Account(&beneficiary, thorclient.Revision(block.ParentID.String()))
	require.NoError(t, err)
	beneficiaryAfterBlock, err := staker.Raw().Client().Account(&beneficiary, thorclient.Revision(block.ID.String()))
	require.NoError(t, err)

	difference := new(big.Int).Sub((*big.Int)(beneficiaryAfterBlock.Energy), (*big.Int)(beneficiaryBeforeBlock.Energy))
	assert.Equal(t, blockRewards, difference, "Beneficiary should receive the block rewards")
}

func addValidatorWithStake(seq *testutil.TxSequence, staker *builtin.Staker, nodePair *hayabusa.NodePair, stake *big.Int, period uint32) thor.Address {
	receipt := seq.Send(nodePair.Endorser, staker.AddValidation(nodePair.Node.Address(), stake, period))
	id := receipt.Outputs[0].Events[0].Topics[1]
	amount := big.NewInt(0).Quo(stake, big.NewInt(1e18))
	slog.Info("✅ - added validator", "validator", nodePair.Node.Address().String(), "period", period, "stake", amount, "id", id.String())

	return thor.BytesToAddress(id.Bytes())
}

func addValidator(seq *testutil.TxSequence, staker *builtin.Staker, nodePair *hayabusa.NodePair, period uint32) thor.Address {
	return addValidatorWithStake(seq, staker, nodePair, calculateValidatorStake(), period)
}

func validatorWithdraw(t *testing.T, staker *builtin.Staker, signer bind.Signer, validatorID thor.Address) {
	receipt := testutil.Send(t, signer, staker.WithdrawStake(validatorID))
	addr := signer.Address()
	assert.Equal(t, addr.Bytes(), receipt.Outputs[0].Events[0].Topics[1].Bytes()[12:])
	assert.Len(t, receipt.Outputs[0].Transfers, 1)
	assert.Equal(t, receipt.Outputs[0].Transfers[0].Recipient, addr)
	slog.Info("✅ - validator withdrawn", "validator", validatorID.String())
}

func assertMatchingValidators(t *testing.T, staker *builtin.Staker, id1 thor.Address, masterAddress thor.Address) {
	val1, err := staker.GetValidatorStake(id1)
	assert.NoError(t, err)

	val2, err := staker.GetValidatorStake(masterAddress)
	assert.NoError(t, err)
	assert.Equal(t, val1, val2)
}

func assertValidatorStatus(t *testing.T, staker *builtin.Staker, validatorID thor.Address, expectedStatus builtin.StakerStatus, waitForBlock uint32) {
	assert.NoError(t, utils.NewTicker(staker.Raw().Client()).WaitForBlock(waitForBlock))
	validator, err := staker.GetValidatorStatus(validatorID)
	assert.NoError(t, err)
	assert.Equal(t, expectedStatus, validator.Status)
}

func assertValidatorStatusUnknown(t *testing.T, staker *builtin.Staker, validatorID thor.Address, expectedStatus builtin.StakerStatus, waitForBlock uint32) error {
	assert.NoError(t, utils.NewTicker(staker.Raw().Client()).WaitForBlock(waitForBlock))
	validator, err := staker.GetValidatorStatus(validatorID)
	assert.NoError(t, err)
	if validator.Status == builtin.StakerStatusUnknown {
		return testutil.StakerStatusUnknownError{ValidationID: validatorID.String()}
	}
	assert.Equal(t, expectedStatus, validator.Status)
	return nil
}

func assertValidatorStakingPeriod(t *testing.T, staker *builtin.Staker, validatorID thor.Address, expectedPeriod uint32) {
	validator, err := staker.GetValidatorPeriodDetails(validatorID)
	assert.NoError(t, err)
	assert.Equal(t, expectedPeriod, validator.Period)
}

func setupTestNetwork(t *testing.T, maxBlockProposers uint32) (*hayabusa.Config, *thorclient.Client) {
	config := &hayabusa.Config{
		Nodes:             6,
		MaxBlockProposers: maxBlockProposers,
		ForkBlock:         0,
		TransitionPeriod:  10,
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
	return config, network.ThorClient()
}

func setupStakerAndWaitForFork(t *testing.T, client *thorclient.Client, config *hayabusa.Config) *builtin.Staker {
	staker, err := builtin.NewStaker(client)
	require.NoError(t, err)
	require.NoError(t, utils.WaitForFork(staker, config.ForkBlock))
	return staker
}

func calculateValidatorStake() *big.Int {
	stake := big.NewInt(1e18)
	stake = new(big.Int).Mul(stake, big.NewInt(1e6))
	stake = new(big.Int).Mul(stake, big.NewInt(25))
	return stake
}

func assertQueuedStakeAndWeight(t *testing.T, staker *builtin.Staker, expectedQueuedCount int) {
	validatorStake := calculateValidatorStake()
	queued, queuedWeight, err := staker.QueuedStake()
	assert.NoError(t, err)
	expectedQueuedStake := new(big.Int).Mul(validatorStake, big.NewInt(int64(expectedQueuedCount)))
	expectedQueuedWeight := new(big.Int).Mul(validatorStake, big.NewInt(int64(expectedQueuedCount*2)))
	assert.Equal(t, expectedQueuedStake, queued)
	assert.Equal(t, expectedQueuedWeight, queuedWeight)
}

func assertTotalStakeAndWeight(t *testing.T, staker *builtin.Staker, expectedActiveCount int) {
	validatorStake := calculateValidatorStake()
	total, totalWeight, err := staker.TotalStake()
	assert.NoError(t, err)
	expectedTotalStake := new(big.Int).Mul(validatorStake, big.NewInt(int64(expectedActiveCount)))
	expectedTotalWeight := new(big.Int).Mul(validatorStake, big.NewInt(int64(expectedActiveCount*2)))
	assert.Equal(t, expectedTotalStake, total)
	assert.Equal(t, expectedTotalWeight, totalWeight)
}

func waitForPoSAndAssertFirstActive(t *testing.T, staker *builtin.Staker, config *hayabusa.Config, expectedFirstActive thor.Address) uint32 {
	block := config.ForkBlock + config.TransitionPeriod
	require.NoError(t, utils.WaitForPOS(staker, block))

	_, validatorID, err := staker.FirstActive()
	assert.NoError(t, err)
	assert.Equal(t, expectedFirstActive, validatorID)
	t.Log("✅ - Validator is active")

	return block
}

func receiptToDelegationID(receipt *api.Receipt) *big.Int {
	// 0 is the event, 1 is the validation ID
	return new(big.Int).SetBytes(receipt.Outputs[0].Events[0].Topics[2][:])
}
