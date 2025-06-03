package main

import (
	"fmt"
	big2 "math/big"

	"github.com/vechain/thor/v2/thorclient"

	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/thor/v2/api/events"
	"github.com/vechain/thor/v2/builtin"
	"github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/tx"
)

func setStargateAddr(client *thorclient.Client, stargate thor.Address) error {
	executor := genesis.DevAccounts()[0]
	chainTag, err := client.ChainTag()
	if err != nil {
		return fmt.Errorf("failed to get chain tag: %w", err)
	}

	// set params call data
	paramsMethod, ok := builtin.Params.ABI.MethodByName("set")
	if !ok {
		return fmt.Errorf("set method not found in params ABI")
	}
	big := big2.NewInt(0).SetBytes(stargate.Bytes())
	paramsEncoded, err := paramsMethod.EncodeInput(thor.MustParseBytes32(hayabusa.ParamsStargateKey), big)
	if err != nil {
		return fmt.Errorf("failed to encode params set input: %w", err)
	}

	// set executor call data
	executorMethod, ok := builtin.Executor.ABI.MethodByName("propose")
	if !ok {
		return fmt.Errorf("propose method not found in executor ABI")
	}
	executorEncoded, err := executorMethod.EncodeInput(builtin.Params.Address, paramsEncoded)
	if err != nil {
		return fmt.Errorf("failed to encode executor propose input: %w", err)
	}

	// send the propose transaction
	executorAddr := builtin.Executor.Address
	clause := tx.NewClause(&executorAddr).WithData(executorEncoded)

	proposalReceipt, err := sendAndWait(chainTag, clause, executor, client)
	if err != nil {
		return fmt.Errorf("failed to send transaction: %w", err)
	}
	if proposalReceipt.Reverted {
		return fmt.Errorf("transaction reverted: %s", proposalReceipt.Meta.TxID)
	}

	fmt.Printf("(1/3) proposal receipt found: %s\n", proposalReceipt.Meta.TxID)

	// send the approval tx
	block := uint64(proposalReceipt.Meta.BlockNumber)
	events, err := client.FilterEvents(&events.EventFilter{
		Range: &events.Range{
			From: &block,
			To:   &block,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to filter events: %w", err)
	}
	proposalID := events[0].Topics[1]
	approvalMethod, ok := builtin.Executor.ABI.MethodByName("approve")
	if !ok {
		return fmt.Errorf("approve method not found in executor ABI")
	}
	approvalEncoded, err := approvalMethod.EncodeInput(proposalID)
	if err != nil {
		return fmt.Errorf("failed to encode approval input: %w", err)
	}
	approvalClause := tx.NewClause(&executorAddr).WithData(approvalEncoded)
	approvalReceipt, err := sendAndWait(chainTag, approvalClause, executor, client)
	if err != nil {
		return fmt.Errorf("failed to send transaction: %w", err)
	}
	if approvalReceipt.Reverted {
		return fmt.Errorf("transaction reverted: %s", approvalReceipt.Meta.TxID)
	}

	fmt.Printf("(2/3) approval receipt found: %s\n", approvalReceipt.Meta.TxID)

	// send the execute tx
	executeMethod, ok := builtin.Executor.ABI.MethodByName("execute")
	if !ok {
		return fmt.Errorf("execute method not found in executor ABI")
	}
	executeEncoded, err := executeMethod.EncodeInput(proposalID)
	if err != nil {
		return fmt.Errorf("failed to encode execute input: %w", err)
	}
	executeClause := tx.NewClause(&executorAddr).WithData(executeEncoded)
	executeReceipt, err := sendAndWait(chainTag, executeClause, executor, client)
	if err != nil {
		return fmt.Errorf("failed to send transaction: %w", err)
	}
	if executeReceipt.Reverted {
		return fmt.Errorf("transaction reverted: %s", executeReceipt.Meta.TxID)
	}

	fmt.Printf("(3/3) execute receipt found: %s\n", executeReceipt.Meta.TxID)

	return nil
}
