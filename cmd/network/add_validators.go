package main

import (
	"fmt"
	"github.com/vechain/draupnir/contracts"
	"github.com/vechain/hayabusa-e2e/builtins"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/thor/v2/genesis"
)

func addValidators(staker *builtins.Staker, config *hayabusa.Config) error {
	fmt.Println("")
	senders := contracts.Senders{}
	for i := 0; i < int(config.MaxBlockProposers); i++ {
		acc := genesis.DevAccounts()[i]
		// Add the validator
		sender := staker.Attach(acc.PrivateKey).AddValidator(acc.Address, builtins.MinStake, config.MinStakingPeriod, true)
		senders.Add(sender)
	}

	fmt.Println("⏳ Add Validator transactions sent, waiting for confirmation...")
	_, _, err := senders.Send(false)
	if err != nil {
		fmt.Println("  - Error sending transactions:", err)
		return err
	}

	events, err := staker.FilterValidatorQueued(0, 1000)
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
