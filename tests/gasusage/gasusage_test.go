package gasusage

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func Test_Staker_GasUsage(t *testing.T) {
	config, client := setupTestNetwork(t, 3)
	staker, err := builtin.NewStaker(client)
	require.NoError(t, err)

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]

	require.NoError(t, utils.WaitForFork(staker, config.ForkBlock))

	stake := big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(3)) // 3x MinStake for each validator
	addReceipt1 := testutil.Send(t, validator1, staker.AddValidator(validator1.Address(), stake, config.MinStakingPeriod))
	addReceipt2 := testutil.Send(t, validator2, staker.AddValidator(validator2.Address(), stake, config.MinStakingPeriod))
	addReceipt3 := testutil.Send(t, validator3, staker.AddValidator(validator3.Address(), stake, config.MinStakingPeriod))

	addr1 := validator1.Address()
	addr2 := validator2.Address()
	addr3 := validator3.Address()

	require.NoError(t, utils.WaitForPOS(staker, config.ForkBlock+config.TransitionPeriod))

	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"Name", "Gas Used"})
	t.Cleanup(func() {
		tw.SortBy([]table.SortBy{{Name: "Name", Mode: table.Asc}})
		fmt.Println(tw.Render())
	})

	checkCallResult := func(t *testing.T, res *api.CallResult, err error, method string) {
		require.NoError(t, err)
		require.False(t, res.Reverted)
		tw.AppendRow(table.Row{method, res.GasUsed})
	}

	t.Run("totalStake", func(t *testing.T) {
		t.Parallel()
		totalStake, err := staker.Raw().Method("totalStake").Call().Execute()
		checkCallResult(t, totalStake, err, "totalStake")
	})

	t.Run("queuedStake", func(t *testing.T) {
		t.Parallel()
		queuedStake, err := staker.Raw().Method("queuedStake").Call().Execute()
		checkCallResult(t, queuedStake, err, "queuedStake")
	})

	t.Run("firstActive", func(t *testing.T) {
		t.Parallel()
		firstActive, err := staker.Raw().Method("firstActive").Call().Execute()
		checkCallResult(t, firstActive, err, "firstActive")
	})

	t.Run("firstQueued", func(t *testing.T) {
		t.Parallel()
		firstQueued, err := staker.Raw().Method("firstQueued").Call().Execute()
		checkCallResult(t, firstQueued, err, "firstQueued")
	})

	t.Run("addValidator", func(t *testing.T) {
		t.Parallel()
		tw.AppendRow(table.Row{"addValidator-1", addReceipt1.GasUsed})
		tw.AppendRow(table.Row{"addValidator-2", addReceipt2.GasUsed})
		tw.AppendRow(table.Row{"addValidator-3", addReceipt3.GasUsed})
	})

	t.Run("get", func(t *testing.T) {
		t.Parallel()
		res, err := staker.Raw().Method("get", addr1).Call().Execute()
		checkCallResult(t, res, err, "get")
	})

	t.Run("next", func(t *testing.T) {
		t.Parallel()
		res, err := staker.Raw().Method("next", addr1).Call().Execute()
		checkCallResult(t, res, err, "next")
	})

	t.Run("increase / autorenew / decrease ", func(t *testing.T) {
		t.Parallel()
		receipt := testutil.Send(t, validator1, staker.IncreaseStake(addr1, builtin.MinStake()))
		tw.AppendRow(table.Row{"increaseStake", receipt.GasUsed})
		receipt = testutil.Send(t, validator1, staker.DecreaseStake(addr1, builtin.MinStake()))
		tw.AppendRow(table.Row{"decreaseStake", receipt.GasUsed})
		receipt = testutil.Send(t, validator1, staker.SignalExit(addr1))
		tw.AppendRow(table.Row{"updateAutoRenew", receipt.GasUsed})
	})

	t.Run("updateAutoRenew / withdraw", func(t *testing.T) {
		t.Parallel()
		receipt := testutil.Send(t, validator2, staker.SignalExit(addr2))
		tw.AppendRow(table.Row{"updateAutoRenew", receipt.GasUsed})
		receipt = testutil.Send(t, validator2, staker.WithdrawStake(addr2))
		tw.AppendRow(table.Row{"withdraw", receipt.GasUsed})
	})

	delegationStake := big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(2))
	t.Run("addDelegation / updateDelegationAutoRenew", func(t *testing.T) {
		t.Parallel()
		receipt := testutil.Send(t, hayabusa.Stargate, staker.AddDelegation(addr3, delegationStake, 100))
		tw.AppendRow(table.Row{"addDelegation-1", receipt.GasUsed})
		delegationID := receipt.Outputs[0].Events[0].Topics[2]
		receipt = testutil.Send(t, hayabusa.Stargate, staker.SignalDelegationExit(delegationID))
		tw.AppendRow(table.Row{"updateDelegationAutoRenew", receipt.GasUsed})
	})

	t.Run("addDelegation-2 / get", func(t *testing.T) {
		t.Parallel()
		receipt := testutil.Send(t, hayabusa.Stargate, staker.AddDelegation(addr3, delegationStake, 100))
		tw.AppendRow(table.Row{"addDelegation-2", receipt.GasUsed})
		delegationID := receipt.Outputs[0].Events[0].Topics[2]
		res, err := staker.Raw().Method("getDelegation", delegationID).Call().Execute()
		checkCallResult(t, res, err, "getDelegation")
	})

	t.Run("addDelegation-3 / withdrawDelegation", func(t *testing.T) {
		t.Parallel()
		receipt := testutil.Send(t, hayabusa.Stargate, staker.AddDelegation(addr3, delegationStake, 100))
		tw.AppendRow(table.Row{"addDelegation-3", receipt.GasUsed})
		delegationID := receipt.Outputs[0].Events[0].Topics[2]
		receipt = testutil.Send(t, hayabusa.Stargate, staker.WithdrawDelegation(delegationID))
		tw.AppendRow(table.Row{"withdrawDelegation", receipt.GasUsed})
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
		MinStakingPeriod:  120,
		MidStakingPeriod:  240,
		HighStakingPeriod: 259200,
		Verbosity:         1,
		Name:              t.Name(),
	}

	network, err := hayabusa.NewNetwork(config, t.Context())
	require.NoError(t, err)
	t.Cleanup(network.Stop)
	require.NoError(t, network.Start())
	return config, network.ThorClient()
}
