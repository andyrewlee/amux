package ptyio

import (
	"testing"
	"time"
)

func TestFlushGateDefersWhileOutputArriving(t *testing.T) {
	now := time.Now()
	st := &State{
		LastOutputAt:      now, // just received output, still in the quiet window
		FlushPendingSince: now,
	}
	delay, deferred := st.FlushGate(now, 30*time.Millisecond, 120*time.Millisecond)
	if !deferred {
		t.Fatalf("deferred = false, want true while still within quiet window")
	}
	if delay <= 0 {
		t.Fatalf("delay = %v, want > 0 for a re-arm", delay)
	}
	if !st.FlushScheduled {
		t.Fatalf("FlushScheduled = false, want true after a deferred gate")
	}
}

func TestFlushGateProceedsAfterQuiet(t *testing.T) {
	now := time.Now()
	st := &State{
		LastOutputAt:      now.Add(-50 * time.Millisecond), // quiet period elapsed
		FlushPendingSince: now.Add(-50 * time.Millisecond),
		FlushScheduled:    true,
	}
	_, deferred := st.FlushGate(now, 30*time.Millisecond, 120*time.Millisecond)
	if deferred {
		t.Fatalf("deferred = true, want false once the quiet period elapsed")
	}
	if st.FlushScheduled {
		t.Fatalf("FlushScheduled = true, want cleared when proceeding to flush")
	}
	if !st.FlushPendingSince.IsZero() {
		t.Fatalf("FlushPendingSince = %v, want zeroed when proceeding", st.FlushPendingSince)
	}
}

func TestRearmFlushDrained(t *testing.T) {
	st := &State{PendingOutput: []byte{}}
	drained := false
	now := time.Now()
	if st.RearmFlush(now, func() { drained = true }) {
		t.Fatalf("rearm = true, want false when the buffer is drained")
	}
	if !drained {
		t.Fatalf("onDrained not invoked when the buffer drained")
	}
	if st.FlushScheduled {
		t.Fatalf("FlushScheduled = true, want false when drained")
	}
}

func TestRearmFlushMoreBuffered(t *testing.T) {
	now := time.Now()
	st := &State{PendingOutput: []byte("more")}
	if !st.RearmFlush(now, func() { t.Fatalf("onDrained called while bytes remain") }) {
		t.Fatalf("rearm = false, want true when bytes remain buffered")
	}
	if !st.FlushScheduled {
		t.Fatalf("FlushScheduled = false, want true after a re-arm")
	}
	if !st.FlushPendingSince.Equal(now) {
		t.Fatalf("FlushPendingSince = %v, want %v", st.FlushPendingSince, now)
	}
}
