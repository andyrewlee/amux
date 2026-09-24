package center

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/tmux"
)

// TestActivityTagFlush_BatchesAllMarkedSessions is the plan-044 invariant:
// N tabs marking activity in the same window produce ONE batched tag write,
// not N subprocesses.
func TestActivityTagFlush_BatchesAllMarkedSessions(t *testing.T) {
	m := New(nil)
	m.markActivityTagForFlush("amux-sess-a")
	m.markActivityTagForFlush("amux-sess-b")
	m.markActivityTagForFlush("amux-sess-a") // duplicate mark collapses

	sessions := m.drainActivityTags()
	if len(sessions) != 2 {
		t.Fatalf("drained %v, want [a b]", sessions)
	}
	if sessions[0] != "amux-sess-a" || sessions[1] != "amux-sess-b" {
		t.Fatalf("drain must sort deterministically, got %v", sessions)
	}
	// Drained set is empty; scheduler re-armed.
	if again := m.drainActivityTags(); len(again) != 0 {
		t.Fatalf("second drain must be empty, got %v", again)
	}
}

// TestActivityTagFlush_FiresWithinThrottleWindow: a mark schedules a flush
// message that arrives within ~one throttle window.
func TestActivityTagFlush_FiresWithinThrottleWindow(t *testing.T) {
	m := New(nil)
	msgs := make(chan tea.Msg, 4)
	m.SetMsgSink(func(msg tea.Msg) { msgs <- msg })

	m.markActivityTagForFlush("amux-sess-x")
	select {
	case msg := <-msgs:
		if _, ok := msg.(activityTagFlushDue); !ok {
			t.Fatalf("expected activityTagFlushDue, got %T", msg)
		}
	case <-time.After(activityTagThrottle + 500*time.Millisecond):
		t.Fatal("flush message did not arrive within the throttle window")
	}

	// A second mark during the pending window does not schedule again.
	m.markActivityTagForFlush("amux-sess-y")
	select {
	case msg := <-msgs:
		t.Fatalf("second mark must not reschedule, got %T", msg)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestActivityTagFlush_UpdateWritesOneBatch drives the Update path end to
// end through the seam: the flush message produces exactly one
// SetSessionTagValueForSessions call carrying every marked session.
func TestActivityTagFlush_UpdateWritesOneBatch(t *testing.T) {
	m := New(nil)

	var (
		mu       sync.Mutex
		calls    [][]string
		tagKey   string
		tagValue string
	)
	prev := setSessionTagForSessions
	setSessionTagForSessions = func(sessions []string, key, value string, _ tmux.Options) error {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, append([]string(nil), sessions...))
		tagKey, tagValue = key, value
		return nil
	}
	t.Cleanup(func() { setSessionTagForSessions = prev })

	m.markActivityTagForFlush("amux-sess-a")
	m.markActivityTagForFlush("amux-sess-b")
	m.Update(activityTagFlushDue{})

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(calls)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("batched tag write never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 batched write, got %d", len(calls))
	}
	if len(calls[0]) != 2 {
		t.Fatalf("batch carried %v, want both sessions", calls[0])
	}
	if tagKey != tmux.TagLastOutputAt {
		t.Fatalf("tag key = %q, want %q", tagKey, tmux.TagLastOutputAt)
	}
	if tagValue == "" {
		t.Fatal("flush must stamp a timestamp")
	}
}

// TestActivityTagFlush_MarksDuringFlightReschedule: a mark landing after the
// drain starts a new pending window instead of being lost.
func TestActivityTagFlush_MarksDuringFlightReschedule(t *testing.T) {
	m := New(nil)
	m.markActivityTagForFlush("amux-sess-a")
	if got := m.drainActivityTags(); len(got) != 1 {
		t.Fatalf("first drain = %v", got)
	}
	m.markActivityTagForFlush("amux-sess-c")
	if got := m.drainActivityTags(); len(got) != 1 || got[0] != "amux-sess-c" {
		t.Fatalf("post-drain mark must be pending, got %v", got)
	}
}
