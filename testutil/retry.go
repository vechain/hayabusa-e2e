package testutil

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// StakerStatusUnknownError is returned when the validator status is unknown
// This is usually due to a fork, and we need to retry the test
type StakerStatusUnknownError struct {
	ValidationID string
}

func (e StakerStatusUnknownError) Error() string {
	return fmt.Sprintf("validator status is unknown for validation ID: %s", e.ValidationID)
}

func IsStakerStatusUnknownError(err error) bool {
	var statusErr StakerStatusUnknownError
	return errors.As(err, &statusErr)
}

func RetryOnStakerStatusUnknown(t *testing.T, maxRetries int, fn func() error) {
	var lastErr error
	delay := 2 * time.Second
	backoff := 1.5

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			t.Logf("🔄 Retrying due to StakerStatusUnknown (attempt %d/%d, delay: %v)", attempt, maxRetries, delay)
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * backoff)
		}

		if err := fn(); err != nil {
			lastErr = err

			if IsStakerStatusUnknownError(err) {
				t.Logf("❌ Attempt %d failed with StakerStatusUnknown: %v", attempt, err)
				continue
			} else {
				t.Logf("❌ Attempt %d failed with non-retryable error: %v", attempt, err)
				t.Fatalf("Non-retryable error: %v", err)
			}
		}

		if attempt > 1 {
			t.Logf("✅ Operation successful on attempt %d", attempt)
		}
		return
	}

	t.Fatalf("❌ Operation failed after %d attempts. Last error: %v", maxRetries, lastErr)
}

func RunTestWithRetry(t *testing.T, maxRetries int, testFunc func() error) {
	var lastErr error
	delay := 3 * time.Second
	backoff := 2.0

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			t.Logf("🔄 Retrying complete test from the beginning (attempt %d/%d, delay: %v)", attempt, maxRetries, delay)
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * backoff)
		}

		if err := testFunc(); err != nil {
			lastErr = err
			t.Logf("❌ Attempt %d of complete test failed: %v", attempt, err)
			continue
		}

		if attempt > 1 {
			t.Logf("✅ Complete test successful on attempt %d", attempt)
		}
		return
	}

	t.Fatalf("❌ Complete test failed after %d attempts. Last error: %v", maxRetries, lastErr)
}

func RunFlakyTest(t *testing.T, testFunc func() error) {
	RunTestWithRetry(t, 3, testFunc)
}
