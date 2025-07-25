package testutil

import (
	"context"
	"github.com/vechain/thor/v2/thorclient"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/tx"
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

func RequireNonRevert(t *testing.T, sender *bind.MethodBuilder, receipt *api.Receipt) {
	// If the transaction is reverted, we try to call it at the block where it was mined
	// to get more information about the failure.
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
	RequireNonRevert(t, sender, receipt)
	return receipt
}

// TxManager enforces a sequence of transactions to be mined in the order they are sent.
// Useful when chains have just started and are having fork issues.
type TxManager struct {
	test   *testing.T
	client *thorclient.Client

	mu         sync.Mutex            // mutex to protect txSequence
	txSequence []thor.Bytes32        // sequence of transaction IDs
	pending    map[thor.Bytes32]bool // pending transactions to be flushed
}

func NewTxManager(t *testing.T, client *thorclient.Client) *TxManager {
	return &TxManager{test: t, client: client, txSequence: make([]thor.Bytes32, 0), pending: make(map[thor.Bytes32]bool)}
}

func (m *TxManager) Test() *testing.T {
	return m.test
}

// Send a transaction and wait for it to be mined.
func (m *TxManager) Send(signer bind.Signer, sender *bind.MethodBuilder) *api.Receipt {
	m.mu.Lock()
	defer m.mu.Unlock()

	options := TxOptions()
	if len(m.txSequence) > 0 {
		options.DependsOn = &m.txSequence[len(m.txSequence)-1]
	}

	receipt, _, err := sender.Send().
		WithOptions(options).
		WithSigner(signer).
		SubmitAndConfirm(TxContext(m.test))
	require.NoError(m.test, err, "failed to send transaction")
	RequireNonRevert(m.test, sender, receipt)
	m.txSequence = append(m.txSequence, receipt.Meta.TxID)
	return receipt
}

// SendAsync sends a transaction asynchronously and returns the transaction object.
func (m *TxManager) SendAsync(signer bind.Signer, sender *bind.MethodBuilder) *tx.Transaction {
	m.mu.Lock()
	defer m.mu.Unlock()

	options := TxOptions()
	if len(m.txSequence) > 0 {
		options.DependsOn = &m.txSequence[len(m.txSequence)-1]
	}

	transaction, err := sender.Send().
		WithOptions(options).
		WithSigner(signer).
		Submit()
	require.NoError(m.test, err, "failed to send transaction")
	m.txSequence = append(m.txSequence, transaction.ID())
	m.pending[transaction.ID()] = true

	return transaction
}

// Flush waits for all pending transactions to be mined and returns their receipts.
func (m *TxManager) Flush(ctx context.Context) []*api.Receipt {
	m.mu.Lock()
	defer m.mu.Unlock()

	receipts := make([]*api.Receipt, 0, len(m.pending))
	defer func() {
		for _, receipt := range receipts {
			delete(m.pending, receipt.Meta.TxID)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			m.test.Fatal("context cancelled while waiting for transactions to be mined")
		default:
			for txID := range m.pending {
				receipt, err := m.client.TransactionReceipt(&txID)
				if err != nil {
					time.Sleep(1 * time.Second) // wait before retrying
					continue
				}
				if receipt != nil {
					receipts = append(receipts, receipt)
				}
			}
		}
	}

	return receipts
}

// Copy the transaction sequence to a new TxManager.
func (m *TxManager) Copy(t *testing.T) *TxManager {
	m.mu.Lock()
	defer m.mu.Unlock()

	txs := make([]thor.Bytes32, len(m.txSequence))
	copy(txs, m.txSequence)
	return &TxManager{
		test:       t,
		txSequence: txs,
		client:     m.client,
		pending:    make(map[thor.Bytes32]bool),
	}
}
