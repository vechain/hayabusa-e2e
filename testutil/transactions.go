package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thorclient/bind"
)

func TxContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TxOptions() *bind.TxOptions {
	gas := uint64(10_000_000)
	return &bind.TxOptions{
		Gas: &gas,
	}
}

// Send a transaction with the method, signer and default transaction options/ context.
// It asserts that the transaction is sent successfully and not reverted.
func Send(t *testing.T, signer bind.Signer, sender bind.MethodBuilder) *api.Receipt {
	receipt, _, err := sender.Send().
		WithOptions(TxOptions()).
		WithSigner(signer).
		SubmitAndConfirm(TxContext(t))
	assert.NoError(t, err, "failed to send transaction")
	assert.False(t, receipt.Reverted, "transaction reverted")
	return receipt
}
