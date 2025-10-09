package reentrancy

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/tests/reentrancy/delegatorattack"
	"github.com/vechain/hayabusa-e2e/tests/reentrancy/validatorattack"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/thorclient"
	bind2 "github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func TestDelegator_Reentrancy(t *testing.T) {
	client, config, staker, val1 := newTestSetup(t, 2)

	// Deploy the Reentrancy contract
	contractAddress := testutil.DeployContract(t, client, hayabusa.AdditionalAccounts[0], delegatorattack.Bin)
	testutil.SetDelegatorContract(t, client, contractAddress)
	reentrancyAttack, err := bind2.NewContract(client, delegatorattack.ABI, &contractAddress)
	require.NoError(t, err)

	require.NoError(t, utils.WaitForPOS(t.Context(), staker, config.ForkBlock+config.TransitionPeriod))

	stake := new(big.Int).Div(builtin.MinStake(), big.NewInt(5))
	// Start the attack
	testutil.Send(t, hayabusa.Stargate, reentrancyAttack.Method("setupDelegation", val1.Node.Address(), uint8(200)).WithValue(stake))
	receipt := testutil.Send(t, hayabusa.Stargate, reentrancyAttack.Method("executeAttack"))

	received := big.NewInt(0)
	for _, output := range receipt.Outputs {
		for _, transfer := range output.Transfers {
			received.Add(received, (*big.Int)(transfer.Amount))
		}
	}
	assert.Equal(t, stake, received)

	contract, err := client.Account(&contractAddress)
	require.NoError(t, err)
	balance := (*big.Int)(contract.Balance)
	assert.Equal(t, stake.Cmp(balance), 0, "expected = 0, got %s", balance.String())
}

func TestValidator_Reentrancy(t *testing.T) {
	client, config, staker, _ := newTestSetup(t, 3)

	// Deploy the Reentrancy contract
	contractAddress := testutil.DeployContract(t, client, hayabusa.AdditionalAccounts[0], validatorattack.Bin)
	testutil.SetDelegatorContract(t, client, contractAddress)
	reentrancyAttack, err := bind2.NewContract(client, validatorattack.ABI, &contractAddress)
	require.NoError(t, err)

	require.NoError(t, utils.WaitForPOS(t.Context(), staker, config.ForkBlock+config.TransitionPeriod))

	validator := hayabusa.ValidatorAccounts[2]
	// Add the validation through the reentrancy attack contract
	stake := builtin.MinStake()
	testutil.Send(t, hayabusa.Stargate, reentrancyAttack.Method("addValidation", validator.Node.Address(), config.MinStakingPeriod).WithValue(stake))

	// wait until contractAddress is an active validator
	block := config.ForkBlock + config.TransitionPeriod + config.MinStakingPeriod
	require.NoError(t, utils.WaitForCondition(t.Context(), client, block, func() (bool, error) {
		val, err := staker.GetValidation(validator.Node.Address())
		if err != nil {
			return false, err
		}
		return val.Status == builtin.StakerStatusActive, nil
	}))

	// signal the exit
	testutil.Send(t, hayabusa.AdditionalAccounts[0], reentrancyAttack.Method("signalExit"))

	// wait until the validation is exited and cooldown
	block += config.MinStakingPeriod + config.CooldownPeriod
	require.NoError(t, utils.WaitForCondition(t.Context(), client, block, func() (bool, error) {
		val, err := staker.GetWithdrawable(validator.Node.Address())
		if err != nil {
			return false, err
		}
		return val.Cmp(stake) >= 0, nil
	}))

	// execute the attack
	receipt := testutil.Send(t, hayabusa.Stargate, reentrancyAttack.Method("executeAttack"))
	received := big.NewInt(0)
	for _, output := range receipt.Outputs {
		for _, transfer := range output.Transfers {
			received.Add(received, (*big.Int)(transfer.Amount))
		}
	}
	assert.Equal(t, stake, received)

	contract, err := client.Account(&contractAddress)
	require.NoError(t, err)
	balance := (*big.Int)(contract.Balance)
	assert.Equal(t, stake.Cmp(balance), 0, "expected = 0, got %s", balance.String())
}

func newTestSetup(t *testing.T, nodes int) (*thorclient.Client, *hayabusa.Config, *builtin.Staker, *hayabusa.NodePair) {
	config := &hayabusa.Config{
		Nodes:             nodes,
		MaxBlockProposers: uint32(nodes),
		ForkBlock:         0,
		TransitionPeriod:  6,
		EpochLength:       6,
		CooldownPeriod:    6,
		MinStakingPeriod:  6,
		MidStakingPeriod:  12,
		HighStakingPeriod: 24,
		Name:              t.Name(),
		BlockInterval:     uint64(2),
	}

	// Network, client and staker setup
	network, err := hayabusa.NewNetwork(config, t.Context())
	require.NoError(t, err)
	require.NoError(t, network.Start())
	t.Cleanup(network.Stop)
	client := network.ThorClient()
	staker, err := builtin.NewStaker(network.ThorClient())
	require.NoError(t, err)
	require.NoError(t, utils.WaitForFork(t.Context(), staker, config.ForkBlock))

	// Add the validators
	stake := new(big.Int).Mul(builtin.MinStake(), big.NewInt(4))
	val1 := hayabusa.ValidatorAccounts[0]
	val2 := hayabusa.ValidatorAccounts[1]
	testutil.Send(t, val1.Endorser, staker.AddValidation(val1.Node.Address(), stake, config.MinStakingPeriod))
	testutil.Send(t, val2.Endorser, staker.AddValidation(val2.Node.Address(), stake, config.MinStakingPeriod))

	return client, config, staker, val1
}
