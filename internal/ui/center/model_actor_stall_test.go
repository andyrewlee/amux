package center

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/logging"
)

// TestTabActorStall_LogsOnceAndRecovers proves the actor-stall degradation (which
// silently routes input/scroll/writes through the slower synchronous direct-send
// path) logs exactly once per episode, and a heartbeat logs a single recovery.
func TestTabActorStall_LogsOnceAndRecovers(t *testing.T) {
	dir := t.TempDir()
	if err := logging.Initialize(dir, logging.LevelDebug); err != nil {
		t.Fatalf("logging init: %v", err)
	}
	defer logging.Close()

	m := &Model{}
	m.setTabActorReady()
	// Force the heartbeat older than the stall timeout.
	atomic.StoreInt64(&m.tabActorHeartbeat, time.Now().Add(-2*tabActorStallTimeout).UnixNano())

	for i := 0; i < 5; i++ {
		if m.isTabActorReady() {
			t.Fatal("expected actor to read as not-ready while stalled")
		}
	}

	// Recovery: a heartbeat flips ready back and logs exactly one recovery; a
	// second heartbeat must not re-log.
	m.noteTabActorHeartbeat()
	m.noteTabActorHeartbeat()

	logging.Close()
	if got := countLogLines(t, dir, "tab actor stalled"); got != 1 {
		t.Fatalf("expected exactly one stall Warn, got %d", got)
	}
	if got := countLogLines(t, dir, "tab actor recovered"); got != 1 {
		t.Fatalf("expected exactly one recovery Info, got %d", got)
	}
}

// TestSetTabActorReady_ClearsStallWithoutRecoveryLog proves a fresh (re)attach
// clears a prior stall episode silently — no spurious "recovered" line.
func TestSetTabActorReady_ClearsStallWithoutRecoveryLog(t *testing.T) {
	dir := t.TempDir()
	if err := logging.Initialize(dir, logging.LevelDebug); err != nil {
		t.Fatalf("logging init: %v", err)
	}
	defer logging.Close()

	m := &Model{}
	m.setTabActorReady()
	atomic.StoreInt64(&m.tabActorHeartbeat, time.Now().Add(-2*tabActorStallTimeout).UnixNano())
	if m.isTabActorReady() {
		t.Fatal("expected stalled actor to read not-ready")
	}

	// A fresh attach resets readiness and the stall flag, but must not log a
	// heartbeat-style recovery.
	m.setTabActorReady()
	m.noteTabActorHeartbeat()

	logging.Close()
	if got := countLogLines(t, dir, "tab actor recovered"); got != 0 {
		t.Fatalf("expected no recovery Info after a fresh attach cleared the stall, got %d", got)
	}
}
