package common

import (
	"context"
	"time"
)

// TimedExit returns a context that will be cancelled after a timeout
func TimedExit(timeout time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	start := func() {
		time.Sleep(timeout)
		cancel()
	}
	return ctx, start
}
