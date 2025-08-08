package pause

import (
	"context"
	"errors"
	"log/slog"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

var (
	executor = hayabusa.Executor
)

func TestStakerPauseForValidation(t *testing.T) {
	t.Parallel()
	config, client := setupTestNetwork(t, 3)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]
	sequence := testutil.NewTxSequence(t)

	staker := setupStakerAndWaitForFork(t, client, config)
	parames := setupParames(t, client)

	// Add two validators to the staker
	id1 := addValidator(sequence, staker, validator1, config.MinStakingPeriod)
	id2 := addValidator(sequence, staker, validator2, config.MinStakingPeriod)

	_, validatorID, err := staker.FirstQueued()
	assert.NoError(t, err)
	assert.Equal(t, id1, validatorID)
	t.Log("✅ - Queued validator OK")

	block := waitForPoSAndAssertFirstActive(t, staker, config, id1)

	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)

	t.Run("Add validation", func(t *testing.T) {
		// Set Staker pause active, the validator3 could not be added
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b10))
		_, err = sendNoRequire(t, validator3, staker.AddValidation(validator3.Address(), calculateValidatorStake(), config.MinStakingPeriod))
		require.ErrorContains(t, err, "staker is paused")

		// Set Staker pause inactive, the validator3 could be add
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b00))
		receipt, err := sendNoRequire(t, validator3, staker.AddValidation(validator3.Address(), calculateValidatorStake(), config.MinStakingPeriod))
		require.NoError(t, err)
		require.NotNil(t, receipt)
		id3 := thor.BytesToAddress(receipt.Outputs[0].Events[0].Topics[2].Bytes())

		block = (receipt.Meta.BlockNumber + config.MinStakingPeriod)

		assertValidatorStatus(t, staker, id3, builtin.StakerStatusActive, block)
	})

	t.Run("Validation increases", func(t *testing.T) {
		// Set Staker pause active, the validator1 could not to increases
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b10))
		increase := big.NewInt(1e18)
		increase = big.NewInt(0).Mul(increase, big.NewInt(1e6))
		increase = big.NewInt(0).Mul(increase, big.NewInt(5))

		_, err = sendNoRequire(t, validator1, staker.IncreaseStake(id1, increase))
		require.ErrorContains(t, err, "staker is paused")

		// Set Staker pause inactive, the validator1 could be increases
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b00))
		receipt, err := sendNoRequire(t, validator1, staker.IncreaseStake(id1, increase))
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.False(t, receipt.Reverted)
		t.Log("✅ - Validator 1 stake increased tx sent")

		block = (receipt.Meta.BlockNumber + config.MinStakingPeriod)
		assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	})

	t.Run("Validation decreases", func(t *testing.T) {
		// Set Staker pause active, the validator1 could not to decrease
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b10))
		decrease := big.NewInt(1e18)
		decrease = big.NewInt(0).Mul(decrease, big.NewInt(1e6))
		decrease = big.NewInt(0).Mul(decrease, big.NewInt(3))

		_, err = sendNoRequire(t, validator1, staker.DecreaseStake(id1, decrease))
		require.ErrorContains(t, err, "staker is paused")

		// Set Staker pause inactive, the validator1 could be decrease
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b00))
		receipt, err := sendNoRequire(t, validator1, staker.DecreaseStake(id1, decrease))
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.False(t, receipt.Reverted)
		t.Log("✅ - Validator 1 stake decrease tx sent")

		block = (receipt.Meta.BlockNumber + config.MinStakingPeriod)
		assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	})

	t.Run("Validator exit", func(t *testing.T) {
		// Set Staker pause active, the validator1 could not to exit
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b10))
		_, err = sendNoRequire(t, validator1, staker.SignalExit(id1))
		require.ErrorContains(t, err, "staker is paused")

		// Set Staker pause inactive, the validator1 could be exit
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0))
		receipt, err := sendNoRequire(t, validator1, staker.SignalExit(id1))
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.False(t, receipt.Reverted)
		t.Log("✅ - Validator 1 exit tx sent")

		block = (receipt.Meta.BlockNumber + config.MinStakingPeriod)
		assertValidatorStatus(t, staker, id1, builtin.StakerStatusExited, block)
	})

	t.Run("Validator withdraw", func(t *testing.T) {
		// Set Staker pause active, the validator1 could not to withdraw
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b10))
		_, err = sendNoRequire(t, validator1, staker.WithdrawStake(id1))
		require.ErrorContains(t, err, "staker is paused")

		// Set Staker pause inactive, the validator1 could be exit
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0))
		_, err := sendNoRequire(t, validator1, staker.WithdrawStake(id1))
		require.False(t, strings.Contains(err.Error(), "staker is paused"))
		t.Log("✅ - Validator 1 withdraw tx sent")
	})
}

func TestPauseForDelegator(t *testing.T) {
	t.Parallel()
	config, client := setupTestNetwork(t, 2)
	ticker := utils.NewTicker(client)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]

	sequence := testutil.NewTxSequence(t)
	staker := setupStakerAndWaitForFork(t, client, config)
	parames := setupParames(t, client)

	// Add validator to the staker
	id1 := addValidator(sequence, staker, validator1, config.MinStakingPeriod)
	id2 := addValidator(sequence, staker, validator2, config.MinStakingPeriod)
	id3 := addValidator(sequence, staker, validator3, config.MinStakingPeriod)

	block := waitForPoSAndAssertFirstActive(t, staker, config, id1)
	assertValidatorStatus(t, staker, id1, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id2, builtin.StakerStatusActive, block)
	assertValidatorStatus(t, staker, id3, builtin.StakerStatusQueued, block)

	// Set hayabusa.Stargate account to parames
	setParame(t, sequence, parames, thor.KeyDelegatorContractAddress, big.NewInt(0).SetBytes(hayabusa.Stargate.Address().Bytes()))

	delegatorStake := new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1e6))
	delegatorStake = new(big.Int).Mul(delegatorStake, big.NewInt(10))

	delegationID := big.NewInt(0)

	t.Run("Add Delegation", func(t *testing.T) {
		// Set delegator pause active, the delegator could not be added
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b01))
		_, err := sendNoRequire(t, hayabusa.Stargate, staker.AddDelegation(id1, delegatorStake, 100))
		require.ErrorContains(t, err, "delegator is paused")

		// Set staker pause active, the delegator could not be added
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b10))
		_, err = sendNoRequire(t, hayabusa.Stargate, staker.AddDelegation(id1, delegatorStake, 100))
		require.ErrorContains(t, err, "staker is paused")

		// Set staker and delegator pause both active, the delegator could not be added
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b11))
		_, err = sendNoRequire(t, hayabusa.Stargate, staker.AddDelegation(id1, delegatorStake, 100))
		require.ErrorContains(t, err, "staker is paused")

		// Set staker and delegator pause both inactive, the delegator could be added
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b00))
		receipt, err := sendNoRequire(t, hayabusa.Stargate, staker.AddDelegation(id1, delegatorStake, 100))
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.False(t, receipt.Reverted)
		delegationID = new(big.Int).SetBytes(receipt.Outputs[0].Events[0].Topics[2].Bytes())

		block = (receipt.Meta.BlockNumber + config.EpochLength)
		require.NoError(t, ticker.WaitForBlock(block))
	})

	t.Run("Delegation Exit", func(t *testing.T) {
		// Set delegator pause active, the delegator could not be exit
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b01))
		_, err := sendNoRequire(t, hayabusa.Stargate, staker.SignalDelegationExit(delegationID))
		require.ErrorContains(t, err, "delegator is paused")

		// Set staker pause active, the delegator could not be exit
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b10))
		_, err = sendNoRequire(t, hayabusa.Stargate, staker.SignalDelegationExit(delegationID))
		require.ErrorContains(t, err, "staker is paused")

		// Set staker and delegator pause both active, the delegator could not be exit
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b11))
		_, err = sendNoRequire(t, hayabusa.Stargate, staker.SignalDelegationExit(delegationID))
		require.ErrorContains(t, err, "staker is paused")

		// Set staker and delegator pause both inactive, the delegator could be exit
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b00))
		receipt, err := sendNoRequire(t, hayabusa.Stargate, staker.SignalDelegationExit(delegationID))
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.False(t, receipt.Reverted)

		block = (receipt.Meta.BlockNumber + config.EpochLength)
		require.NoError(t, ticker.WaitForBlock(block))
	})

	t.Run("Delegation Withdraw", func(t *testing.T) {
		// Set delegator pause active, the delegator could not be withdrawn
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b01))
		_, err := sendNoRequire(t, hayabusa.Stargate, staker.WithdrawDelegation(delegationID))
		require.ErrorContains(t, err, "delegator is paused")

		// Set staker pause active, the delegator could not be withdrawn
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b10))
		_, err = sendNoRequire(t, hayabusa.Stargate, staker.WithdrawDelegation(delegationID))
		require.ErrorContains(t, err, "staker is paused")

		// Set staker and delegator pause both active, the delegator could not be withdrawn
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b11))
		_, err = sendNoRequire(t, hayabusa.Stargate, staker.WithdrawDelegation(delegationID))
		require.ErrorContains(t, err, "staker is paused")

		// Set staker and delegator pause both inactive, the delegator could be added
		setParame(t, sequence, parames, thor.KeyStakerSwitches, big.NewInt(0b00))
		receipt, err := sendNoRequire(t, hayabusa.Stargate, staker.WithdrawDelegation(delegationID))
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.False(t, receipt.Reverted)
	})
}

func setupTestNetwork(t *testing.T, maxBlockProposers uint32) (*hayabusa.Config, *thorclient.Client) {
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

func setupParames(t *testing.T, client *thorclient.Client) *builtin.Params {
	params, err := builtin.NewParams(client)
	require.NoError(t, err)
	return params
}

func addValidator(seq *testutil.TxSequence, staker *builtin.Staker, signer bind.Signer, period uint32) thor.Address {
	return addValidatorWithStake(seq, staker, signer, calculateValidatorStake(), period)
}

func addValidatorWithStake(seq *testutil.TxSequence, staker *builtin.Staker, signer bind.Signer, stake *big.Int, period uint32) thor.Address {
	receipt := seq.Send(signer, staker.AddValidation(signer.Address(), stake, period))
	id := receipt.Outputs[0].Events[0].Topics[2]
	amount := big.NewInt(0).Quo(stake, big.NewInt(1e18))
	slog.Info("✅ - added validator", "validator", signer.Address().String(), "period", period, "stake", amount, "id", id.String())

	return thor.BytesToAddress(id.Bytes())
}

func calculateValidatorStake() *big.Int {
	stake := big.NewInt(1e18)
	stake = new(big.Int).Mul(stake, big.NewInt(1e6))
	stake = new(big.Int).Mul(stake, big.NewInt(25))
	return stake
}

func setParame(t *testing.T, seq *testutil.TxSequence, parames *builtin.Params, key thor.Bytes32, value *big.Int) {
	receipt := seq.Send(executor, parames.Set(key, value))
	if receipt == nil {
		require.Fail(t, "Parames Set :receipt is nil")
		return
	}
	if receipt.Reverted {
		require.Fail(t, "Parames Set :transaction reverted")
		return
	}
}

func TxOptions() *bind.TxOptions {
	gas := uint64(10_000_000)
	return &bind.TxOptions{
		Gas: &gas,
	}
}

func TxContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// Send a transaction with the method, signer and default transaction options/ context.
// It will return any occurred error, but will not use require to interrupt the execution.
// The method caller can be handled the errors as needed.
func sendNoRequire(t *testing.T, signer bind.Signer, sender *bind.MethodBuilder) (*api.Receipt, error) {
	receipt, _, err := sender.Send().
		WithOptions(TxOptions()).
		WithSigner(signer).
		SubmitAndConfirm(TxContext(t))

	if err != nil {
		return receipt, err
	}

	if receipt == nil {
		return nil, errors.New("receipt is nil")
	}

	if receipt.Reverted {
		_, err := sender.Call().
			AtRevision(receipt.Meta.BlockID.String()).
			Caller(&receipt.Meta.TxOrigin).
			Execute()
		if err != nil {
			return receipt, err
		} else {
			return receipt, errors.New("transaction did not revert as expected")
		}
	}

	return receipt, nil
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

func assertValidatorStatus(t *testing.T, staker *builtin.Staker, validatorID thor.Address, expectedStatus builtin.StakerStatus, waitForBlock uint32) {
	assert.NoError(t, utils.NewTicker(staker.Raw().Client()).WaitForBlock(waitForBlock))
	validator, err := staker.GetValidation(validatorID)
	assert.NoError(t, err)
	assert.Equal(t, expectedStatus, validator.Status)
}
