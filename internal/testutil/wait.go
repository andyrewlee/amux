// Package testutil provides small polling helpers shared across test packages,
// so individual tests don't re-derive deadline/poll loops (which tend to drift
// in interval and failure messaging).
package testutil

import (
	"cmp"
	"testing"
	"time"
)

// Eventually polls cond until it returns true or timeout elapses, failing the
// test (via t.Fatalf with the formatted message) on timeout.
func Eventually(t *testing.T, timeout, interval time.Duration, cond func() bool, msgf string, args ...any) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf(msgf, args...)
			return
		}
		time.Sleep(interval)
	}
}

// WaitForAtomic polls load until it reports a value >= want or timeout elapses,
// failing the test on timeout. load typically reads an atomic counter.
func WaitForAtomic[T cmp.Ordered](t *testing.T, load func() T, want T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if load() >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for value >= %v (got %v)", timeout, want, load())
			return
		}
		time.Sleep(time.Millisecond)
	}
}
