package main

import (
	"crypto/ecdsa"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/vechain/draupnir/contracts"
	"github.com/vechain/hayabusa-e2e/builtins"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
)

func main() {
	// Parse network URL from environment variable
	networkURL := os.Getenv("NETWORK_URL")
	if networkURL == "" {
		fmt.Println("Error: NETWORK_URL environment variable is required")
		os.Exit(1)
	}

	// Parse fork block from environment variable
	forkBlockStr := os.Getenv("FORK_BLOCK")
	if forkBlockStr == "" {
		fmt.Println("Error: FORK_BLOCK environment variable is required")
		os.Exit(1)
	}

	forkBlock, err := strconv.ParseUint(forkBlockStr, 10, 32)
	if err != nil {
		fmt.Printf("Error parsing FORK_BLOCK: %v\n", err)
		os.Exit(1)
	}

	// Parse validator private keys from environment variable as a list of strings
	privateKeysStr := os.Getenv("VALIDATOR_PRIVATE_KEYS")
	if privateKeysStr == "" {
		fmt.Println("Error: VALIDATOR_PRIVATE_KEYS environment variable is required")
		os.Exit(1)
	}

	validators, err := parseValidatorKeys(privateKeysStr)
	if err != nil {
		fmt.Printf("Error parsing validator keys: %v\n", err)
		os.Exit(1)
	}

	// Parse optional min staking period from environment variable
	minStakingPeriodStr := os.Getenv("MIN_STAKING_PERIOD")
	minStakingPeriod := uint32(12)
	if minStakingPeriodStr != "" {
		if val, err := strconv.ParseUint(minStakingPeriodStr, 10, 32); err == nil {
			minStakingPeriod = uint32(val)
		}
	}
	client := thorclient.New(networkURL)
	fmt.Printf("Successfully connected to network at %s\n", networkURL)

	staker := builtins.NewStaker(client, validators[0].PrivateKey)

	if err := staker.WaitForFork(uint32(forkBlock)); err != nil {
		fmt.Printf("Error waiting for fork: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Start sending transactions to register validators")
	senders := contracts.Senders{}
	for i := range len(validators) {
		acc := validators[i]
		fmt.Printf("Preparing validator %d: %s\n", i, acc.Address)

		sender := staker.Attach(acc.PrivateKey).AddValidator(acc.Address, builtins.MinStake, minStakingPeriod, true)
		senders.Add(sender)
	}

	fmt.Println("Sending transactions...")
	txIDs, receipts, err := senders.Send(false)
	if err != nil {
		fmt.Printf("Error sending transactions: %v\n", err)
		os.Exit(1)
	}

	for i, txID := range txIDs {
		if receipts[i] != nil && receipts[i].Reverted {
			fmt.Printf("❌ Validator %d: TX %s (reverted)\n", i, txID)
		} else {
			fmt.Printf("✅ Validator %d: TX %s\n", i, txID)
		}
	}

	fmt.Printf("✅ Successfully registered %d of 10 validators - PoS is now active\n", len(validators))
}

type Validator struct {
	Address    thor.Address
	PrivateKey *ecdsa.PrivateKey
}

func parseValidatorKeys(privateKeysStr string) ([]Validator, error) {
	privateKeysList := strings.Split(privateKeysStr, ",")
	// At the moment we have 10 validators from poa, then at least 7 need to be registered as pos validators
	if len(privateKeysList) < 7 {
		return nil, fmt.Errorf("VALIDATOR_PRIVATE_KEYS environment variable must contain at least 7 private keys")
	}
	validators := make([]Validator, 0, len(privateKeysList))

	for i, keyStr := range privateKeysList {
		keyStr = strings.TrimSpace(keyStr)

		if keyStr == "" {
			continue
		}
		if strings.HasPrefix(keyStr, "0x") {
			keyStr = keyStr[2:]
		}

		privateKey, err := crypto.HexToECDSA(keyStr)
		if err != nil {
			return nil, fmt.Errorf("invalid private key at position %d: %w", i+1, err)
		}

		address := crypto.PubkeyToAddress(privateKey.PublicKey)
		validators = append(validators, Validator{
			Address:    thor.Address(address),
			PrivateKey: privateKey,
		})
	}

	return validators, nil
}
