package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"syscall"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	fmt.Println(" 🛠️ Setting up test network...")
	config, client, err := setupTestNetwork(ctx, 3)
	if err != nil {
		return fmt.Errorf("failed to setup test network: %w", err)
	}

	staker, err := builtin.NewStaker(client)
	if err != nil {
		return fmt.Errorf("failed to create staker: %w", err)
	}

	validator1 := hayabusa.ValidatorAccounts[0]
	validator2 := hayabusa.ValidatorAccounts[1]
	validator3 := hayabusa.ValidatorAccounts[2]

	if err := utils.WaitForFork(staker, config.ForkBlock); err != nil {
		return fmt.Errorf("failed to wait for fork: %w", err)
	}

	stake := big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(3)) // 3x MinStake for each validator
	addReceipt1, err := SendTx(ctx, validator1.Endorser, staker.AddValidation(validator1.Node.Address(), stake, config.MinStakingPeriod))
	if err != nil {
		return fmt.Errorf("failed to send addValidation transaction: %w", err)
	}
	addReceipt2, err := SendTx(ctx, validator2.Endorser, staker.AddValidation(validator2.Node.Address(), stake, config.MinStakingPeriod))
	if err != nil {
		return fmt.Errorf("failed to send addValidation transaction: %w", err)
	}
	addReceipt3, err := SendTx(ctx, validator3.Endorser, staker.AddValidation(validator3.Node.Address(), stake, config.MinStakingPeriod))
	if err != nil {
		return fmt.Errorf("failed to send addValidation transaction: %w", err)
	}

	addr1 := validator1.Node.Address()
	addr2 := validator2.Node.Address()
	addr3 := validator3.Node.Address()

	if err := utils.WaitForPOS(staker, config.ForkBlock+config.TransitionPeriod); err != nil {
		return fmt.Errorf("failed to wait for POS: %w", err)
	}

	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"Name", "Gas MainnetUsed"})
	defer func() {
		tw.SortBy([]table.SortBy{{Name: "Name", Mode: table.Asc}})
		fmt.Println(tw.Render())
	}()

	checkCallResult := func(res *api.CallResult, err error, method string) error {
		if err != nil {
			return fmt.Errorf("%s failed: %w", method, err)
		}
		if res.Reverted {
			return fmt.Errorf("transaction reverted for %s", method)
		}
		tw.AppendRow(table.Row{method, res.GasUsed})
		return nil
	}

	// Read operations
	fmt.Println("📖 Executing read operations...")
	res, err := staker.Raw().Method("totalStake").Call().Execute()
	checkCallResult(res, err, "totalStake")

	res, err = staker.Raw().Method("queuedStake").Call().Execute()
	checkCallResult(res, err, "queuedStake")

	res, err = staker.Raw().Method("firstActive").Call().Execute()
	checkCallResult(res, err, "firstActive")

	res, err = staker.Raw().Method("firstQueued").Call().Execute()
	checkCallResult(res, err, "firstQueued")

	// Read validator additions
	tw.AppendRow(table.Row{"addValidator-1", addReceipt1.GasUsed})
	tw.AppendRow(table.Row{"addValidator-2", addReceipt2.GasUsed})
	tw.AppendRow(table.Row{"addValidator-3", addReceipt3.GasUsed})

	// Get validator info
	res, err = staker.Raw().Method("get", addr1).Call().Execute()
	checkCallResult(res, err, "get")
	res, err = staker.Raw().Method("next", addr1).Call().Execute()
	checkCallResult(res, err, "next")

	// Stake operations
	fmt.Println("💰 Executing stake operations...")
	receipt, err := SendTx(ctx, validator1.Endorser, staker.IncreaseStake(addr1, builtin.MinStake()))
	if err != nil {
		return fmt.Errorf("failed to send increaseStake transaction: %w", err)
	}
	tw.AppendRow(table.Row{"increaseStake", receipt.GasUsed})

	receipt, err = SendTx(ctx, validator1.Endorser, staker.DecreaseStake(addr1, builtin.MinStake()))
	if err != nil {
		return fmt.Errorf("failed to send decreaseStake transaction: %w", err)
	}
	tw.AppendRow(table.Row{"decreaseStake", receipt.GasUsed})

	receipt, err = SendTx(ctx, validator1.Endorser, staker.SignalExit(addr1))
	if err != nil {
		return fmt.Errorf("failed to send signalExit transaction: %w", err)
	}
	tw.AppendRow(table.Row{"updateAutoRenew", receipt.GasUsed})

	// Withdrawal operations
	fmt.Println("💸 Executing withdrawal operations...")

	receipt, err = SendTx(ctx, validator2.Endorser, staker.SignalExit(addr2))
	if err != nil {
		return fmt.Errorf("failed to send signalExit transaction: %w", err)
	}
	tw.AppendRow(table.Row{"updateAutoRenew", receipt.GasUsed})
	receipt, err = SendTx(ctx, validator2.Endorser, staker.WithdrawStake(addr2))
	if err != nil {
		return fmt.Errorf("failed to send withdrawStake transaction: %w", err)
	}
	tw.AppendRow(table.Row{"withdraw", receipt.GasUsed})

	// Delegation operations
	fmt.Println("🤝 Executing delegation operations...")

	delegationStake := big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(2))
	receipt, err = SendTx(ctx, hayabusa.Stargate, staker.AddDelegation(addr3, delegationStake, 100))
	if err != nil {
		return fmt.Errorf("failed to send addDelegation transaction: %w", err)
	}
	tw.AppendRow(table.Row{"addDelegation-1", receipt.GasUsed})

	delegationID := testutil.ReceiptToID(receipt)
	receipt, err = SendTx(ctx, hayabusa.Stargate, staker.SignalDelegationExit(delegationID))
	if err != nil {
		return fmt.Errorf("failed to send signalDelegationExit transaction: %w", err)
	}
	tw.AppendRow(table.Row{"updateDelegationAutoRenew", receipt.GasUsed})

	receipt, err = SendTx(ctx, hayabusa.Stargate, staker.AddDelegation(addr3, delegationStake, 100))
	if err != nil {
		return fmt.Errorf("add delegation 2: %w", err)
	}
	tw.AppendRow(table.Row{"addDelegation-2", receipt.GasUsed})

	delegationID = testutil.ReceiptToID(receipt)
	res, err = staker.Raw().Method("getDelegation", delegationID).Call().Execute()
	checkCallResult(res, err, "getDelegation")

	receipt, err = SendTx(ctx, hayabusa.Stargate, staker.AddDelegation(addr3, delegationStake, 100))
	if err != nil {
		return fmt.Errorf("add delegation 3: %w", err)
	}
	tw.AppendRow(table.Row{"addDelegation-3", receipt.GasUsed})

	delegationID = testutil.ReceiptToID(receipt)
	receipt, err = SendTx(ctx, hayabusa.Stargate, staker.WithdrawDelegation(delegationID))
	if err != nil {
		return fmt.Errorf("withdraw delegation: %w", err)
	}
	tw.AppendRow(table.Row{"withdrawDelegation", receipt.GasUsed})

	return nil
}

func setupTestNetwork(ctx context.Context, maxBlockProposers uint32) (*hayabusa.Config, *thorclient.Client, error) {
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
	}

	network, err := hayabusa.NewNetwork(config, ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to setup test network: %w", err)
	}
	if err := network.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start network: %w", err)
	}
	go func() {
		<-ctx.Done()
		network.Stop()
	}()
	return config, network.ThorClient(), nil
}

func SendTx(ctx context.Context, signer bind.Signer, sender *bind.MethodBuilder) (*api.Receipt, error) {
	receipt, _, err := sender.Send().
		WithOptions(testutil.TxOptions()).
		WithSigner(signer).
		SubmitAndConfirm(ctx)
	if err != nil {
		return nil, err
	}
	return receipt, nil
}
