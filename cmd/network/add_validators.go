package main

import (
	"context"
	"fmt"
	"time"

	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/logdb"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func addValidators(staker *builtin.Staker, config *hayabusa.Config) ([]builtin.ValidationQueuedEvent, error) {
	fmt.Println("")
	senders := &utils.Senders{}
	for i := range int(config.MaxBlockProposers) {
		acc := hayabusa.ValidatorAccounts[i]
		// Add the validator
		sender := staker.AddValidation(acc.Address(), builtin.MinStake(), config.MinStakingPeriod).Send().WithSigner(acc).WithOptions(testutil.TxOptions())
		senders.Add(sender)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("⏳ Add Validator transactions sent, waiting for confirmation...")
	_, _, err := senders.Send(ctx)
	if err != nil {
		fmt.Println("  - Error sending transactions:", err)
		return nil, err
	}

	events, err := staker.FilterValidatorQueued(nil, nil, logdb.ASC)
	if err != nil {
		fmt.Println("  - Error filtering events:", err)
		return nil, err
	}

	fmt.Println("✅ Validators added successfully")

	return events, nil
}
