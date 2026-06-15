// Package testutil provides small polling helpers shared across test packages,
// so individual tests don't re-derive deadline/poll loops (which tend to drift
// in interval and failure messaging).
package testutil

import (
	"cmp"
	"time"
)

// fataler is the minimal subset of *testing.T the polling helpers need.
// Splitting it out (instead of taking a concrete *testing.T) lets the
// timeout/failure branches be exercised by a fake recorder in tests without
// failing the real harness. *testing.T satisfies this interface.
type fataler interface {
	Helper()
	Fatalf(format string, args ...any)
}

// clock makes the polling loops deterministic under test: real callers get the
// wall clock and time.Sleep, while wait_test.go injects a fake clock so the
// failure paths run instantly and without flakiness.
type clock struct {
	now   func() time.Time
	sleep func(time.Duration)
}

func realClock() clock {
	return clock{now: time.Now, sleep: time.Sleep}
}

// Eventually polls cond until it returns true or timeout elapses, failing the
// test (via t.Fatalf with the formatted message) on timeout.
func Eventually(t fataler, timeout, interval time.Duration, cond func() bool, msgf string, args ...any) {
	t.Helper()
	if !eventually(realClock(), timeout, interval, cond) {
		t.Fatalf(msgf, args...)
	}
}

// eventually is the pure polling loop behind Eventually. It returns true if
// cond became true before the deadline and false on timeout, so the public
// wrapper decides how to fail.
func eventually(c clock, timeout, interval time.Duration, cond func() bool) (ok bool) {
	deadline := c.now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if c.now().After(deadline) {
			return false
		}
		c.sleep(interval)
	}
}

// Consistently polls check for the full duration at the given interval,
// failing the test the first time check returns a non-empty failure message.
// It is the inverse of Eventually: the condition must hold the whole time.
func Consistently(t fataler, duration, interval time.Duration, check func() string) {
	t.Helper()
	if msg := consistently(realClock(), duration, interval, check); msg != "" {
		t.Fatalf("%s", msg)
	}
}

// consistently is the pure polling loop behind Consistently. It returns the
// first non-empty failure message check produces, or "" if check held for the
// whole duration.
func consistently(c clock, duration, interval time.Duration, check func() string) string {
	deadline := c.now().Add(duration)
	for c.now().Before(deadline) {
		if msg := check(); msg != "" {
			return msg
		}
		c.sleep(interval)
	}
	return ""
}

// WaitForAtomic polls load until it reports a value >= want or timeout elapses,
// failing the test on timeout. load typically reads an atomic counter.
func WaitForAtomic[T cmp.Ordered](t fataler, load func() T, want T, timeout time.Duration) {
	t.Helper()
	if !eventually(realClock(), timeout, time.Millisecond, func() bool { return load() >= want }) {
		t.Fatalf("timed out after %s waiting for value >= %v (got %v)", timeout, want, load())
	}
}
