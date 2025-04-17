package common

import (
	"crypto/ecdsa"
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"math/big"
)

type Account struct {
	Address          *thor.Address
	PrivateKey       *ecdsa.PrivateKey
	PrivateKeyString string
}

func NewAccount(pkString string) *Account {
	pk, err := crypto.HexToECDSA(pkString)
	if err != nil {
		panic(err)
	}
	addr := thor.Address(crypto.PubkeyToAddress(pk.PublicKey))
	return &Account{
		Address:          &addr,
		PrivateKey:       pk,
		PrivateKeyString: pkString,
	}
}

// ValidateAccountBalance Checks an account’s balance/energy via RPC)
func ValidateAccountBalance(vcClient *thorclient.Client, address *thor.Address) error {
	account, err := vcClient.Account(address)
	if err != nil {
		return err
	}
	vet := big.Int(account.Balance)
	vtho := big.Int(account.Energy)
	if vet.Cmp(big.NewInt(0)) != 1 {
		return fmt.Errorf("0 VET balance")
	}
	if vtho.Cmp(big.NewInt(0)) != 1 {
		return fmt.Errorf("0 VTHO balance")
	}

	return nil
}
