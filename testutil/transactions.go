package testutil

import (
	"context"
	"errors"
	"math"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/httpclient"
	"github.com/vechain/thor/v2/tx"
)

func TxContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	t.Cleanup(cancel)
	return ctx
}

func TxOptions() *bind.TxOptions {
	gas := uint64(10_000_000)
	return &bind.TxOptions{
		Gas: &gas,
	}
}

func DebugRevert(t *testing.T, receipt *api.Receipt, sender *bind.MethodBuilder) {
	if receipt == nil {
		require.Fail(t, "receipt is nil")
		return
	}
	if receipt.Reverted {
		_, err := sender.Call().
			AtRevision(receipt.Meta.BlockID.String()).
			Caller(&receipt.Meta.TxOrigin).
			Execute()
		if err != nil {
			require.Fail(t, "transaction reverted", err)
		} else {
			require.Fail(t, "transaction reverted for unknown reason")
		}
	}
}

// Send a transaction with the method, signer and default transaction options/ context.
// It asserts that the transaction is sent successfully and not reverted.
func Send(t *testing.T, signer bind.Signer, sender *bind.MethodBuilder) *api.Receipt {
	receipt, _, err := sender.Send().
		WithOptions(TxOptions()).
		WithSigner(signer).
		SubmitAndConfirm(TxContext(t))
	assert.NoError(t, err, "failed to send transaction")
	DebugRevert(t, receipt, sender)
	return receipt
}

// TxSequence is a helper to send transactions in sequence, where each transaction depends on the previous one.
// It is useful for testing scenarios where transactions must be sent in a specific order.
type TxSequence struct {
	t   *testing.T
	txs []thor.Bytes32
	mu  sync.Mutex
}

func NewTxSequence(t *testing.T) *TxSequence {
	return &TxSequence{
		txs: make([]thor.Bytes32, 0),
		t:   t,
	}
}

func (s *TxSequence) Send(signer bind.Signer, sender *bind.MethodBuilder) *api.Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()

	options := TxOptions()
	if len(s.txs) > 0 {
		options.DependsOn = &s.txs[len(s.txs)-1]
	}

	receipt, _, err := sender.Send().
		WithOptions(options).
		WithSigner(signer).
		SubmitAndConfirm(TxContext(s.t))
	assert.NoError(s.t, err, "failed to send transaction")
	DebugRevert(s.t, receipt, sender)

	s.txs = append(s.txs, receipt.Meta.TxID)
	return receipt
}

// Receipts
func ReceiptToID(receipt *api.Receipt) *big.Int {
	// 0 is the event, 1 is the validation ID
	return new(big.Int).SetBytes(receipt.Outputs[0].Events[0].Topics[2][:])
}

func SendClauses(
	t *testing.T,
	signer bind.Signer,
	clauses []*tx.Clause,
	client *thorclient.Client,
	ctx context.Context,
) *api.Receipt {
	chainTag, err := client.ChainTag()
	require.NoError(t, err)

	trx := tx.NewBuilder(tx.TypeLegacy).
		Clauses(clauses).
		ChainTag(chainTag).
		Gas(10_000_000).
		Nonce(datagen.RandUint64()).
		Expiration(math.MaxUint32).
		Build()

	trx, err = signer.SignTransaction(trx)
	require.NoError(t, err, "failed to sign transaction")

	send, err := client.SendTransaction(trx)
	require.NoError(t, err, "failed to send transaction")

	ticker := time.NewTicker(100 * time.Millisecond)

	for {
		select {
		case <-ctx.Done():
			require.Fail(t, "context done before transaction is mined")
			return nil
		case <-ticker.C:
			receipt, err := client.TransactionReceipt(send.ID)
			if errors.Is(err, httpclient.ErrNotFound) {
				continue
			}
			require.NoError(t, err, "failed to get transaction receipt")
			require.False(t, receipt.Reverted, "transaction reverted")
			return receipt
		}
	}
}
