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

	staker := builtins.NewStaker(client, validators[0].PrivateKey)
	_, first, _ := staker.FirstActive()
	if !first.IsZero() {
		fmt.Println("✅ PoS is already active, exiting")
		os.Exit(0)
	}

	authEntries, err := fetchAuthorities(client)
	if err != nil {
		fmt.Printf("Error fetching authorities: %v\n", err)
		os.Exit(1)
	}

	if err := staker.WaitForFork(uint32(forkBlock)); err != nil {
		fmt.Printf("Error waiting for fork: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Start sending transactions to register validators")
	senders := contracts.Senders{}
	for i := range len(validators) {
		acc := validators[i]
		node, ok := authEntries[acc.Address]
		if !ok {
			fmt.Printf("Validator %d (%s) is not an authority node, skipping registration\n", i, acc.Address)
			continue
		}
		fmt.Printf("Preparing validator %d: %s\n", i, acc.Address)

		sender := staker.Attach(acc.PrivateKey).AddValidator(node.master, builtins.MinStake, minStakingPeriod, true)
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

	best, err := client.Block("0")
	if err != nil {
		fmt.Printf("Error fetching best block: %v\n", err)
		os.Exit(1)
	}

	err = staker.WaitForPOS(best.Number + 180)
	if err != nil {
		fmt.Printf("Error waiting for PoS activation: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ PoS is now active, all validators registered successfully")
}

type Validator struct {
	Address    thor.Address
	PrivateKey *ecdsa.PrivateKey
}

func parseValidatorKeys(privateKeysStr string) ([]Validator, error) {
	privateKeysList := strings.Split(privateKeysStr, ",")
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

type nodeEntry struct {
	master thor.Address
	entry  *builtins.AuthorityNode
}

// fetchAuthorities retrieves all authority nodes from the blockchain and returns them as a map.
// The map key is the endorsor address.
func fetchAuthorities(client *thorclient.Client) (map[thor.Address]nodeEntry, error) {
	tempCaller, err := crypto.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate temporary key: %w", err)
	}
	contract := builtins.NewAuthority(client, tempCaller)

	prev, err := contract.First()
	if err != nil {
		return nil, fmt.Errorf("failed to get first authority: %w", err)
	}

	entries := make(map[thor.Address]nodeEntry)

	for !prev.IsZero() {
		node, err := contract.Get(prev)
		if err != nil {
			return nil, fmt.Errorf("failed to get authority node %s: %w", prev.String(), err)
		}

		entries[node.Endorsor] = nodeEntry{
			master: prev,
			entry:  node,
		}

		prev, err = contract.Next(prev)
		if err != nil {
			return nil, fmt.Errorf("failed to get next authority: %w", err)
		}
	}

	return entries, nil
}
