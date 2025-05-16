package main

import (
	"github.com/vechain/draupnir/datagen"
	"github.com/vechain/thor/v2/api/transactions"
	"github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/tx"
	"time"
)

func sendTx(chainTag byte, clause *tx.Clause, acc genesis.DevAccount, client *thorclient.Client) (*transactions.SendTxResult, error) {
	trx := new(tx.Builder).
		Clause(clause).
		BlockRef(tx.NewBlockRef(0)).
		Nonce(datagen.RandUInt64()).
		ChainTag(chainTag).
		GasPriceCoef(255).
		Gas(1_000_000).
		Expiration(1_000).
		Build()

	trx = tx.MustSign(trx, acc.PrivateKey)

	return client.SendTransaction(trx)
}

func pollReceipt(sent *transactions.SendTxResult, client *thorclient.Client) (*transactions.Receipt, error) {
	var receipt *transactions.Receipt
	var err error
	for i := 0; i < 20; i++ {
		receipt, err = client.TransactionReceipt(sent.ID)
		if err == nil && receipt != nil {
			break
		}
		time.Sleep(time.Second)
	}
	return receipt, err
}

func sendAndWait(chainTag byte, clause *tx.Clause, acc genesis.DevAccount, client *thorclient.Client) (*transactions.Receipt, error) {
	sent, err := sendTx(chainTag, clause, acc, client)
	if err != nil {
		return nil, err
	}
	return pollReceipt(sent, client)
}
