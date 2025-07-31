package testutil

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
)

func TxContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
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
