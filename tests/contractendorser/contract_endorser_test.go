package contractendorser

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/tests/contractendorser/noreceive"
	"github.com/vechain/hayabusa-e2e/tests/contractendorser/receiverevert"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thor"
	bind2 "github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
	"github.com/vechain/thor/v2/tx"
)

func TestEndorser_Contract_NoReceive(t *testing.T) {
	newTest(t, noreceive.Bin, noreceive.ABI)
}

func TestEndorser_Contract_RevertReceive(t *testing.T) {
	newTest(t, receiverevert.Bin, receiverevert.ABI)
}

func newTest(t *testing.T, bytecode string, abiBytes []byte) {
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

	// Deploy the contract
	t.Log("ℹ️ deploying contract")
	deployClause := tx.NewClause(nil).WithData(hexutil.MustDecode("0x" + bytecode))
	receipt := testutil.SendClauses(t, hayabusa.AdditionalAccounts[0], []*tx.Clause{deployClause}, client, testutil.TxContext(t))
	contractAddress := receipt.Outputs[0].ContractAddress
	contract, err := bind2.NewContract(client, abiBytes, contractAddress)
	require.NoError(t, err)

	// Wait for POS
	require.NoError(t, utils.WaitForPOS(t.Context(), staker, config.ForkBlock+config.TransitionPeriod))
	stakerBalBefore, err := client.Account(staker.Raw().Address())
	require.NoError(t, err)

	// Add a validator via the contract
	validator := datagen.RandAddress()
	method := contract.Method("addValidation", validator, config.HighStakingPeriod).
		WithValue(builtin.MinStake())
	testutil.Send(t, hayabusa.AdditionalAccounts[0], method)

	// Call withdraw to see if it reverts
	method = contract.Method("withdraw")
	receipt, _, err = method.Send().
		WithOptions(testutil.TxOptions()).
		WithSigner(hayabusa.AdditionalAccounts[0]).
		SubmitAndConfirm(testutil.TxContext(t))
	require.NoError(t, err)

	t.Log("ℹ️ Performing check on test: " + t.Name())

	if receipt.Reverted {
		t.Log("✅ - Test is okay, transaction reverted as expected")
		return
	} else {
		t.Log("‼️ - withdraw tx did not revert")
	}

	transfers := len(receipt.Outputs[0].Transfers)
	events := len(receipt.Outputs[0].Events)
	t.Logf("ℹ️ - found %d transfers and %d events", transfers, events)

	withdrawSuccessEvent := contract.ABI().Events["WithdrawSuccess"]
	caughtErrorEvent := contract.ABI().Events["CaughtError"]
	caughtBytesEvent := contract.ABI().Events["CaughtBytes"]

	stakerWithdraw := staker.Raw().ABI().Events["ValidationWithdrawn"]

	for _, outputs := range receipt.Outputs {
		for _, event := range outputs.Events {
			if event.Address == *contractAddress {
				if event.Topics[0] == thor.Bytes32(withdrawSuccessEvent.Id()) {
					t.Log("⁉️withdraw was successful, but expected it to revert")
				}
				if event.Topics[0] == thor.Bytes32(caughtErrorEvent.Id()) {
					t.Log("ℹ️ - proxy endorser contract caught error")
				}
				if event.Topics[0] == thor.Bytes32(caughtBytesEvent.Id()) {
					t.Log("ℹ️ - proxy endorser contract caught bytes error")
				}
			}

			if event.Address == *staker.Raw().Address() {
				if event.Topics[0] == thor.Bytes32(stakerWithdraw.Id()) {
					t.Log("🤨 - Staker withdrawn event was emitted, but expected it to not be emitted")
				}
			}
		}
	}

	withdrawable, err := staker.GetWithdrawable(validator)
	require.NoError(t, err)
	if withdrawable.Cmp(big.NewInt(0)) != 0 {
		t.Log("‼️ (staker) validator stake is now 0, staker internal state updated")
	} else {
		t.Log("❔ validator balance was not updated, even though tx did not revert")
	}

	stakerBalAfter, err := client.Account(staker.Raw().Address())
	require.NoError(t, err)

	beforeInt := (*big.Int)(stakerBalBefore.Balance)
	stakerExpectedBal := new(big.Int).Add(beforeInt, builtin.MinStake())
	afterInt := (*big.Int)(stakerBalAfter.Balance)
	if stakerExpectedBal.Cmp(afterInt) == 0 {
		t.Log("❔ (staker) contract VET balance was not updated, still contains the extra stake of proxied endorser")
	} else {
		t.Log("‼️ (staker) contract VET balance was updated")
	}

	balanceProxy, err := client.Account(contractAddress)
	require.NoError(t, err)
	balProxy := (*big.Int)(balanceProxy.Balance)
	if balProxy.Cmp(big.NewInt(0)) == 0 {
		t.Log("⁉️ proxy contract did not receive any funds")
	} else {
		t.Log("❔proxy contract received funds, but should not have")
	}
}
