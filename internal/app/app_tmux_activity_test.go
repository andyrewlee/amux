package app

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
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

type scriptedActivityTmuxOps struct {
	sessionName   string
	workspaceID   string
	contentByScan []string
	scanIndex     int
}

func (s *scriptedActivityTmuxOps) EnsureAvailable() error { return nil }
func (s *scriptedActivityTmuxOps) InstallHint() string    { return "" }

func (s *scriptedActivityTmuxOps) ActiveAgentSessionsByActivity(time.Duration, tmux.Options) ([]tmux.SessionActivity, error) {
	return []tmux.SessionActivity{{
		Name:        s.sessionName,
		WorkspaceID: s.workspaceID,
		Type:        "agent",
		Tagged:      true,
	}}, nil
}

// SessionsWithTags increments scanIndex so CapturePaneTail can serve the
// matching content. This mirrors the real call order: sessions are fetched
// before pane content is captured.
func (s *scriptedActivityTmuxOps) SessionsWithTags(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
	s.scanIndex++
	nowMillis := strconv.FormatInt(time.Now().UnixMilli(), 10)
	return []tmux.SessionTagValues{{
		Name: s.sessionName,
		Tags: map[string]string{
			"@amux":              "1",
			"@amux_workspace":    s.workspaceID,
			"@amux_tab":          "tab-1",
			"@amux_type":         "agent",
			tmux.TagLastOutputAt: nowMillis,
		},
	}}, nil
}

func (s *scriptedActivityTmuxOps) AllSessionStates(tmux.Options) (map[string]tmux.SessionState, error) {
	return map[string]tmux.SessionState{
		s.sessionName: {Exists: true, HasLivePane: true},
	}, nil
}

func (s *scriptedActivityTmuxOps) SessionStateFor(string, tmux.Options) (tmux.SessionState, error) {
	return tmux.SessionState{Exists: true, HasLivePane: true}, nil
}

func (s *scriptedActivityTmuxOps) SessionHasClients(string, tmux.Options) (bool, error) {
	return true, nil
}

func (s *scriptedActivityTmuxOps) SessionCreatedAt(string, tmux.Options) (int64, error) {
	return 0, nil
}
func (s *scriptedActivityTmuxOps) KillSession(string, tmux.Options) error { return nil }
func (s *scriptedActivityTmuxOps) KillSessionsMatchingTags(map[string]string, tmux.Options) (bool, error) {
	return false, nil
}
func (s *scriptedActivityTmuxOps) KillSessionsWithPrefix(string, tmux.Options) error { return nil }
func (s *scriptedActivityTmuxOps) KillWorkspaceSessions(string, tmux.Options) error  { return nil }
func (s *scriptedActivityTmuxOps) SetMonitorActivityOn(tmux.Options) error           { return nil }
func (s *scriptedActivityTmuxOps) SetStatusOff(tmux.Options) error                   { return nil }

func (s *scriptedActivityTmuxOps) CapturePaneTail(string, int, tmux.Options) (string, bool) {
	if len(s.contentByScan) == 0 {
		return "", true
	}
	idx := s.scanIndex - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(s.contentByScan) {
		idx = len(s.contentByScan) - 1
	}
	return s.contentByScan[idx], true
}

func (s *scriptedActivityTmuxOps) ContentHash(content string) [16]byte {
	return tmux.ContentHash(content)
}

func newActivityTestAppWithScriptedTmux(contentByScan []string) (*App, string) {
	repo := "/tmp/test-repo"
	root := "/tmp/test-repo"
	ws := data.Workspace{
		Name:   "test-ws",
		Branch: "main",
		Repo:   repo,
		Root:   root,
		OpenTabs: []data.TabInfo{{
			Assistant:   "codex",
			Name:        "codex",
			SessionName: "amux-testws-tab-1",
			Status:      "running",
		}},
	}
	wsID := string(ws.ID())
	project := data.Project{
		Name:       "test-repo",
		Path:       repo,
		Workspaces: []data.Workspace{ws},
	}
	tmuxOps := &scriptedActivityTmuxOps{
		sessionName:   "amux-testws-tab-1",
		workspaceID:   wsID,
		contentByScan: contentByScan,
	}
	app := &App{
		config: &config.Config{
			Assistants: map[string]config.AssistantConfig{
				"codex": {},
			},
		},
		projects:               []data.Project{project},
		tmuxService:            newTmuxService(tmuxOps),
		tmuxOptions:            tmux.Options{},
		tmuxAvailable:          true,
		dashboard:              dashboard.New(),
		sessionActivityStates:  make(map[string]*activity.SessionState),
		tmuxActiveWorkspaceIDs: make(map[string]bool),
	}
	return app, wsID
}

func runImmediateTmuxActivityScan(t *testing.T, app *App) {
	t.Helper()
	cmd := app.scanTmuxActivityNow()
	if cmd == nil {
		t.Fatal("expected scan command")
	}
	msg := cmd()
	result, ok := msg.(tmuxActivityResult)
	if !ok {
		t.Fatalf("expected tmuxActivityResult, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("unexpected scan error: %v", result.Err)
	}
	app.handleTmuxActivityResult(result)
}

func TestTmuxActivityScan_KnownFreshUnchangedRemainsInactiveAcrossScans(t *testing.T) {
	app, wsID := newActivityTestAppWithScriptedTmux([]string{
		"same output",
		"same output",
		"same output",
		"same output",
		"same output",
	})

	for i := 0; i < 5; i++ {
		runImmediateTmuxActivityScan(t, app)
		if app.tmuxActiveWorkspaceIDs[wsID] {
			t.Fatalf("scan %d: expected workspace %s to remain inactive on unchanged output", i+1, wsID)
		}
	}
}

func TestTmuxActivityScan_KnownFreshConsecutiveDeltasBecomeActive(t *testing.T) {
	app, wsID := newActivityTestAppWithScriptedTmux([]string{
		"baseline",
		"delta-one",
		"delta-two",
	})

	runImmediateTmuxActivityScan(t, app)
	if app.tmuxActiveWorkspaceIDs[wsID] {
		t.Fatalf("expected workspace %s inactive after baseline scan", wsID)
	}

	runImmediateTmuxActivityScan(t, app)
	if app.tmuxActiveWorkspaceIDs[wsID] {
		t.Fatalf("expected workspace %s inactive after first delta scan", wsID)
	}

	runImmediateTmuxActivityScan(t, app)
	if !app.tmuxActiveWorkspaceIDs[wsID] {
		t.Fatalf("expected workspace %s active after second consecutive delta", wsID)
	}
}
