package common

import (
	"os"
	"strings"
	"testing"
)

// CheckSkip ensures that only tests explicitly named in RUNTESTS are allowed to run.
//
// Usage examples:
//
//  1. RUNTESTS="TestReplay" go test -v ./...
//     --> Only a test named exactly "TestReplay" will run. Everything else is skipped.
//
//  2. RUNTESTS="TestReplay,TestTxBombard" go test -v ./...
//     --> Only tests named exactly "TestReplay" or "TestTxBombard" will run.
//
//  3. RUNTESTS="" go test -v ./...
//     --> All tests are skipped, because no test names are allowed.
//
// Implementation details:
//   - If RUNTESTS is empty, we skip all tests by default.
//   - Otherwise, we split RUNTESTS on commas to allow multiple test names.
//   - If t.Name() doesn't match any of the allowed names exactly, we skip.
//
// Adjust as needed if you prefer a different matching strategy (e.g., substring).
func CheckSkip(t *testing.T) {
	runTests := os.Getenv("RUNTESTS")

	// If RUNTESTS is empty, skip everything.
	if runTests == "" {
		t.Skipf("Skipping %s because RUNTESTS is empty, and only listed tests should run.", t.Name())
		return
	}

	testName := t.Name()
	// Split RUNTESTS by commas to support multiple test names.
	allowedTests := strings.Split(runTests, ",")

	// Trim spaces and compare exactly to t.Name().
	for _, allowedTest := range allowedTests {
		if strings.TrimSpace(allowedTest) == testName {
			// Found a match, so do not skip.
			return
		}
	}

	// If no match found, skip.
	t.Skipf("Skipping %s because it was not listed in RUNTESTS (%s).",
		testName, runTests)
}
