package testutil

import (
	"context"
	"testing"
	"time"

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
	
