package main

import (
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/vechain/thor/v2/api/transactions"
	"github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thorclient/httpclient"
	"github.com/vechain/thor/v2/tx"
)

func sendTx(chainTag byte, clause *tx.Clause, acc genesis.DevAccount, client *httpclient.Client) (*transactions.SendTxResult, error) {
	trx := new(tx.Builder).
		Clause(clause).
		BlockRef(tx.NewBlockRef(0)).
		Nonce(datagen.RandUint64()).
		ChainTag(chainTag).
		GasPriceCoef(255).
		Gas(1_000_000).
		Expiration(1_000).
		Build()

	trx = tx.MustSign(trx, acc.PrivateKey)

	rlpTx, err := trx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("unable to encode transaction - %w", err)
	}

	return client.SendTransaction(&transactions.RawTx{Raw: hexutil.Encode(rlpTx)})
}

func pollReceipt(sent *transactions.SendTxResult, client *httpclient.Client) (*transactions.Receipt, error) {
	var receipt *transactions.Receipt
	var err error
	for i := 0; i < 20; i++ {
		receipt, err = client.GetTransactionReceipt(sent.ID, "")
		if err == nil && receipt != nil {
			break
		}
		time.Sleep(time.Second)
	}
	return receipt, err
}

func sendAndWait(chainTag byte, clause *tx.Clause, acc genesis.DevAccount, client *httpclient.Client) (*transactions.Receipt, error) {
	sent, err := sendTx(chainTag, clause, acc, client)
	if err != nil {
		return nil, err
	}
	return pollReceipt(sent, client)
}
