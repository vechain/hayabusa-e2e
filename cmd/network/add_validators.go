package main

import (
	"context"
	"fmt"
	"time"

	"github.com/vechain/hayabusa-e2e/utils"

	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/logdb"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func addValidators(staker *builtin.Staker, config *hayabusa.Config) error {
	fmt.Println("")
	senders := &utils.Senders{}
	for i := range int(config.MaxBlockProposers) {
		acc := genesis.DevAccounts()[i]
		signer := (*bind.PrivateKeySigner)(acc.PrivateKey)
		// Add the validator
		sender := staker.AddValidator(signer.Address(), builtin.MinStake(), config.MinStakingPeriod, true).Send().WithSigner(signer).WithOptions(testutil.TxOptions())
		senders.Add(sender)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("⏳ Add Validator transactions sent, waiting for confirmation...")
	_, _, err := senders.Send(ctx)
	if err != nil {
		fmt.Println("  - Error sending transactions:", err)
		return err
	}

	events, err := staker.FilterValidatorQueued(nil, nil, logdb.ASC)
	if err != nil {
		fmt.Println("  - Error filtering events:", err)
		return err
	}

	fmt.Println("")
	for i, event := range events {
		fmt.Printf("  - Validation %d:", i)
		fmt.Printf("    📭 %s", event.Master)
		fmt.Printf("    🆔 %s", event.ValidationID)
	}

	fmt.Println("✅ - All validators added successfully")

	return nil
}
