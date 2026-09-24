package activity

import (
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

	captures := 0
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captures++
		return "pane content", true
	}

	_, _, _ = ActiveWorkspaceIDsFromTagsWithRemoved(info, sessions, recent, states, tmux.Options{}, captureFn, tmux.ContentHash)
	if captures != 1 {
		t.Fatalf("first scan of a fresh-tag session must capture exactly once (seed reuses itself in hysteresis), got %d", captures)
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

	captures := 0
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captures++
		return "pane content", true
	}

	_, _, _ = ActiveWorkspaceIDsFromTagsWithRemoved(info, sessions, recent, states, tmux.Options{}, captureFn, tmux.ContentHash)
	if captures != k {
		t.Fatalf("first scan of %d fresh-tag sessions must capture %d times, got %d", k, k, captures)
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

	captures := 0
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captures++
		return "pane content", true
	}

	_, updated, _ := ActiveWorkspaceIDsFromTagsWithRemoved(info, sessions, recent, states, tmux.Options{}, captureFn, tmux.ContentHash)
	if captures != 2 {
		t.Fatalf("first scan must capture once per session, got %d", captures)
	}

	captures = 0
	_, _, _ = ActiveWorkspaceIDsFromTagsWithRemoved(info, sessions, recent, updated, tmux.Options{}, captureFn, tmux.ContentHash)
	if captures != 2 {
		t.Fatalf("steady-state scan must capture once per fallback session, got %d", captures)
	}
}

// TestActivityScan_ZeroSessionsNoCaptures pins the empty-scan floor.
func TestActivityScan_ZeroSessionsNoCaptures(t *testing.T) {
	captures := 0
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captures++
		return "", true
	}
	_, _, _ = ActiveWorkspaceIDsFromTagsWithRemoved(nil, nil, nil, nil, tmux.Options{}, captureFn, tmux.ContentHash)
	if captures != 0 {
		t.Fatalf("empty scan must not capture, got %d", captures)
	}
}
