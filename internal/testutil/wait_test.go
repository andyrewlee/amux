package testutil

import (
	"fmt"
	"testing"
	"time"
)

// fakeFataler records Fatalf calls instead of failing a real *testing.T, so the
// timeout/failure branches of the polling helpers can be asserted without
// failing the harness running this test.
type fakeFataler struct {
	helperCalls int
	fatalCalls  int
	lastMsg     string
}

func (f *fakeFataler) Helper() { f.helperCalls++ }

func (f *fakeFataler) Fatalf(format string, args ...any) {
	f.fatalCalls++
	f.lastMsg = fmt.Sprintf(format, args...)
}

// fakeClock advances by `step` on each now() call and never actually sleeps, so
// the failure paths complete instantly and deterministically.
type fakeClock struct {
	cur    time.Time
	step   time.Duration
	sleeps int
}

func (fc *fakeClock) toClock() clock {
	return clock{
		now: func() time.Time {
			t := fc.cur
			fc.cur = fc.cur.Add(fc.step)
			return t
		},
		sleep: func(time.Duration) { fc.sleeps++ },
	}
}

func TestEventuallyTrueImmediately(t *testing.T) {
	f := &fakeFataler{}
	calls := 0
	Eventually(f, time.Second, time.Millisecond, func() bool {
		calls++
		return true
	}, "should not fail")

	if f.fatalCalls != 0 {
		t.Fatalf("expected no Fatalf when cond is true immediately, got %d (msg=%q)", f.fatalCalls, f.lastMsg)
	}
	if calls != 1 {
		t.Fatalf("expected cond evaluated exactly once, got %d", calls)
	}
	if f.helperCalls != 1 {
		t.Fatalf("expected Helper called once, got %d", f.helperCalls)
	}
}

func TestEventuallyNeverTrueFailsOnceWithMessage(t *testing.T) {
	f := &fakeFataler{}
	// Short deadline keeps this near-instant: the public Eventually uses the real
	// clock, and the condition never becomes true, so the call sleeps until the
	// timeout. A few ms is enough to exercise the formatted-message timeout path.
	Eventually(f, 5*time.Millisecond, time.Millisecond, func() bool { return false }, "timed out waiting for %s", "thing")

	if f.fatalCalls != 1 {
		t.Fatalf("expected exactly one Fatalf on timeout, got %d", f.fatalCalls)
	}
	if want := "timed out waiting for thing"; f.lastMsg != want {
		t.Fatalf("expected formatted message %q, got %q", want, f.lastMsg)
	}
}

func TestEventuallyPureTimesOut(t *testing.T) {
	// now() advances 10ms per call against a 25ms timeout, so the loop must
	// give up rather than spin forever; the fake clock guarantees termination.
	fc := &fakeClock{cur: time.Unix(0, 0), step: 10 * time.Millisecond}
	ok := eventually(fc.toClock(), 25*time.Millisecond, time.Millisecond, func() bool { return false })
	if ok {
		t.Fatal("expected eventually to report timeout (false) when cond never holds")
	}
	if fc.sleeps == 0 {
		t.Fatal("expected the loop to sleep between polls before timing out")
	}
}

func TestEventuallyPureSucceedsBeforeDeadline(t *testing.T) {
	fc := &fakeClock{cur: time.Unix(0, 0), step: time.Millisecond}
	hit := 0
	ok := eventually(fc.toClock(), time.Second, time.Millisecond, func() bool {
		hit++
		return hit >= 3
	})
	if !ok {
		t.Fatal("expected eventually to report success when cond becomes true before deadline")
	}
}

func TestConsistentlyFailsOnFirstNonEmptyCheck(t *testing.T) {
	f := &fakeFataler{}
	checks := 0
	Consistently(f, time.Second, time.Millisecond, func() string {
		checks++
		return "broke immediately"
	})

	if f.fatalCalls != 1 {
		t.Fatalf("expected exactly one Fatalf on first non-empty check, got %d", f.fatalCalls)
	}
	if f.lastMsg != "broke immediately" {
		t.Fatalf("expected message %q, got %q", "broke immediately", f.lastMsg)
	}
	if checks != 1 {
		t.Fatalf("expected check evaluated exactly once before failing, got %d", checks)
	}
}

func TestConsistentlyHoldsForWholeDuration(t *testing.T) {
	f := &fakeFataler{}
	Consistently(f, 5*time.Millisecond, time.Millisecond, func() string { return "" })
	if f.fatalCalls != 0 {
		t.Fatalf("expected no Fatalf when check stays empty, got %d (msg=%q)", f.fatalCalls, f.lastMsg)
	}
}

func TestConsistentlyPureReturnsFirstFailure(t *testing.T) {
	fc := &fakeClock{cur: time.Unix(0, 0), step: time.Millisecond}
	calls := 0
	msg := consistently(fc.toClock(), time.Second, time.Millisecond, func() string {
		calls++
		if calls == 2 {
			return "failed on second check"
		}
		return ""
	})
	if msg != "failed on second check" {
		t.Fatalf("expected first non-empty failure message, got %q", msg)
	}
}

func TestWaitForAtomicReachesWant(t *testing.T) {
	f := &fakeFataler{}
	v := int64(0)
	WaitForAtomic(f, func() int64 { v++; return v }, 3, time.Second)
	if f.fatalCalls != 0 {
		t.Fatalf("expected no Fatalf once load reaches want, got %d (msg=%q)", f.fatalCalls, f.lastMsg)
	}
}

func TestWaitForAtomicTimesOut(t *testing.T) {
	f := &fakeFataler{}
	WaitForAtomic(f, func() int64 { return 0 }, 3, time.Millisecond)
	if f.fatalCalls != 1 {
		t.Fatalf("expected exactly one Fatalf on timeout, got %d", f.fatalCalls)
	}
	if f.lastMsg == "" {
		t.Fatal("expected a non-empty timeout message")
	}
}
