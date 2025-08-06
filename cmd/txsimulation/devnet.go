package main

import (
	"context"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
)

func startAgainstDevnet(ctx context.Context, devnet string) (*lifecycle.Engine, func()) {
	panic("Devnet support is not implemented yet")
}
