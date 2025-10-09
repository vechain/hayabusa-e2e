package testutil

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
	"github.com/vechain/thor/v2/tx"
)

func SetDelegatorContract(t *testing.T, client *thorclient.Client, contractAddr thor.Address) thor.Address {
	params, err := builtin.NewParams(client)
	require.NoError(t, err)
	value := new(big.Int).SetBytes(contractAddr[:])
	receipt := Send(t, hayabusa.Executor, params.Set(thor.KeyDelegatorContractAddress, value))

	return receipt.Outputs[0].Events[0].Address
}

func DeployContract(t *testing.T, client *thorclient.Client, signer bind.Signer, bytecode string) thor.Address {
	genesis, err := client.Block("0")
	require.NoError(t, err)
	chainTag := genesis.ID[31]

	bytecode = strings.TrimSpace(bytecode)
	bytes, err := hexutil.Decode("0x" + bytecode)
	require.NoError(t, err)

	clause := tx.NewClause(nil).WithData(bytes)
	trx := new(tx.Builder).
		Clause(clause).
		Gas(40_000_000).
		Nonce(datagen.RandUint64()).
		ChainTag(chainTag).
		Expiration(10000).
		GasPriceCoef(255).
		Build()

	caller := signer.Address()
	inspection, err := client.InspectClauses(&api.BatchCallData{
		Caller: &caller,
		Clauses: api.Clauses{
			{
				Data:  "0x" + bytecode,
				Value: (*math.HexOrDecimal256)(trx.Clauses()[0].Value()),
				To:    trx.Clauses()[0].To(),
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(inspection))

	trx, err = signer.SignTransaction(trx)
	require.NoError(t, err)
	res, err := client.SendTransaction(trx)
	require.NoError(t, err)

	var receipt *api.Receipt
	for range 30 {
		receipt, err = client.TransactionReceipt(res.ID)
		if err == nil && receipt != nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	require.NotNil(t, receipt, "receipt not found")

	return receipt.Outputs[0].Events[0].Address
}
