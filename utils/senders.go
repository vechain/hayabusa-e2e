package utils

import (
	"context"
	"errors"
	"sync"

	"github.com/vechain/thor/v2/api"

	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/tx"
)

type Senders struct {
	senders []*bind.SendBuilder
	mu      sync.Mutex
}

// Add a new sender to the collection.
func (s *Senders) Add(sender *bind.SendBuilder) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.senders == nil {
		s.senders = make([]*bind.SendBuilder, 0)
	}
	s.senders = append(s.senders, sender)
}

// Send all transactions in parallel and returns the transactions and receipts.
func (s *Senders) Send(ctx context.Context) ([]*api.Receipt, []*tx.Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	txs := make([]*tx.Transaction, len(s.senders))
	receipts := make([]*api.Receipt, len(s.senders))
	errs := make([]error, len(s.senders))

	var wg sync.WaitGroup
	for i, sender := range s.senders {
		wg.Add(1)
		go func(i int, sender *bind.SendBuilder) {
			defer wg.Done()
			receipt, trx, err := sender.SubmitAndConfirm(ctx)
			if err != nil {
				errs[i] = err
				return
			}
			receipts[i] = receipt
			txs[i] = trx
		}(i, sender)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return receipts, txs, errors.Join(errs...)
		}
	}

	return receipts, txs, nil
}
