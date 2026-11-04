package activity

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// TestLivenessMachineFormallyChecked verifies the demotion-hysteresis table
// by exhaustive model checking: every state reachable, no single miss ever
// demotes, and a live observation always resets.
func TestLivenessMachineFormallyChecked(t *testing.T) {
	if err := LivenessCheck(); err != nil {
		t.Fatalf("liveness machine failed model checking: %v", err)
	}
}

func TestLivenessTrackerMatchesThreshold(t *testing.T) {
	l := NewLiveness()
	if l.Demoted() {
		t.Fatal("fresh tracker must not be demoted")
	}
	if l.ObserveMiss() {
		t.Fatal("first miss must not demote")
	}
	if l.Misses() != 1 {
		t.Fatalf("after one miss: %d", l.Misses())
	}
	if !l.ObserveMiss() {
		t.Fatal("second consecutive miss must demote")
	}
	if !l.Demoted() || l.Misses() != DemotionThreshold {
		t.Fatalf("after demotion: demoted=%v misses=%d", l.Demoted(), l.Misses())
	}
	// Misses past the threshold stay capped and demotion does not re-emit.
	if l.ObserveMiss() {
		t.Fatal("a third miss must not re-emit demotion")
	}
}

func TestLivenessLiveResetsAfterMiss(t *testing.T) {
	l := NewLiveness()
	l.ObserveMiss()
	l.observeLive()
	if l.Misses() != 0 || l.Demoted() {
		t.Fatalf("live observation must reset: misses=%d demoted=%v", l.Misses(), l.Demoted())
	}
	// A single miss after a reset must not demote (flicker tolerance).
	if l.ObserveMiss() {
		t.Fatal("post-reset single miss must not demote")
	}
}

func TestLivenessRevivesAfterDemotion(t *testing.T) {
	l := NewLiveness()
	l.ObserveMiss()
	l.ObserveMiss()
	if !l.Demoted() {
		t.Fatal("expected demotion after two misses")
	}
	l.observeLive()
	if l.Demoted() {
		t.Fatal("live observation must revive a demoted session")
	}
}

func TestStatusForMapsThroughSessionStatusMachine(t *testing.T) {
	demoted := NewLiveness()
	demoted.DemoteNow()
	for _, from := range []data.SessionStatus{data.SessionRunning, data.SessionDetached} {
		if got := demoted.StatusFor(from); got != data.SessionStopped {
			t.Errorf("demoted tracker must map %q to stopped, got %q", from, got)
		}
	}
	if got := demoted.StatusFor(data.SessionStopped); got != data.SessionStopped {
		t.Errorf("stopped must stay stopped, got %q", got)
	}
	live := NewLiveness()
	for _, from := range []data.SessionStatus{data.SessionRunning, data.SessionDetached, data.SessionStopped} {
		if got := live.StatusFor(from); got != from {
			t.Errorf("undemoted tracker must not touch %q, got %q", from, got)
		}
	}
}
