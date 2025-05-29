package main

import (
	"context"
	"fmt"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/logdb"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
	"time"
)

func addValidators(staker *builtin.Staker, config *hayabusa.Config) error {
	fmt.Println("")
	senders := bind.Senders{}
	for i := 0; i < int(config.MaxBlockProposers); i++ {
		acc := genesis.DevAccounts()[i]
		signer := (*bind.PrivateKeySigner)(acc.PrivateKey)
		// Add the validator
		sender := staker.AddValidator(signer, signer.Address(), builtin.MinStake(), config.MinStakingPeriod, true)
		senders.Add(sender)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("⏳ Add Validator transactions sent, waiting for confirmation...")
	_, _, err := senders.Send(ctx, testutil.TxOptions())
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
		fmt.Println(fmt.Sprintf("  - Validation %d:", i))
		fmt.Println(fmt.Sprintf("    📭 %s", event.Master))
		fmt.Println(fmt.Sprintf("    🆔 %s", event.ValidationID))
	}

	fmt.Println("✅ - All validators added successfully")

	return nil
}
