package validations

import (
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func TestHayabusaAddNonPoAValidator(t *testing.T) {
	t.Parallel()
	config, client, cancel := setupTestNetwork(t, 3)
	t.Cleanup(cancel)

	validator1NonPoA := hayabusa.AdditionalAccounts[0]
	validator1PoA := hayabusa.ValidatorAccounts[0]
	validator2PoA := hayabusa.ValidatorAccounts[1]

	staker := setupStakerAndWaitForFork(t, client, config)

	stake := calculateValidatorStake()
	firstStake := new(big.Int).Mul(stake, big.NewInt(2))

	receipt, _, err := staker.AddValidator(validator1NonPoA.Address(), firstStake, config.MinStakingPeriod, false).
		Send().WithOptions(testutil.TxOptions()).WithSigner(validator1NonPoA).SubmitAndConfirm(testutil.TxContext(t))
	assert.NoError(t, err)
	assert.True(t, receipt.Reverted)
	t.Log("✅ - Not a PoA candidate refused to join")

	id1 := addValidator(t, staker, validator1PoA, true, config.MinStakingPeriod)

	firstQueued, _, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, *firstQueued.Endorsor, validator1PoA.Address())
	t.Log("✅ - Queued validator OK")

	id2 := addValidator(t, staker, validator2PoA, true, config.MinStakingPeriod)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusQueued, config.ForkBlock)

	t.Log("✅ - Queued validator OK")

	currentBlock, err := client.Block("best")
	assert.NoError(t, err)

	ticker := utils.NewTicker(client)
	block := currentBlock.Number
	block += config.TransitionPeriod
	assert.NoError(t, ticker.WaitForBlock(block))

	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)

	id3 := addValidator(t, staker, validator1NonPoA, false, config.MinStakingPeriod)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusQueued, block)
	t.Log("✅ - Not a PoA candidate joined")

	t.Log("✅ - All 3 validators joined")
}

func TestHayabusaNoForkThenJoinLater(t *testing.T) {
	t.Parallel()
	config, client, cancel := setupTestNetwork(t, 3)
	t.Cleanup(cancel)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]

	staker := setupStakerAndWaitForFork(t, client, config)

	id1 := addValidator(t, staker, validator1, false, config.MinStakingPeriod)

	firstQueued, _, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, *firstQueued.Endorsor, validator1.Address())
	t.Log("✅ - Queued validator OK")

	block := config.ForkBlock + config.TransitionPeriod
	ticker := utils.NewTicker(client)
	require.NoError(t, ticker.WaitForBlock(block))

	_, validatorID, err := staker.FirstActive()
	assert.ErrorContains(t, err, "no active validator")
	assert.Equal(t, thor.Bytes32{}, validatorID)
	t.Log("✅ - Validator is not activated since min validator threshold is not met")

	id2 := addValidator(t, staker, validator2, false, config.MinStakingPeriod)

	block += config.TransitionPeriod
	periodStart := block
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	t.Log("✅ - Both validators are activated")

	block += config.MinStakingPeriod
	periodEnd := block
	id3 := addValidator(t, staker, validator3, true, config.MinStakingPeriod)
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)

	stake := big.NewInt(1e18)
	stake = big.NewInt(0).Mul(stake, big.NewInt(1e6))
	stake = big.NewInt(0).Mul(stake, big.NewInt(25))
	totalStake := big.NewInt(0).Mul(stake, big.NewInt(2))
	assertRewards(t, staker, id1, totalStake, periodStart, periodEnd)

	t.Log("✅ - All three validators are activated")
}

func TestHayabusaFullFlowJoinQueuedCooldownExit(t *testing.T) {
	t.Parallel()
	config, client, cancel := setupTestNetwork(t, 3)
	t.Cleanup(cancel)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]

	staker := setupStakerAndWaitForFork(t, client, config)

	id1 := addValidator(t, staker, validator1, false, config.MinStakingPeriod)
	id2 := addValidator(t, staker, validator2, false, config.MinStakingPeriod)
	id3 := addValidator(t, staker, validator3, true, config.MinStakingPeriod)

	_, validatorID, err := staker.FirstQueued()
	assert.NoError(t, err)

	assert.Equal(t, id1.String(), validatorID.String())
	t.Log("✅ - Queued validator OK")

	block := config.ForkBlock + config.TransitionPeriod
	periodStart := block
	require.NoError(t, utils.WaitForPOS(staker, block))

	_, validatorID, err = staker.FirstActive()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Active validator OK")

	// assert validators are active
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)

	// assert validators staking periods
	assertValidatorStakingPeriod(t, staker, id1, config.MinStakingPeriod)
	assertValidatorStakingPeriod(t, staker, id2, config.MinStakingPeriod)
	assertValidatorStakingPeriod(t, staker, id3, config.MinStakingPeriod)

	t.Log("✅ - All three validators are activated")

	// assert validators are on cooldown
	block += config.MinStakingPeriod
	periodEnd := block
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)
	stake := big.NewInt(1e18)
	stake = big.NewInt(0).Mul(stake, big.NewInt(1e6))
	stake = big.NewInt(0).Mul(stake, big.NewInt(25))
	totalStake := big.NewInt(0).Mul(stake, big.NewInt(3))
	assertRewards(t, staker, id1, totalStake, periodStart, periodEnd)

	t.Log("✅ - Non-AutoRenew validators are on cooldown")

	// assert 1 validator has exited
	block += config.EpochLength
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)

	t.Log("✅ - One validator has exited")

	// assert 1 validator remains
	block += config.EpochLength
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)

	t.Log("✅ - Second validator exited")
}

func TestHayabusaQueuedAndThenEnter(t *testing.T) {
	t.Parallel()
	config, client, cancel := setupTestNetwork(t, 3)
	t.Cleanup(cancel)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]
	validator4 := hayabusa.ValidatorAccounts[3]
	validator5 := hayabusa.ValidatorAccounts[4]

	staker := setupStakerAndWaitForFork(t, client, config)

	stake := big.NewInt(1e18)
	stake = new(big.Int).Mul(stake, big.NewInt(1e6))
	stake = new(big.Int).Mul(stake, big.NewInt(26))
	id1 := addValidator(t, staker, validator1, true, config.MinStakingPeriod)
	id2 := addValidator(t, staker, validator2, true, config.MinStakingPeriod)
	id3 := addValidator(t, staker, validator3, true, config.MinStakingPeriod)
	id4 := addValidator(t, staker, validator4, false, config.MinStakingPeriod)

	_, validatorID, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Queued validator OK")

	block := config.ForkBlock + config.TransitionPeriod
	periodStart := block
	require.NoError(t, utils.WaitForPOS(staker, block))

	_, validatorID, err = staker.FirstActive()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Validator is active")

	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id4, builtin.StakerStatusQueued, block)
	t.Log("✅ - Three validators are activated one is queued")

	assertTotalStakeAndWeight(t, staker, 3)
	assertQueuedStakeAndWeight(t, staker, 1)

	id5 := addValidatorWithStake(t, staker, validator5, false, stake, config.MinStakingPeriod)
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id4, builtin.StakerStatusQueued, block)
	assertValidatorStatus(t, staker, id5, builtin.StakerStatusQueued, block)

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

	receipt, _, err := staker.UpdateAutoRenew(id3, false).
		Send().
		WithSigner(validator3).
		WithOptions(testutil.TxOptions()).
		SubmitAndConfirm(testutil.TxContext(t))
	assert.NoError(t, err)
	require.False(t, receipt.Reverted, "Transaction should not be reverted")
	assert.Equal(t, staker.Raw().Address().String(), receipt.Outputs[0].Events[0].Address.String())
	assert.Equal(t, validator3.Address().Bytes(), receipt.Outputs[0].Events[0].Topics[1].Bytes()[12:])
	assert.Equal(t, id3, receipt.Outputs[0].Events[0].Topics[2])

	t.Log("✅ - AutoRenew updated")

	block += config.MinStakingPeriod
	periodEnd := block
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id4, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id5, builtin.StakerStatusQueued, block)

	minStake := calculateValidatorStake()
	totalStake := new(big.Int).Mul(minStake, big.NewInt(3))
	assertRewards(t, staker, id2, totalStake, periodStart, periodEnd)

	_, validationID, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id5, validationID)

	t.Log("✅ - Three validators are activated, 2 are queued, queue order has changed based on weight")

	receipt, _, err = staker.UpdateAutoRenew(id4, true).
		Send().
		WithSigner(validator4).
		WithOptions(testutil.TxOptions()).
		SubmitAndConfirm(testutil.TxContext(t))
	assert.NoError(t, err)
}

func TestHayabusaTotalStakeDecreased(t *testing.T) {
	t.Parallel()
	config, client, cancel := setupTestNetwork(t, 3)
	t.Cleanup(cancel)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]

	staker := setupStakerAndWaitForFork(t, client, config)

	stake := big.NewInt(1e18)
	stake = big.NewInt(0).Mul(stake, big.NewInt(1e6))
	stake = big.NewInt(0).Mul(stake, big.NewInt(26))
	id1 := addValidator(t, staker, validator1, false, config.MinStakingPeriod)
	id2 := addValidator(t, staker, validator2, true, config.MinStakingPeriod)

	_, validatorID, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Queued validator OK")

	block := waitForPoSAndAssertFirstActive(t, staker, config, id1)

	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)

	assertTotalStakeAndWeight(t, staker, 2)

	block += config.MinStakingPeriod
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)

	assertTotalStakeAndWeight(t, staker, 1)
}

func TestHayabusaQueuedWeightDecreasedWhenValidatorExits(t *testing.T) {
	t.Parallel()
	config, client, cancel := setupTestNetwork(t, 2)
	t.Cleanup(cancel)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]

	staker, err := builtin.NewStaker(client)
	require.NoError(t, err)
	require.NoError(t, utils.WaitForFork(staker, config.ForkBlock))

	stake := big.NewInt(1e18)
	stake = big.NewInt(0).Mul(stake, big.NewInt(1e6))
	stake = big.NewInt(0).Mul(stake, big.NewInt(26))
	id1 := addValidator(t, staker, validator1, false, config.MinStakingPeriod)
	id2 := addValidator(t, staker, validator2, true, config.MinStakingPeriod)

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

	validatorStake := big.NewInt(1e18)
	validatorStake = big.NewInt(0).Mul(validatorStake, big.NewInt(1e6))
	validatorStake = big.NewInt(0).Mul(validatorStake, big.NewInt(25))
	total, totalWeight, err := staker.TotalStake()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(0).Mul(validatorStake, big.NewInt(2)), total)
	assert.Equal(t, big.NewInt(0).Mul(validatorStake, big.NewInt(4)), totalWeight)

	block += config.MinStakingPeriod
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)

	total, totalWeight, err = staker.TotalStake()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(0).Mul(validatorStake, big.NewInt(1)), total)
	assert.Equal(t, big.NewInt(0).Mul(validatorStake, big.NewInt(2)), totalWeight)
}

func TestHayabusaQueuedWeightDecreasedWhenValidatorSelectedForLeaderGroup(t *testing.T) {
	t.Parallel()
	config, client, cancel := setupTestNetwork(t, 3)
	t.Cleanup(cancel)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]

	staker, err := builtin.NewStaker(client)
	require.NoError(t, err)
	require.NoError(t, utils.WaitForFork(staker, config.ForkBlock))

	stake := big.NewInt(1e18)
	stake = big.NewInt(0).Mul(stake, big.NewInt(1e6))
	stake = big.NewInt(0).Mul(stake, big.NewInt(26))
	id1 := addValidator(t, staker, validator1, false, config.MinStakingPeriod)
	id2 := addValidator(t, staker, validator2, true, config.MinStakingPeriod)
	id3 := addValidator(t, staker, validator3, true, config.MinStakingPeriod)

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

	validatorStake := big.NewInt(1e18)
	validatorStake = big.NewInt(0).Mul(validatorStake, big.NewInt(1e6))
	validatorStake = big.NewInt(0).Mul(validatorStake, big.NewInt(25))
	total, totalWeight, err := staker.TotalStake()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(0).Mul(validatorStake, big.NewInt(3)), total)
	assert.Equal(t, big.NewInt(0).Mul(validatorStake, big.NewInt(6)), totalWeight)

	block += config.MinStakingPeriod
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusExited, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)

	total, totalWeight, err = staker.TotalStake()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(0).Mul(validatorStake, big.NewInt(2)), total)
	assert.Equal(t, big.NewInt(0).Mul(validatorStake, big.NewInt(4)), totalWeight)
}

func TestHayabusaQueuedStakeAndWeightChangesWhenDelegator(t *testing.T) {
	t.Parallel()
	_, _, cancel := setupTestNetwork(t, 1)
	t.Cleanup(cancel)
}

func addValidatorWithStake(t *testing.T, staker *builtin.Staker, signer bind.Signer, autoRenew bool, stake *big.Int, period uint32) thor.Bytes32 {
	sender := staker.AddValidator(signer.Address(), stake, period, autoRenew).Send().WithSigner(signer).WithOptions(testutil.TxOptions())
	receipt, _, err := sender.SubmitAndConfirm(testutil.TxContext(t))
	require.NoError(t, err)
	require.False(t, receipt.Reverted, "Transaction should not be reverted")
	assert.Equal(t, staker.Raw().Address().String(), receipt.Outputs[0].Events[0].Address.String())
	assert.Equal(t, signer.Address().Bytes(), receipt.Outputs[0].Events[0].Topics[1].Bytes()[12:])
	assert.Equal(t, signer.Address().Bytes(), receipt.Outputs[0].Events[0].Topics[2].Bytes()[12:])

	id := receipt.Outputs[0].Events[0].Topics[3]
	amount := big.NewInt(0).Quo(stake, big.NewInt(1e18))
	slog.Info("✅ - added validator", "validator", signer.Address().String(), "autoRenew", autoRenew, "period", period, "stake", amount, "id", id.String())

	return id
}

func addValidator(t *testing.T, staker *builtin.Staker, signer bind.Signer, autoRenew bool, period uint32) thor.Bytes32 {
	stake := big.NewInt(1e18)
	stake = big.NewInt(0).Mul(stake, big.NewInt(1e6))
	stake = big.NewInt(0).Mul(stake, big.NewInt(25))
	return addValidatorWithStake(t, staker, signer, autoRenew, stake, period)
}

func assertValidatorStatus(t *testing.T, staker *builtin.Staker, validatorID thor.Bytes32, expectedStatus builtin.StakerStatus, waitForBlock uint32) {
	assert.NoError(t, utils.NewTicker(staker.Raw().Client()).WaitForBlock(waitForBlock))
	validator, err := staker.Get(validatorID)
	assert.NoError(t, err)
	assert.Equal(t, expectedStatus, validator.Status)
}

func assertValidatorStakingPeriod(t *testing.T, staker *builtin.Staker, validatorID thor.Bytes32, expectedPeriod uint32) {
	validator, err := staker.Get(validatorID)
	assert.NoError(t, err)
	assert.Equal(t, expectedPeriod, validator.Period)
}

func assertRewards(t *testing.T, staker *builtin.Staker, validatorID thor.Bytes32, totalStaked *big.Int, periodStart uint32, periodEnd uint32) {
	expectedReward := hayabusa.GetExpectedReward(totalStaked)
	validator, err := staker.Get(validatorID)
	assert.NoError(t, err)

	proposedBlocks := 0
	for periodStart < periodEnd {
		block, err := staker.Raw().Client().Block(strconv.Itoa(int(periodStart)))
		assert.NoError(t, err)
		periodStart = periodStart + 1
		if block.Signer.String() == validator.Master.String() {
			proposedBlocks = proposedBlocks + 1
		}
	}

	res, err := staker.GetRewards(validatorID, 1)
	assert.NoError(t, err)

	assert.Equal(t, big.NewInt(0).Mul(expectedReward, big.NewInt(int64(proposedBlocks))).String(), res.String())
}

func setupTestNetwork(t *testing.T, maxBlockProposers uint32) (*hayabusa.Config, *thorclient.Client, func()) {
	config := &hayabusa.Config{
		Nodes:             6,
		MaxBlockProposers: maxBlockProposers,
		ForkBlock:         0,
		TransitionPeriod:  6,
		EpochLength:       2,
		CooldownPeriod:    2,
		MinStakingPeriod:  4,
		MidStakingPeriod:  12,
		HighStakingPeriod: 259200,
	}

	// Generate unique ID for this test
	testID := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	client, _, cancel, err := hayabusa.StartNetworkWithID(config, testID)
	if err != nil {
		t.Fatal(err)
	}
	return config, client, cancel
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

func waitForPoSAndAssertFirstActive(t *testing.T, staker *builtin.Staker, config *hayabusa.Config, expectedID thor.Bytes32) uint32 {
	block := config.ForkBlock + config.TransitionPeriod
	require.NoError(t, utils.WaitForPOS(staker, block))

	_, validatorID, err := staker.FirstActive()
	assert.NoError(t, err)
	assert.Equal(t, expectedID, validatorID)
	t.Log("✅ - Validator is active")
	return block
}

func assertTotalStakeAndWeight(t *testing.T, staker *builtin.Staker, expectedValidators int) {
	validatorStake := big.NewInt(1e18)
	validatorStake = big.NewInt(0).Mul(validatorStake, big.NewInt(1e6))
	validatorStake = big.NewInt(0).Mul(validatorStake, big.NewInt(25))
	total, totalWeight, err := staker.TotalStake()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(0).Mul(validatorStake, big.NewInt(int64(expectedValidators))), total)
	assert.Equal(t, big.NewInt(0).Mul(validatorStake, big.NewInt(int64(expectedValidators*2))), totalWeight)
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
