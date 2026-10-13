package activity

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// These tests pin the capture-subprocess count per activity scan:
// the scan must cost exactly one capture per fallback-routed session — never a
// second capture for a baseline that was already seeded in the same scan's
// classification pass. List-call fan-out lives behind SessionsWithTags /
// AllSessionStates / ActiveAgentSessionsByActivity and is pinned separately in
// internal/tmux/subprocess_fanout_test.go.

// freshKnownSession builds a tagged session that routes through
// classifyFreshOutput's known-tab path: fresh output tag + recent window
// activity + an entry in infoBySession.
func freshKnownSession(name, workspaceID string, now time.Time) TaggedSession {
	return TaggedSession{
		Session: tmux.SessionActivity{
			Name:        name,
			WorkspaceID: workspaceID,
			Type:        "agent",
			Tagged:      true,
		},
		HasLastOutput: true,
		LastOutputAt:  now,
	}
}

func TestActivityScan_SeededBaselineCapturesOnceNotTwice(t *testing.T) {
	now := time.Now()
	sessions := []TaggedSession{freshKnownSession("sess-a", "ws-a", now)}
	info := map[string]SessionInfo{
		"sess-a": {WorkspaceID: "ws-a", Status: "running"},
	}
	recent := map[string]bool{"sess-a": true}
	states := map[string]*SessionState{}

	var captures atomic.Int64
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captures.Add(1)
		return "pane content", true
	}

	_, _, _ = ActiveWorkspaceIDsFromTagsWithRemoved(info, sessions, recent, states, tmux.Options{}, captureFn, tmux.ContentHash)
	if captures.Load() != 1 {
		t.Fatalf("first scan of a fresh-tag session must capture exactly once (seed reuses itself in hysteresis), got %d", captures.Load())
	}
}

// TestActivityScan_CaptureCountScalesWithSessions pins the N-session formula:
// K fresh-tag known sessions cost exactly K captures on first observation —
// previously 2K because the seeded baseline was captured twice.
func TestActivityScan_CaptureCountScalesWithSessions(t *testing.T) {
	now := time.Now()
	const k = 10
	sessions := make([]TaggedSession, 0, k)
	info := map[string]SessionInfo{}
	recent := map[string]bool{}
	for i := 0; i < k; i++ {
		name := "sess-" + string(rune('a'+i))
		sessions = append(sessions, freshKnownSession(name, "ws-"+name, now))
		info[name] = SessionInfo{WorkspaceID: "ws-" + name, Status: "running"}
		recent[name] = true
	}
	states := map[string]*SessionState{}

	var captures atomic.Int64
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captures.Add(1)
		return "pane content", true
	}

	_, _, _ = ActiveWorkspaceIDsFromTagsWithRemoved(info, sessions, recent, states, tmux.Options{}, captureFn, tmux.ContentHash)
	if captures.Load() != k {
		t.Fatalf("first scan of %d fresh-tag sessions must capture %d times, got %d", k, k, captures.Load())
	}
}

// TestActivityScan_SteadyStateCapturesOncePerSession covers the second scan:
// initialized states are no longer seeded, so hysteresis captures each
// fallback session exactly once.
func TestActivityScan_SteadyStateCapturesOncePerSession(t *testing.T) {
	now := time.Now()
	sessions := []TaggedSession{
		freshKnownSession("sess-a", "ws-a", now),
		freshKnownSession("sess-b", "ws-b", now),
	}
	info := map[string]SessionInfo{
		"sess-a": {WorkspaceID: "ws-a", Status: "running"},
		"sess-b": {WorkspaceID: "ws-b", Status: "running"},
	}
	recent := map[string]bool{"sess-a": true, "sess-b": true}
	states := map[string]*SessionState{}

	var captures atomic.Int64
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captures.Add(1)
		return "pane content", true
	}

	_, updated, _ := ActiveWorkspaceIDsFromTagsWithRemoved(info, sessions, recent, states, tmux.Options{}, captureFn, tmux.ContentHash)
	if captures.Load() != 2 {
		t.Fatalf("first scan must capture once per session, got %d", captures.Load())
	}

	captures.Store(0)
	_, _, _ = ActiveWorkspaceIDsFromTagsWithRemoved(info, sessions, recent, updated, tmux.Options{}, captureFn, tmux.ContentHash)
	if captures.Load() != 2 {
		t.Fatalf("steady-state scan must capture once per fallback session, got %d", captures.Load())
	}
}

// TestActivityScan_ZeroSessionsNoCaptures pins the empty-scan floor.
func TestActivityScan_ZeroSessionsNoCaptures(t *testing.T) {
	var captures atomic.Int64
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captures.Add(1)
		return "", true
	}
	_, _, _ = ActiveWorkspaceIDsFromTagsWithRemoved(nil, nil, nil, nil, tmux.Options{}, captureFn, tmux.ContentHash)
	if captures.Load() != 0 {
		t.Fatalf("empty scan must not capture, got %d", captures.Load())
	}
}

// taglessRunningSession builds a session with no output tag, so the
// classifier routes it straight to hysteresis fallback — the path that runs
// captures through the bounded worker pool.
func taglessRunningSession(name, workspaceID string) TaggedSession {
	return TaggedSession{
		Session: tmux.SessionActivity{
			Name:        name,
			WorkspaceID: workspaceID,
			Type:        "agent",
			Tagged:      true,
		},
	}
}

// TestActivityScan_FallbackCapturesBoundedParallel proves three properties of
// the parallel capture phase at once: in-flight captures never exceed the
// worker bound, at least two captures genuinely overlap, and the hysteresis
// outcome for every session matches serial semantics.
func TestActivityScan_FallbackCapturesBoundedParallel(t *testing.T) {
	const n = 12 // > activityCaptureWorkers, so the bound is observable
	sessions := make([]TaggedSession, 0, n)
	info := map[string]SessionInfo{}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("sess-%02d", i)
		sessions = append(sessions, taglessRunningSession(name, "ws-"+name))
		info[name] = SessionInfo{WorkspaceID: "ws-" + name, Status: "running"}
	}
	states := map[string]*SessionState{}

	// Rendezvous: the first two captures to start both block until both have
	// entered, so overlap is proven deterministically. A serial regression
	// would leave only one capture entered, so the gate times out and the
	// maxInFlight assertion below fails instead of hanging.
	enteredTwo := make(chan struct{})
	var entered atomic.Int64
	go func() {
		for entered.Load() < 2 {
			time.Sleep(time.Millisecond)
		}
		close(enteredTwo)
	}()

	var inflight, maxInFlight, calls atomic.Int64
	captureFn := func(name string, _ int, _ tmux.Options) (string, bool) {
		calls.Add(1)
		cur := inflight.Add(1)
		for {
			m := maxInFlight.Load()
			if cur <= m || maxInFlight.CompareAndSwap(m, cur) {
				break
			}
		}
		if entered.Add(1) <= 2 {
			select {
			case <-enteredTwo:
			case <-time.After(3 * time.Second):
			}
		}
		inflight.Add(-1)
		return "pane-" + name, true
	}

	active, updated, _ := ActiveWorkspaceIDsFromTagsWithRemoved(info, sessions, nil, states, tmux.Options{}, captureFn, tmux.ContentHash)

	if got := calls.Load(); got != n {
		t.Fatalf("expected %d fallback captures, got %d", n, got)
	}
	if got := maxInFlight.Load(); got > activityCaptureWorkers {
		t.Fatalf("in-flight captures %d exceeded worker bound %d", got, activityCaptureWorkers)
	}
	if got := maxInFlight.Load(); got < 2 {
		t.Fatalf("expected overlapping captures with %d sessions, max in-flight was %d", n, got)
	}
	// Serial semantics: each session initialized from its own capture, scored
	// at threshold, and marked its workspace active.
	for _, s := range sessions {
		st := updated[s.Session.Name]
		if st == nil || !st.Initialized {
			t.Fatalf("session %s missing initialized state: %+v", s.Session.Name, st)
		}
		if st.Score != ScoreThreshold {
			t.Fatalf("session %s score = %d, want %d", s.Session.Name, st.Score, ScoreThreshold)
		}
		if want := tmux.ContentHash("pane-" + s.Session.Name); st.LastHash != want {
			t.Fatalf("session %s hash mismatch: got %v want %v", s.Session.Name, st.LastHash, want)
		}
		if !active["ws-"+s.Session.Name] {
			t.Fatalf("session %s workspace not active", s.Session.Name)
		}
	}
}

// TestActivityScan_ParallelCaptureFailureDecays proves the capture-failure
// decay branch is preserved under parallel execution: failed captures decay
// score and stay uninitialized/inactive while successful ones initialize.
func TestActivityScan_ParallelCaptureFailureDecays(t *testing.T) {
	const n = 8
	sessions := make([]TaggedSession, 0, n)
	info := map[string]SessionInfo{}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("sess-%02d", i)
		sessions = append(sessions, taglessRunningSession(name, "ws-"+name))
		info[name] = SessionInfo{WorkspaceID: "ws-" + name, Status: "running"}
	}
	states := map[string]*SessionState{}

	captureFn := func(name string, _ int, _ tmux.Options) (string, bool) {
		if name == "sess-00" || name == "sess-03" || name == "sess-07" {
			return "", false
		}
		return "pane-" + name, true
	}

	active, updated, _ := ActiveWorkspaceIDsFromTagsWithRemoved(info, sessions, nil, states, tmux.Options{}, captureFn, tmux.ContentHash)

	failed := map[string]bool{"sess-00": true, "sess-03": true, "sess-07": true}
	for _, s := range sessions {
		st := updated[s.Session.Name]
		if st == nil {
			t.Fatalf("session %s missing state", s.Session.Name)
		}
		if failed[s.Session.Name] {
			if st.Initialized || st.Score != 0 {
				t.Fatalf("failed session %s: initialized=%v score=%d, want uninitialized score 0", s.Session.Name, st.Initialized, st.Score)
			}
			if active["ws-"+s.Session.Name] {
				t.Fatalf("failed session %s must not mark workspace active", s.Session.Name)
			}
			continue
		}
		if !st.Initialized || st.Score != ScoreThreshold {
			t.Fatalf("session %s: initialized=%v score=%d, want initialized score %d", s.Session.Name, st.Initialized, st.Score, ScoreThreshold)
		}
		if !active["ws-"+s.Session.Name] {
			t.Fatalf("session %s workspace must be active", s.Session.Name)
		}
	}
}
