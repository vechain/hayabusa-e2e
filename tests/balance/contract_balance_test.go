package balance

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/tests/balance/selfdestruct"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/thorclient"
	bind2 "github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
	"github.com/vechain/thor/v2/tx"
)

func Test_ContractSuicide_StakerRecipient(t *testing.T) {
	config := &hayabusa.Config{
		Nodes:             2,
		MaxBlockProposers: 2,
		ForkBlock:         0,
		TransitionPeriod:  4,
		EpochLength:       4,
		CooldownPeriod:    4,
		MinStakingPeriod:  4,
		MidStakingPeriod:  12,
		HighStakingPeriod: 24,
		Name:              t.Name(),
		BlockInterval:     uint64(5),
	}

	// Network, client and staker setup
	network, err := hayabusa.NewNetwork(config, t.Context())
	require.NoError(t, err)
	require.NoError(t, network.Start())
	defer network.Stop()
	client := network.ThorClient()
	staker, err := builtin.NewStaker(network.ThorClient())
	require.NoError(t, err)
	require.NoError(t, utils.WaitForFork(t.Context(), staker, config.ForkBlock))

	// Add the validators
	val1 := hayabusa.ValidatorAccounts[0]
	val2 := hayabusa.ValidatorAccounts[1]
	testutil.Send(t, val1.Endorser, staker.AddValidation(val1.Node.Address(), builtin.MinStake(), config.MinStakingPeriod))
	testutil.Send(t, val2.Endorser, staker.AddValidation(val2.Node.Address(), builtin.MinStake(), config.MinStakingPeriod))
	require.NoError(t, utils.WaitForPOS(t.Context(), staker, config.ForkBlock+config.TransitionPeriod))

	// Deploy the SelfDestruct contract
	deployClause := tx.NewClause(nil).WithData(hexutil.MustDecode("0x" + selfdestruct.Bin))
	receipt := testutil.SendClauses(t, hayabusa.AdditionalAccounts[0], []*tx.Clause{deployClause}, client, testutil.TxContext(t))
	contractAddress := receipt.Outputs[0].ContractAddress
	bind, err := bind2.NewContract(client, selfdestruct.ABI, contractAddress)
	require.NoError(t, err)

	// Send VET to the new contract
	sendClause := tx.NewClause(contractAddress).WithValue(builtin.MinStake())
	testutil.SendClauses(t, hayabusa.AdditionalAccounts[0], []*tx.Clause{sendClause}, client, testutil.TxContext(t))

	// Self-destruct the contract, sending balance to a staker
	testutil.Send(t, hayabusa.AdditionalAccounts[0], bind.Method("destroy"))
	assertStakerBalance(t, client, staker, new(big.Int).Mul(builtin.MinStake(), big.NewInt(3)))

	// TODO: This is failing, housekeeping fails due to balance check
	// Signal exit and wait for 1 validator
	receipt = testutil.Send(t, val1.Endorser, staker.SignalExit(val1.Node.Address()))
	exitBlock := receipt.Meta.BlockNumber + (config.MinStakingPeriod - receipt.Meta.BlockNumber%config.MinStakingPeriod)
	assert.NoError(t, utils.NewTicker(staker.Raw().Client()).WaitForBlock(exitBlock))
	validation, err := staker.GetValidation(val1.Node.Address())
	require.NoError(t, err)
	assert.Equal(t, validation.Status, builtin.StakerStatusExited)
}

func Test_ContractBalance_TransferBeforeFork(t *testing.T) {
	config := &hayabusa.Config{
		Nodes:             2,
		MaxBlockProposers: 2,
		// Important, staker gets deployed at ForkBlock. This allows us to send VET before the staker contract exists
		ForkBlock:         4,
		TransitionPeriod:  4,
		EpochLength:       4,
		CooldownPeriod:    4,
		MinStakingPeriod:  4,
		MidStakingPeriod:  12,
		HighStakingPeriod: 24,
		Name:              t.Name(),
		BlockInterval:     uint64(5),
	}

	// Network, client and staker setup
	network, err := hayabusa.NewNetwork(config, t.Context())
	require.NoError(t, err)
	require.NoError(t, network.Start())
	defer network.Stop()
	client := network.ThorClient()
	staker, err := builtin.NewStaker(network.ThorClient())
	require.NoError(t, err)

	// Send VET to the contract before the fork
	to := staker.Raw().Address()
	sendVETClauses := []*tx.Clause{tx.NewClause(to).WithValue(big.NewInt(10000))}
	testutil.SendClauses(t, hayabusa.AdditionalAccounts[0], sendVETClauses, client, testutil.TxContext(t))
	require.NoError(t, utils.WaitForFork(t.Context(), staker, config.ForkBlock))
	balance := big.NewInt(10000)
	assertStakerBalance(t, client, staker, balance)

	// Add the validators
	val1 := hayabusa.ValidatorAccounts[0]
	val2 := hayabusa.ValidatorAccounts[1]
	testutil.Send(t, val1.Endorser, staker.AddValidation(val1.Node.Address(), builtin.MinStake(), config.MinStakingPeriod))
	testutil.Send(t, val2.Endorser, staker.AddValidation(val2.Node.Address(), builtin.MinStake(), config.MinStakingPeriod))
	require.NoError(t, utils.WaitForPOS(t.Context(), staker, config.ForkBlock+config.TransitionPeriod))
	balance.Add(balance, builtin.MinStake())
	balance.Add(balance, builtin.MinStake())
	assertStakerBalance(t, client, staker, balance)

	// Exit 1 validator
	receipt := testutil.Send(t, val1.Endorser, staker.SignalExit(val1.Node.Address()))
	exitBlock := receipt.Meta.BlockNumber + (config.MinStakingPeriod - receipt.Meta.BlockNumber%config.MinStakingPeriod)
	assert.NoError(t, utils.NewTicker(staker.Raw().Client()).WaitForBlock(exitBlock))
	validation, err := staker.GetValidation(val1.Node.Address())
	require.NoError(t, err)
	assert.Equal(t, validation.Status, builtin.StakerStatusExited)

	// Wait for cooldown and withdraw
	cooldownBlock := exitBlock + config.CooldownPeriod
	assert.NoError(t, utils.NewTicker(staker.Raw().Client()).WaitForBlock(cooldownBlock))
	receipt = testutil.Send(t, val1.Endorser, staker.WithdrawStake(val1.Node.Address()))
	balance.Sub(balance, builtin.MinStake())
	assertStakerBalance(t, client, staker, balance)
}

func assertStakerBalance(t *testing.T, client *thorclient.Client, staker *builtin.Staker, expected *big.Int) {
	stakerBalance, err := client.Account(staker.Raw().Address())
	require.NoError(t, err)
	balanceVET := (*big.Int)(stakerBalance.Balance)
	t.Logf("✅ Staker balance: %s VET", balanceVET.String())
	assert.Equal(t, 0, balanceVET.Cmp(expected), "staker balance mismatch, expected %s, got %s", expected.String(), balanceVET.String())
}
