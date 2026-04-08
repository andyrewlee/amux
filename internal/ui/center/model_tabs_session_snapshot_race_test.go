package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestReattachActiveTab_DiscardsPreAttachSnapshotWhenSessionRecreated(t *testing.T) {
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionClientCountFn := sessionClientCountFn
	oldSessionActiveWithinFn := sessionActiveWithinFn
	oldSessionCreatedAtFn := sessionCreatedAtFn
	oldSessionPaneIDFn := sessionPaneIDFn
	oldSessionPaneSnapshotInfoFn := sessionPaneSnapshotInfoFn
	oldSessionPaneSizeFn := sessionPaneSizeFn
	oldResizePaneToSizeFn := resizePaneToSizeFn
	oldCapturePaneSnapshotFn := capturePaneSnapshotFn
	oldCapturePaneFn := capturePaneFn
	oldCreateAgentWithTagsFn := createAgentWithTagsFn
	defer func() {
		sessionStateForFn = oldSessionStateForFn
		sessionHasClientsFn = oldSessionHasClientsFn
		sessionClientCountFn = oldSessionClientCountFn
		sessionActiveWithinFn = oldSessionActiveWithinFn
		sessionCreatedAtFn = oldSessionCreatedAtFn
		sessionPaneIDFn = oldSessionPaneIDFn
		sessionPaneSnapshotInfoFn = oldSessionPaneSnapshotInfoFn
		sessionPaneSizeFn = oldSessionPaneSizeFn
		resizePaneToSizeFn = oldResizePaneToSizeFn
		capturePaneSnapshotFn = oldCapturePaneSnapshotFn
		capturePaneFn = oldCapturePaneFn
		createAgentWithTagsFn = oldCreateAgentWithTagsFn
	}()

	calls := make([]string, 0, 8)
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionHasClientsFn = func(sessionName string, opts tmux.Options) (bool, error) {
		calls = append(calls, "clients")
		return false, nil
	}
	sessionClientCountFn = func(sessionName string, opts tmux.Options) (int, error) {
		return 1, nil
	}
	sessionActiveWithinFn = func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
		calls = append(calls, "activity")
		return false, nil
	}
	createdAtCalls := 0
	sessionCreatedAtFn = func(sessionName string, opts tmux.Options) (int64, error) {
		createdAtCalls++
		return 111, nil
	}
	paneIDCalls := 0
	sessionPaneIDFn = func(sessionName string, opts tmux.Options) (string, error) {
		paneIDCalls++
		if paneIDCalls <= 4 {
			return "%old", nil
		}
		return "%new", nil
	}
	sessionPaneSnapshotInfoFn = func(sessionName string, opts tmux.Options) (int, int, bool, error) {
		calls = append(calls, "info")
		return 91, 27, true, nil
	}
	sessionPaneSizeFn = func(sessionName string, opts tmux.Options) (int, int, bool, error) {
		calls = append(calls, "size")
		return 123, 45, true, nil
	}
	resizePaneToSizeFn = func(sessionName string, cols, rows int, opts tmux.Options) error {
		calls = append(calls, "resize")
		return nil
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("stale frame"), Cols: 77, Rows: 19}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("fresh history"), nil
	}
	createAgentWithTagsFn = func(
		manager *appPty.AgentManager,
		ws *data.Workspace,
		agentType appPty.AgentType,
		sessionName string,
		rows, cols uint16,
		tags tmux.SessionTags,
	) (*appPty.Agent, error) {
		calls = append(calls, "attach")
		return &appPty.Agent{Session: sessionName}, nil
	}

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-reattach-race"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "session-race",
		Detached:    true,
	}
	m.workspace = ws
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0

	msg := m.ReattachActiveTab()()
	result, ok := msg.(ptyTabReattachResult)
	if !ok {
		t.Fatalf("expected ptyTabReattachResult, got %T", msg)
	}
	if result.CaptureFullPane {
		t.Fatal("expected recreated session to discard the pre-attach snapshot")
	}
	if got := string(result.ScrollbackCapture); got != "fresh history" {
		t.Fatalf("expected post-attach history capture, got %q", got)
	}
	if len(result.PostAttachScrollbackCapture) != 0 {
		t.Fatalf("expected no reconciliation capture after demoting to history-only restore, got %q", string(result.PostAttachScrollbackCapture))
	}
	if result.Cols != 123 || result.Rows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", result.Cols, result.Rows)
	}
	if result.SnapshotCols != 0 || result.SnapshotRows != 0 {
		t.Fatalf("expected stale snapshot metadata to be cleared, got %dx%d", result.SnapshotCols, result.SnapshotRows)
	}
	assertCallOrder(t, calls, "state", "clients", "activity", "info", "resize", "snapshot", "attach", "size", "scrollback")
}

func TestReattachToSession_DiscardsPreAttachSnapshotWhenSessionRecreated(t *testing.T) {
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionClientCountFn := sessionClientCountFn
	oldSessionActiveWithinFn := sessionActiveWithinFn
	oldSessionCreatedAtFn := sessionCreatedAtFn
	oldSessionPaneIDFn := sessionPaneIDFn
	oldSessionPaneSnapshotInfoFn := sessionPaneSnapshotInfoFn
	oldSessionPaneSizeFn := sessionPaneSizeFn
	oldResizePaneToSizeFn := resizePaneToSizeFn
	oldCapturePaneSnapshotFn := capturePaneSnapshotFn
	oldCapturePaneFn := capturePaneFn
	oldCreateAgentWithTagsFn := createAgentWithTagsFn
	defer func() {
		sessionStateForFn = oldSessionStateForFn
		sessionHasClientsFn = oldSessionHasClientsFn
		sessionClientCountFn = oldSessionClientCountFn
		sessionActiveWithinFn = oldSessionActiveWithinFn
		sessionCreatedAtFn = oldSessionCreatedAtFn
		sessionPaneIDFn = oldSessionPaneIDFn
		sessionPaneSnapshotInfoFn = oldSessionPaneSnapshotInfoFn
		sessionPaneSizeFn = oldSessionPaneSizeFn
		resizePaneToSizeFn = oldResizePaneToSizeFn
		capturePaneSnapshotFn = oldCapturePaneSnapshotFn
		capturePaneFn = oldCapturePaneFn
		createAgentWithTagsFn = oldCreateAgentWithTagsFn
	}()

	calls := make([]string, 0, 8)
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionHasClientsFn = func(sessionName string, opts tmux.Options) (bool, error) {
		calls = append(calls, "clients")
		return false, nil
	}
	sessionClientCountFn = func(sessionName string, opts tmux.Options) (int, error) {
		return 1, nil
	}
	sessionActiveWithinFn = func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
		calls = append(calls, "activity")
		return false, nil
	}
	createdAtCalls := 0
	sessionCreatedAtFn = func(sessionName string, opts tmux.Options) (int64, error) {
		createdAtCalls++
		return 111, nil
	}
	paneIDCalls := 0
	sessionPaneIDFn = func(sessionName string, opts tmux.Options) (string, error) {
		paneIDCalls++
		if paneIDCalls <= 4 {
			return "%old", nil
		}
		return "%new", nil
	}
	sessionPaneSnapshotInfoFn = func(sessionName string, opts tmux.Options) (int, int, bool, error) {
		calls = append(calls, "info")
		return 91, 27, true, nil
	}
	sessionPaneSizeFn = func(sessionName string, opts tmux.Options) (int, int, bool, error) {
		calls = append(calls, "size")
		return 123, 45, true, nil
	}
	resizePaneToSizeFn = func(sessionName string, cols, rows int, opts tmux.Options) error {
		calls = append(calls, "resize")
		return nil
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("stale frame"), Cols: 77, Rows: 19}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("fresh history"), nil
	}
	createAgentWithTagsFn = func(
		manager *appPty.AgentManager,
		ws *data.Workspace,
		agentType appPty.AgentType,
		sessionName string,
		rows, cols uint16,
		tags tmux.SessionTags,
	) (*appPty.Agent, error) {
		calls = append(calls, "attach")
		return &appPty.Agent{Session: sessionName}, nil
	}

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")

	msg := m.reattachToSession(ws, TabID("tab-restore-race"), "codex", "session-race")()
	result, ok := msg.(ptyTabReattachResult)
	if !ok {
		t.Fatalf("expected ptyTabReattachResult, got %T", msg)
	}
	if result.CaptureFullPane {
		t.Fatal("expected recreated session to discard the pre-attach snapshot")
	}
	if got := string(result.ScrollbackCapture); got != "fresh history" {
		t.Fatalf("expected post-attach history capture, got %q", got)
	}
	if len(result.PostAttachScrollbackCapture) != 0 {
		t.Fatalf("expected no reconciliation capture after demoting to history-only restore, got %q", string(result.PostAttachScrollbackCapture))
	}
	if result.Cols != 123 || result.Rows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", result.Cols, result.Rows)
	}
	if result.SnapshotCols != 0 || result.SnapshotRows != 0 {
		t.Fatalf("expected stale snapshot metadata to be cleared, got %dx%d", result.SnapshotCols, result.SnapshotRows)
	}
	assertCallOrder(t, calls, "state", "clients", "activity", "info", "resize", "snapshot", "attach", "size", "scrollback")
}

func TestReattachActiveTab_DiscardsPreAttachSnapshotWhenSessionBecomesActive(t *testing.T) {
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionClientCountFn := sessionClientCountFn
	oldSessionActiveWithinFn := sessionActiveWithinFn
	oldSessionCreatedAtFn := sessionCreatedAtFn
	oldSessionPaneIDFn := sessionPaneIDFn
	oldSessionPaneSnapshotInfoFn := sessionPaneSnapshotInfoFn
	oldSessionPaneSizeFn := sessionPaneSizeFn
	oldResizePaneToSizeFn := resizePaneToSizeFn
	oldCapturePaneSnapshotFn := capturePaneSnapshotFn
	oldCapturePaneFn := capturePaneFn
	oldCreateAgentWithTagsFn := createAgentWithTagsFn
	defer func() {
		sessionStateForFn = oldSessionStateForFn
		sessionHasClientsFn = oldSessionHasClientsFn
		sessionClientCountFn = oldSessionClientCountFn
		sessionActiveWithinFn = oldSessionActiveWithinFn
		sessionCreatedAtFn = oldSessionCreatedAtFn
		sessionPaneIDFn = oldSessionPaneIDFn
		sessionPaneSnapshotInfoFn = oldSessionPaneSnapshotInfoFn
		sessionPaneSizeFn = oldSessionPaneSizeFn
		resizePaneToSizeFn = oldResizePaneToSizeFn
		capturePaneSnapshotFn = oldCapturePaneSnapshotFn
		capturePaneFn = oldCapturePaneFn
		createAgentWithTagsFn = oldCreateAgentWithTagsFn
	}()

	calls := make([]string, 0, 12)
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionHasClientsFn = func(sessionName string, opts tmux.Options) (bool, error) {
		calls = append(calls, "clients")
		return false, nil
	}
	sessionClientCountFn = func(sessionName string, opts tmux.Options) (int, error) {
		return 1, nil
	}
	activityCalls := 0
	sessionActiveWithinFn = func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
		calls = append(calls, "activity")
		activityCalls++
		return activityCalls >= 5, nil
	}
	sessionCreatedAtFn = func(sessionName string, opts tmux.Options) (int64, error) {
		return 111, nil
	}
	sessionPaneIDFn = func(sessionName string, opts tmux.Options) (string, error) {
		return "%same", nil
	}
	sessionPaneSnapshotInfoFn = func(sessionName string, opts tmux.Options) (int, int, bool, error) {
		calls = append(calls, "info")
		return 91, 27, true, nil
	}
	sessionPaneSizeFn = func(sessionName string, opts tmux.Options) (int, int, bool, error) {
		calls = append(calls, "size")
		return 123, 45, true, nil
	}
	resizePaneToSizeFn = func(sessionName string, cols, rows int, opts tmux.Options) error {
		calls = append(calls, "resize")
		return nil
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("stale frame"), Cols: 77, Rows: 19}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("post history"), nil
	}
	createAgentWithTagsFn = func(
		manager *appPty.AgentManager,
		ws *data.Workspace,
		agentType appPty.AgentType,
		sessionName string,
		rows, cols uint16,
		tags tmux.SessionTags,
	) (*appPty.Agent, error) {
		calls = append(calls, "attach")
		return &appPty.Agent{Session: sessionName}, nil
	}

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-reattach-active-race"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "session-race",
		Detached:    true,
	}
	m.workspace = ws
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0

	msg := m.ReattachActiveTab()()
	result, ok := msg.(ptyTabReattachResult)
	if !ok {
		t.Fatalf("expected ptyTabReattachResult, got %T", msg)
	}
	if result.CaptureFullPane {
		t.Fatal("expected activity after snapshot capture to demote back to history-only restore")
	}
	if got := string(result.ScrollbackCapture); got != "post history" {
		t.Fatalf("expected post-attach history recapture after stale snapshot demotion, got %q", got)
	}
	if len(result.PostAttachScrollbackCapture) != 0 {
		t.Fatalf("expected no reconciliation delta after stale snapshot demotion, got %q", string(result.PostAttachScrollbackCapture))
	}
	assertCallOrder(t, calls, "state", "clients", "activity", "info", "resize", "snapshot", "attach", "size", "scrollback")
}

func TestReattachActiveTab_DiscardsPreAttachSnapshotWhenSessionBecomesShared(t *testing.T) {
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionClientCountFn := sessionClientCountFn
	oldSessionActiveWithinFn := sessionActiveWithinFn
	oldSessionCreatedAtFn := sessionCreatedAtFn
	oldSessionPaneIDFn := sessionPaneIDFn
	oldSessionPaneSnapshotInfoFn := sessionPaneSnapshotInfoFn
	oldSessionPaneSizeFn := sessionPaneSizeFn
	oldResizePaneToSizeFn := resizePaneToSizeFn
	oldCapturePaneSnapshotFn := capturePaneSnapshotFn
	oldCapturePaneFn := capturePaneFn
	oldCreateAgentWithTagsFn := createAgentWithTagsFn
	defer func() {
		sessionStateForFn = oldSessionStateForFn
		sessionHasClientsFn = oldSessionHasClientsFn
		sessionClientCountFn = oldSessionClientCountFn
		sessionActiveWithinFn = oldSessionActiveWithinFn
		sessionCreatedAtFn = oldSessionCreatedAtFn
		sessionPaneIDFn = oldSessionPaneIDFn
		sessionPaneSnapshotInfoFn = oldSessionPaneSnapshotInfoFn
		sessionPaneSizeFn = oldSessionPaneSizeFn
		resizePaneToSizeFn = oldResizePaneToSizeFn
		capturePaneSnapshotFn = oldCapturePaneSnapshotFn
		capturePaneFn = oldCapturePaneFn
		createAgentWithTagsFn = oldCreateAgentWithTagsFn
	}()

	calls := make([]string, 0, 12)
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionHasClientsFn = func(sessionName string, opts tmux.Options) (bool, error) {
		calls = append(calls, "clients")
		return false, nil
	}
	sessionClientCountFn = func(sessionName string, opts tmux.Options) (int, error) {
		return 2, nil
	}
	sessionActiveWithinFn = func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
		calls = append(calls, "activity")
		return false, nil
	}
	sessionCreatedAtFn = func(sessionName string, opts tmux.Options) (int64, error) {
		return 111, nil
	}
	sessionPaneIDFn = func(sessionName string, opts tmux.Options) (string, error) {
		return "%same", nil
	}
	sessionPaneSnapshotInfoFn = func(sessionName string, opts tmux.Options) (int, int, bool, error) {
		calls = append(calls, "info")
		return 91, 27, true, nil
	}
	sessionPaneSizeFn = func(sessionName string, opts tmux.Options) (int, int, bool, error) {
		calls = append(calls, "size")
		return 123, 45, true, nil
	}
	resizePaneToSizeFn = func(sessionName string, cols, rows int, opts tmux.Options) error {
		calls = append(calls, "resize")
		return nil
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("stale frame"), Cols: 77, Rows: 19}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("post history"), nil
	}
	createAgentWithTagsFn = func(
		manager *appPty.AgentManager,
		ws *data.Workspace,
		agentType appPty.AgentType,
		sessionName string,
		rows, cols uint16,
		tags tmux.SessionTags,
	) (*appPty.Agent, error) {
		calls = append(calls, "attach")
		return &appPty.Agent{Session: sessionName}, nil
	}

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-reattach-shared-race"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "session-race",
		Detached:    true,
	}
	m.workspace = ws
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0

	msg := m.ReattachActiveTab()()
	result, ok := msg.(ptyTabReattachResult)
	if !ok {
		t.Fatalf("expected ptyTabReattachResult, got %T", msg)
	}
	if result.CaptureFullPane {
		t.Fatal("expected shared session after snapshot capture to demote back to history-only restore")
	}
	if got := string(result.ScrollbackCapture); got != "post history" {
		t.Fatalf("expected post-attach history recapture after shared-session demotion, got %q", got)
	}
	if len(result.PostAttachScrollbackCapture) != 0 {
		t.Fatalf("expected no reconciliation delta after shared-session demotion, got %q", string(result.PostAttachScrollbackCapture))
	}
	assertCallOrder(t, calls, "state", "clients", "activity", "info", "resize", "snapshot", "attach", "size", "scrollback")
}
