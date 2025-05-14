package validations

import (
	"crypto/ecdsa"
	"log/slog"
	"math/big"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/draupnir/common"
	"github.com/vechain/hayabusa-e2e/builtins"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/thor/v2/thor"
)

func TestHayabusaNoForkThenJoinLater(t *testing.T) {
	config := &hayabusa.Config{
		Nodes:             6,
		MaxBlockProposers: 3,
		ForkBlock:         2,
		TransitionPeriod:  2,
		EpochLength:       2,
		CooldownPeriod:    2,
		MinStakingPeriod:  2,
		MidStakingPeriod:  12,
		HighStakingPeriod: 259200,
	}
	client, _, cancel, err := hayabusa.StartNetwork(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]

	block := config.ForkBlock
	staker := builtins.NewStaker(client, validator1.PrivateKey)
	assert.NoError(t, staker.WaitForFork(block))
	ticker := common.NewTicker(client)

	id1 := addValidator(t, staker, validator1.PrivateKey, validator1.Address, false, config.MinStakingPeriod)

	firstQueued, _, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, firstQueued.Endorsor, &validator1.Address)
	t.Log("✅ - Queued validator OK")

	block += config.TransitionPeriod
	require.NoError(t, ticker.WaitForBlock(block))

	_, validatorID, err := staker.FirstActive()
	assert.ErrorContains(t, err, "no active validator")
	assert.Equal(t, thor.Bytes32{}, validatorID)
	t.Log("✅ - Validator is not activated since min validator threshold is not met")

	id2 := addValidator(t, staker, validator2.PrivateKey, validator2.Address, false, config.MinStakingPeriod)

	var validatorIDs []thor.Bytes32
	block += config.TransitionPeriod
	periodStart := block
	assertValidatorStatus(t, staker, id1, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id2, builtins.StatusActive, block)
	t.Log("✅ - Both validators are activated")

	validatorIDs = append(validatorIDs, id1)
	validatorIDs = append(validatorIDs, id2)

	block += config.MinStakingPeriod
	periodEnd := block
	id3 := addValidator(t, staker, validator3.PrivateKey, validator3.Address, true, config.MinStakingPeriod)
	validatorIDs = append(validatorIDs, id3)
	assertValidatorStatus(t, staker, id1, builtins.StatusCooldown, block)
	assertValidatorStatus(t, staker, id2, builtins.StatusCooldown, block)
	assertValidatorStatus(t, staker, id3, builtins.StatusActive, block)

	stake := big.NewInt(1e18)
	stake = big.NewInt(0).Mul(stake, big.NewInt(1e6))
	stake = big.NewInt(0).Mul(stake, big.NewInt(25))
	totalStake := big.NewInt(0).Mul(stake, big.NewInt(2))
	assertRewards(t, staker, id1, totalStake, periodStart, periodEnd)

	t.Log("✅ - All three validators are activated")
}

func TestHayabusaFullFlowJoinQueuedCooldownExit(t *testing.T) {
	config := &hayabusa.Config{
		Nodes:             6,
		MaxBlockProposers: 3,
		ForkBlock:         0,
		TransitionPeriod:  4,
		EpochLength:       2,
		CooldownPeriod:    2,
		MinStakingPeriod:  2,
		MidStakingPeriod:  12,
		HighStakingPeriod: 259200,
	}
	client, _, cancel, err := hayabusa.StartNetwork(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]
	var validatorIDs []thor.Bytes32

	staker := builtins.NewStaker(client, validator1.PrivateKey)
	assert.NoError(t, staker.WaitForFork(config.ForkBlock))
	ticker := common.NewTicker(client)

	id1 := addValidator(t, staker, validator1.PrivateKey, validator1.Address, false, config.MinStakingPeriod)
	validatorIDs = append(validatorIDs, id1)
	id2 := addValidator(t, staker, validator2.PrivateKey, validator2.Address, false, config.MinStakingPeriod)
	validatorIDs = append(validatorIDs, id2)
	id3 := addValidator(t, staker, validator3.PrivateKey, validator3.Address, true, config.MinStakingPeriod)
	validatorIDs = append(validatorIDs, id3)

	_, validatorID, err := staker.FirstQueued()
	assert.NoError(t, err)

	assert.Equal(t, id1.String(), validatorID.String())
	t.Log("✅ - Queued validator OK")

	block := config.ForkBlock + config.TransitionPeriod
	periodStart := block
	require.NoError(t, ticker.WaitForBlock(block))

	_, validatorID, err = staker.FirstActive()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Active validator OK")

	// assert validators are active
	assertValidatorStatus(t, staker, id1, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id2, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id3, builtins.StatusActive, block)

	t.Log("✅ - All three validators are activated")

	// assert validators are on cooldown
	block += config.MinStakingPeriod
	periodEnd := block
	assertValidatorStatus(t, staker, id1, builtins.StatusCooldown, block)
	assertValidatorStatus(t, staker, id2, builtins.StatusCooldown, block)
	assertValidatorStatus(t, staker, id3, builtins.StatusActive, block)
	stake := big.NewInt(1e18)
	stake = big.NewInt(0).Mul(stake, big.NewInt(1e6))
	stake = big.NewInt(0).Mul(stake, big.NewInt(25))
	totalStake := big.NewInt(0).Mul(stake, big.NewInt(3))
	assertRewards(t, staker, id1, totalStake, periodStart, periodEnd)

	t.Log("✅ - Non-AutoRenew validators are on cooldown")

	// assert 1 validator has exited
	block += config.CooldownPeriod
	assertValidatorStatus(t, staker, id1, builtins.StatusExited, block)
	assertValidatorStatus(t, staker, id2, builtins.StatusCooldown, block)
	assertValidatorStatus(t, staker, id3, builtins.StatusActive, block)

	t.Log("✅ - One validator has exited")

	// assert 1 validator has exited rest are forbidden because of 2/3 rule
	block += config.EpochLength
	require.NoError(t, ticker.WaitForBlock(block))
	assertValidatorStatus(t, staker, id1, builtins.StatusExited, block)
	assertValidatorStatus(t, staker, id2, builtins.StatusExited, block)
	assertValidatorStatus(t, staker, id3, builtins.StatusActive, block)

	t.Log("✅ - Second validator exited")

	validatorWithdraw(t, staker, validator1.PrivateKey, id1)
}

func TestHayabusaQueuedAndThenEnter(t *testing.T) {
	config := &hayabusa.Config{
		Nodes:             6,
		MaxBlockProposers: 3,
		ForkBlock:         0,
		TransitionPeriod:  6,
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

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]
	validator4 := hayabusa.ValidatorAccounts[3]
	validator5 := hayabusa.ValidatorAccounts[4]
	var validatorIDs []thor.Bytes32

	staker := builtins.NewStaker(client, validator1.PrivateKey)
	assert.NoError(t, staker.WaitForFork(config.ForkBlock))
	ticker := common.NewTicker(client)

	stake := big.NewInt(1e18)
	stake = big.NewInt(0).Mul(stake, big.NewInt(1e6))
	stake = big.NewInt(0).Mul(stake, big.NewInt(26))
	id1 := addValidator(t, staker, validator1.PrivateKey, validator1.Address, true, config.MinStakingPeriod)
	validatorIDs = append(validatorIDs, id1)
	id2 := addValidator(t, staker, validator2.PrivateKey, validator2.Address, true, config.MinStakingPeriod)
	validatorIDs = append(validatorIDs, id2)
	id3 := addValidator(t, staker, validator3.PrivateKey, validator3.Address, true, config.MinStakingPeriod)
	validatorIDs = append(validatorIDs, id3)
	id4 := addValidator(t, staker, validator4.PrivateKey, validator4.Address, true, config.MinStakingPeriod)
	validatorIDs = append(validatorIDs, id4)

	_, validatorID, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Queued validator OK")

	block := config.ForkBlock + config.TransitionPeriod
	periodStart := block
	require.NoError(t, ticker.WaitForBlock(block))

	_, validatorID, err = staker.FirstActive()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Validator is active")

	assertValidatorStatus(t, staker, id1, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id2, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id3, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id4, builtins.StatusQueued, block)
	t.Log("✅ - Three validators are activated one is queued")

	id5 := addValidatorWithStake(t, staker, validator5.PrivateKey, validator5.Address, false, stake, config.MinStakingPeriod)
	validatorIDs = append(validatorIDs, id5)
	assertValidatorStatus(t, staker, id1, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id2, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id3, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id4, builtins.StatusQueued, block)
	assertValidatorStatus(t, staker, id5, builtins.StatusQueued, block)

	_, validatorID, err = staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id4, validatorID)
	t.Log("✅ - Three validators are activated, 2 are queued, queue order has changed based on weight")

	receipt, _, err := staker.Attach(validator3.PrivateKey).UpdateAutoRenew(id3, false).Receipt(false)
	assert.NoError(t, err)
	assert.Equal(t, staker.Address().String(), receipt.Outputs[0].Events[0].Address.String())
	assert.Equal(t, validator3.Address.Bytes(), receipt.Outputs[0].Events[0].Topics[1].Bytes()[12:])
	assert.Equal(t, id3, receipt.Outputs[0].Events[0].Topics[2])

	t.Log("✅ - AutoRenew updated")

	block += config.MinStakingPeriod
	periodEnd := block
	assertValidatorStatus(t, staker, id1, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id2, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id3, builtins.StatusCooldown, block)
	assertValidatorStatus(t, staker, id4, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id5, builtins.StatusQueued, block)

	minStake := big.NewInt(1e18)
	minStake = big.NewInt(0).Mul(minStake, big.NewInt(1e6))
	minStake = big.NewInt(0).Mul(minStake, big.NewInt(25))
	totalStake := big.NewInt(0).Mul(minStake, big.NewInt(3))
	assertRewards(t, staker, id2, totalStake, periodStart, periodEnd)

	_, validationID, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id5, validationID)

	t.Log("✅ - Three validators are activated, 2 are queued, queue order has changed based on weight")

	block += config.CooldownPeriod
	assertValidatorStatus(t, staker, id1, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id2, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id3, builtins.StatusExited, block)
	assertValidatorStatus(t, staker, id4, builtins.StatusActive, block)
	assertValidatorStatus(t, staker, id5, builtins.StatusQueued, block)

	t.Log("✅ - Three validators are active one is queued and one has exited")
}

func addValidatorWithStake(t *testing.T, staker *builtins.Staker, pk *ecdsa.PrivateKey, validatorAddress thor.Address, autoRenew bool, stake *big.Int, period uint32) thor.Bytes32 {
	sender := staker.Attach(pk).AddValidator(validatorAddress, stake, period, autoRenew)
	receipt, _, err := sender.Receipt(false)
	assert.NoError(t, err)
	assert.Equal(t, staker.Address().String(), receipt.Outputs[0].Events[0].Address.String())
	assert.Equal(t, validatorAddress.Bytes(), receipt.Outputs[0].Events[0].Topics[1].Bytes()[12:])
	assert.Equal(t, validatorAddress.Bytes(), receipt.Outputs[0].Events[0].Topics[2].Bytes()[12:])

	id := receipt.Outputs[0].Events[0].Topics[3]
	amount := big.NewInt(0).Quo(stake, big.NewInt(1e18))
	slog.Info("✅ - added validator", "validator", validatorAddress.String(), "autoRenew", autoRenew, "period", period, "stake", amount, "id", id.String())

	return id
}

func addValidator(t *testing.T, staker *builtins.Staker, pk *ecdsa.PrivateKey, validatorAddress thor.Address, autoRenew bool, period uint32) thor.Bytes32 {
	stake := big.NewInt(1e18)
	stake = big.NewInt(0).Mul(stake, big.NewInt(1e6))
	stake = big.NewInt(0).Mul(stake, big.NewInt(25))
	return addValidatorWithStake(t, staker, pk, validatorAddress, autoRenew, stake, period)
}

func validatorWithdraw(t *testing.T, staker *builtins.Staker, pk *ecdsa.PrivateKey, validatorID thor.Bytes32) {
	receipt, _, err := staker.Attach(pk).Withdraw(validatorID).Receipt(false)
	assert.NoError(t, err)
	addr := thor.Address(crypto.PubkeyToAddress(pk.PublicKey))
	assert.Equal(t, addr.Bytes(), receipt.Outputs[0].Events[0].Topics[1].Bytes()[12:])
	assert.Equal(t, validatorID, receipt.Outputs[0].Events[0].Topics[2])
	assert.Len(t, receipt.Outputs[0].Transfers, 1)
	assert.Equal(t, receipt.Outputs[0].Transfers[0].Recipient, addr)
	slog.Info("✅ - validator withdrawn", "validator", validatorID.String())
}

func assertValidatorStatus(t *testing.T, staker *builtins.Staker, validatorID thor.Bytes32, expectedStatus builtins.Status, waitForBlock uint32) {
	assert.NoError(t, common.NewTicker(staker.Client()).WaitForBlock(waitForBlock))
	validator, err := staker.Get(validatorID)
	assert.NoError(t, err)
	assert.Equal(t, expectedStatus, validator.Status)
}

func assertRewards(t *testing.T, staker *builtins.Staker, validatorID thor.Bytes32, totalStaked *big.Int, periodStart uint32, periodEnd uint32) {

	expectedReward := getExpectedReward(totalStaked)
	validator, err := staker.Get(validatorID)
	assert.NoError(t, err)

	proposedBlocks := 0
	for periodStart < periodEnd {
		block, err := staker.Client().Block(strconv.Itoa(int(periodStart)))
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

func getExpectedReward(totalStaked *big.Int) *big.Int {
	currentYear := time.Now().Year()
	isLeap := false
	if currentYear%4 == 0 {
		if currentYear%100 == 0 {
			isLeap = currentYear%400 == 0
		} else {
			isLeap = true
		}
	}

	bigE18 := big.NewInt(1e18)
	sqrtStake := new(big.Int).Sqrt(new(big.Int).Div(totalStaked, bigE18))
	sqrtStake.Mul(sqrtStake, bigE18)

	blocksPerYear := big.NewInt(3153600)
	scalingFactor := big.NewInt(64)
	targetFactor := big.NewInt(1200)

	if isLeap {
		blocksPerYear = new(big.Int).Sub(blocksPerYear, big.NewInt(thor.SeederInterval))
	}

	reward := big.NewInt(1)
	reward.Mul(reward, targetFactor)
	reward.Mul(reward, scalingFactor)
	reward.Mul(reward, sqrtStake)
	reward.Div(reward, blocksPerYear)

	return reward
}
