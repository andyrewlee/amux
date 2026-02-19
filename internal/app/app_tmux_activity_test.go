package app

import (
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// stubTmuxOps implements TmuxOps for testing syncActivitySessionStates.
// Only AllSessionStates returns real data; all other methods return zero values.
type stubTmuxOps struct {
	allStates    map[string]tmux.SessionState
	allStatesErr error
}

func (s stubTmuxOps) EnsureAvailable() error { return nil }
func (s stubTmuxOps) InstallHint() string    { return "" }
func (s stubTmuxOps) ActiveAgentSessionsByActivity(time.Duration, tmux.Options) ([]tmux.SessionActivity, error) {
	return nil, nil
}

func (s stubTmuxOps) SessionsWithTags(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
	return nil, nil
}

func (s stubTmuxOps) AllSessionStates(tmux.Options) (map[string]tmux.SessionState, error) {
	return s.allStates, s.allStatesErr
}

func (s stubTmuxOps) SessionStateFor(string, tmux.Options) (tmux.SessionState, error) {
	return tmux.SessionState{}, nil
}
func (s stubTmuxOps) SessionHasClients(string, tmux.Options) (bool, error) { return false, nil }
func (s stubTmuxOps) SessionCreatedAt(string, tmux.Options) (int64, error) { return 0, nil }
func (s stubTmuxOps) KillSession(string, tmux.Options) error               { return nil }
func (s stubTmuxOps) KillSessionsMatchingTags(map[string]string, tmux.Options) (bool, error) {
	return false, nil
}
func (s stubTmuxOps) KillSessionsWithPrefix(string, tmux.Options) error        { return nil }
func (s stubTmuxOps) KillWorkspaceSessions(string, tmux.Options) error         { return nil }
func (s stubTmuxOps) SetMonitorActivityOn(tmux.Options) error                  { return nil }
func (s stubTmuxOps) SetStatusOff(tmux.Options) error                          { return nil }
func (s stubTmuxOps) CapturePaneTail(string, int, tmux.Options) (string, bool) { return "", false }
func (s stubTmuxOps) ContentHash(string) [16]byte                              { return [16]byte{} }

func TestScanTmuxActivityNow_QueuesWhenInFlight(t *testing.T) {
	app := &App{tmuxActivityScanInFlight: true}
	cmd := app.scanTmuxActivityNow()
	if cmd != nil {
		t.Fatal("expected nil cmd when scan already in flight")
	}
	if !app.tmuxActivityRescanPending {
		t.Fatal("expected pending rescan to be queued")
	}
}

func TestHandleTmuxActivityTick_QueuesWhenInFlight(t *testing.T) {
	app := &App{
		tmuxActivityToken:        7,
		tmuxAvailable:            true,
		tmuxActivityScanInFlight: true,
	}
	cmds := app.handleTmuxActivityTick(tmuxActivityTick{Token: 7})
	if len(cmds) != 1 {
		t.Fatalf("expected only ticker reschedule while in flight, got %d cmds", len(cmds))
	}
	if !app.tmuxActivityRescanPending {
		t.Fatal("expected pending rescan to be queued")
	}
	if app.tmuxActivityToken != 7 {
		t.Fatalf("expected token unchanged while in flight, got %d", app.tmuxActivityToken)
	}
}

func TestHandleTmuxActivityResult_ConsumesPendingRescan(t *testing.T) {
	app := &App{
		tmuxActivityToken:         2,
		tmuxAvailable:             true,
		tmuxActivityScanInFlight:  true,
		tmuxActivityRescanPending: true,
		sessionActivityStates:     make(map[string]*activity.SessionState),
		tmuxActiveWorkspaceIDs:    make(map[string]bool),
		dashboard:                 dashboard.New(),
	}
	cmds := app.handleTmuxActivityResult(tmuxActivityResult{
		Token:              2,
		ActiveWorkspaceIDs: map[string]bool{},
		UpdatedStates:      map[string]*activity.SessionState{},
	})
	if len(cmds) == 0 {
		t.Fatal("expected pending rescan command to be enqueued")
	}
	if app.tmuxActivityToken != 3 {
		t.Fatalf("expected next scan token to be allocated, got %d", app.tmuxActivityToken)
	}
	if !app.tmuxActivityScanInFlight {
		t.Fatal("expected follow-up scan to be marked in flight")
	}
	if app.tmuxActivityRescanPending {
		t.Fatal("expected pending flag to be cleared")
	}
}

// ---------------------------------------------------------------------------
// syncActivitySessionStates tests
// ---------------------------------------------------------------------------

func TestSyncActivitySessionStates_NilSvc(t *testing.T) {
	result := syncActivitySessionStates(
		map[string]activity.SessionInfo{"s": {Status: "running", WorkspaceID: "ws1"}},
		[]activity.TaggedSession{{Session: tmux.SessionActivity{Name: "s"}}},
		nil,
		tmux.Options{},
	)
	if len(result) != 0 {
		t.Fatalf("expected empty result with nil svc, got %d", len(result))
	}
}

func TestSyncActivitySessionStates_EmptyInfoBySession(t *testing.T) {
	svc := newTmuxService(stubTmuxOps{
		allStates: map[string]tmux.SessionState{
			"s": {Exists: true, HasLivePane: true},
		},
	})
	result := syncActivitySessionStates(
		map[string]activity.SessionInfo{},
		[]activity.TaggedSession{{Session: tmux.SessionActivity{Name: "s"}}},
		svc,
		tmux.Options{},
	)
	if len(result) != 0 {
		t.Fatalf("expected empty result with empty infoBySession, got %d", len(result))
	}
}

func TestSyncActivitySessionStates_AllSessionStatesError(t *testing.T) {
	svc := newTmuxService(stubTmuxOps{
		allStatesErr: errors.New("tmux failed"),
	})
	info := map[string]activity.SessionInfo{
		"s": {Status: "running", WorkspaceID: "ws1"},
	}
	result := syncActivitySessionStates(
		info,
		[]activity.TaggedSession{{Session: tmux.SessionActivity{Name: "s"}}},
		svc,
		tmux.Options{},
	)
	if len(result) != 0 {
		t.Fatalf("expected empty result on AllSessionStates error, got %d", len(result))
	}
	// infoBySession should not be mutated on error.
	if info["s"].Status != "running" {
		t.Fatalf("expected info unchanged on error, got %q", info["s"].Status)
	}
}

func TestSyncActivitySessionStates_RunningSessionDeadPane(t *testing.T) {
	svc := newTmuxService(stubTmuxOps{
		allStates: map[string]tmux.SessionState{
			"s": {Exists: true, HasLivePane: false},
		},
	})
	info := map[string]activity.SessionInfo{
		"s": {Status: "running", WorkspaceID: "ws1"},
	}
	result := syncActivitySessionStates(
		info,
		[]activity.TaggedSession{{Session: tmux.SessionActivity{Name: "s"}}},
		svc,
		tmux.Options{},
	)
	if len(result) != 1 {
		t.Fatalf("expected 1 stopped tab, got %d", len(result))
	}
	if result[0].SessionName != "s" || result[0].Status != "stopped" || result[0].WorkspaceID != "ws1" {
		t.Fatalf("unexpected stopped tab: %+v", result[0])
	}
	if info["s"].Status != "stopped" {
		t.Fatalf("expected infoBySession mutated to stopped, got %q", info["s"].Status)
	}
}

func TestSyncActivitySessionStates_RunningSessionDisappeared(t *testing.T) {
	// Session appears in tagged list but not in AllSessionStates (disappeared).
	svc := newTmuxService(stubTmuxOps{
		allStates: map[string]tmux.SessionState{}, // empty: session gone
	})
	info := map[string]activity.SessionInfo{
		"s": {Status: "running", WorkspaceID: "ws1"},
	}
	result := syncActivitySessionStates(
		info,
		[]activity.TaggedSession{{Session: tmux.SessionActivity{Name: "s"}}},
		svc,
		tmux.Options{},
	)
	if len(result) != 1 {
		t.Fatalf("expected 1 stopped tab for disappeared session, got %d", len(result))
	}
	if result[0].SessionName != "s" || result[0].Status != "stopped" {
		t.Fatalf("unexpected stopped tab: %+v", result[0])
	}
	if info["s"].Status != "stopped" {
		t.Fatalf("expected infoBySession mutated to stopped, got %q", info["s"].Status)
	}
}

func TestSyncActivitySessionStates_StoppedSessionRevived(t *testing.T) {
	svc := newTmuxService(stubTmuxOps{
		allStates: map[string]tmux.SessionState{
			"s": {Exists: true, HasLivePane: true},
		},
	})
	info := map[string]activity.SessionInfo{
		"s": {Status: "stopped", WorkspaceID: "ws1"},
	}
	result := syncActivitySessionStates(
		info,
		[]activity.TaggedSession{{Session: tmux.SessionActivity{Name: "s"}}},
		svc,
		tmux.Options{},
	)
	if len(result) != 0 {
		t.Fatalf("expected no stopped emissions for revived session, got %d", len(result))
	}
	if info["s"].Status != "running" {
		t.Fatalf("expected revived session status to be running, got %q", info["s"].Status)
	}
}

func TestSyncActivitySessionStates_AlreadyStoppedDisappeared(t *testing.T) {
	// A session already marked stopped that also disappeared should not emit a duplicate.
	svc := newTmuxService(stubTmuxOps{
		allStates: map[string]tmux.SessionState{},
	})
	info := map[string]activity.SessionInfo{
		"s": {Status: "stopped", WorkspaceID: "ws1"},
	}
	result := syncActivitySessionStates(
		info,
		[]activity.TaggedSession{{Session: tmux.SessionActivity{Name: "s"}}},
		svc,
		tmux.Options{},
	)
	if len(result) != 0 {
		t.Fatalf("expected no duplicate stopped emission, got %d", len(result))
	}
}

func TestSyncActivitySessionStates_TaggedNotInInfo(t *testing.T) {
	// Session in tagged list but not in infoBySession should be skipped.
	svc := newTmuxService(stubTmuxOps{
		allStates: map[string]tmux.SessionState{
			"unknown": {Exists: true, HasLivePane: false},
		},
	})
	info := map[string]activity.SessionInfo{}
	result := syncActivitySessionStates(
		info,
		[]activity.TaggedSession{{Session: tmux.SessionActivity{Name: "unknown"}}},
		svc,
		tmux.Options{},
	)
	if len(result) != 0 {
		t.Fatalf("expected no stopped emissions for unknown session, got %d", len(result))
	}
}

func TestSyncActivitySessionStates_InfoNotInTaggedRunning(t *testing.T) {
	// Session in infoBySession but not in tagged list, with running status → emits stopped (second loop).
	svc := newTmuxService(stubTmuxOps{
		allStates: map[string]tmux.SessionState{},
	})
	info := map[string]activity.SessionInfo{
		"orphan": {Status: "running", WorkspaceID: "ws1"},
	}
	result := syncActivitySessionStates(
		info,
		[]activity.TaggedSession{}, // no tagged sessions
		svc,
		tmux.Options{},
	)
	if len(result) != 1 {
		t.Fatalf("expected 1 stopped tab for orphan running session, got %d", len(result))
	}
	if result[0].SessionName != "orphan" || result[0].Status != "stopped" {
		t.Fatalf("unexpected stopped tab: %+v", result[0])
	}
	if info["orphan"].Status != "stopped" {
		t.Fatalf("expected orphan mutated to stopped, got %q", info["orphan"].Status)
	}
}
