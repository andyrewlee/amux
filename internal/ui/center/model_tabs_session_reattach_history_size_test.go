package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestReattachActiveTab_BusySessionCapturesHistoryAfterAttach(t *testing.T) {
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionActiveWithinFn := sessionActiveWithinFn
	oldSessionPaneSizeFn := sessionPaneSizeFn
	oldResizePaneToSizeFn := resizePaneToSizeFn
	oldCapturePaneSnapshotFn := capturePaneSnapshotFn
	oldCapturePaneFn := capturePaneFn
	oldCreateAgentWithTagsFn := createAgentWithTagsFn
	defer func() {
		sessionStateForFn = oldSessionStateForFn
		sessionHasClientsFn = oldSessionHasClientsFn
		sessionActiveWithinFn = oldSessionActiveWithinFn
		sessionPaneSizeFn = oldSessionPaneSizeFn
		resizePaneToSizeFn = oldResizePaneToSizeFn
		capturePaneSnapshotFn = oldCapturePaneSnapshotFn
		capturePaneFn = oldCapturePaneFn
		createAgentWithTagsFn = oldCreateAgentWithTagsFn
	}()

	calls := make([]string, 0, 4)
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionHasClientsFn = func(sessionName string, opts tmux.Options) (bool, error) {
		calls = append(calls, "clients")
		return false, nil
	}
	sessionActiveWithinFn = func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
		calls = append(calls, "activity")
		return true, nil
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
		return tmux.PaneSnapshot{Data: []byte("should not use")}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("history"), nil
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
		ID:          TabID("tab-busy-reattach"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "session-busy",
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
		t.Fatal("expected busy session to skip pre-attach full-pane snapshot")
	}
	if got := string(result.ScrollbackCapture); got != "history" {
		t.Fatalf("expected history-only fallback, got %q", got)
	}
	if result.Cols != 123 || result.Rows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", result.Cols, result.Rows)
	}
	assertCallOrder(t, calls, "state", "clients", "activity", "attach", "size", "scrollback")
	for _, call := range calls {
		if call == "snapshot" {
			t.Fatalf("expected busy session to avoid full snapshot capture, got %v", calls)
		}
		if call == "resize" {
			t.Fatalf("expected busy session to avoid pre-attach resize, got %v", calls)
		}
	}
}

func TestReattachActiveTab_SharedClientCapturesHistoryAfterAttach(t *testing.T) {
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionActiveWithinFn := sessionActiveWithinFn
	oldSessionPaneSizeFn := sessionPaneSizeFn
	oldResizePaneToSizeFn := resizePaneToSizeFn
	oldCapturePaneSnapshotFn := capturePaneSnapshotFn
	oldCapturePaneFn := capturePaneFn
	oldCreateAgentWithTagsFn := createAgentWithTagsFn
	defer func() {
		sessionStateForFn = oldSessionStateForFn
		sessionHasClientsFn = oldSessionHasClientsFn
		sessionActiveWithinFn = oldSessionActiveWithinFn
		sessionPaneSizeFn = oldSessionPaneSizeFn
		resizePaneToSizeFn = oldResizePaneToSizeFn
		capturePaneSnapshotFn = oldCapturePaneSnapshotFn
		capturePaneFn = oldCapturePaneFn
		createAgentWithTagsFn = oldCreateAgentWithTagsFn
	}()

	calls := make([]string, 0, 4)
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionHasClientsFn = func(sessionName string, opts tmux.Options) (bool, error) {
		calls = append(calls, "clients")
		return true, nil
	}
	sessionActiveWithinFn = func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
		calls = append(calls, "activity")
		return false, nil
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
		return tmux.PaneSnapshot{Data: []byte("should not use")}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("history"), nil
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
		ID:          TabID("tab-shared-reattach"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "session-shared",
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
		t.Fatal("expected shared-client session to skip pre-attach full-pane snapshot")
	}
	if got := string(result.ScrollbackCapture); got != "history" {
		t.Fatalf("expected history-only fallback, got %q", got)
	}
	if result.Cols != 123 || result.Rows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", result.Cols, result.Rows)
	}
	assertCallOrder(t, calls, "state", "clients", "activity", "attach", "size", "scrollback")
	for _, call := range calls {
		if call == "snapshot" || call == "resize" {
			t.Fatalf("expected shared-client session to avoid pre-attach resize/snapshot, got %v", calls)
		}
	}
}

func TestReattachToSession_BusySessionCapturesHistoryAfterAttach(t *testing.T) {
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionActiveWithinFn := sessionActiveWithinFn
	oldSessionPaneSizeFn := sessionPaneSizeFn
	oldResizePaneToSizeFn := resizePaneToSizeFn
	oldCapturePaneSnapshotFn := capturePaneSnapshotFn
	oldCapturePaneFn := capturePaneFn
	oldCreateAgentWithTagsFn := createAgentWithTagsFn
	defer func() {
		sessionStateForFn = oldSessionStateForFn
		sessionHasClientsFn = oldSessionHasClientsFn
		sessionActiveWithinFn = oldSessionActiveWithinFn
		sessionPaneSizeFn = oldSessionPaneSizeFn
		resizePaneToSizeFn = oldResizePaneToSizeFn
		capturePaneSnapshotFn = oldCapturePaneSnapshotFn
		capturePaneFn = oldCapturePaneFn
		createAgentWithTagsFn = oldCreateAgentWithTagsFn
	}()

	calls := make([]string, 0, 4)
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionHasClientsFn = func(sessionName string, opts tmux.Options) (bool, error) {
		calls = append(calls, "clients")
		return false, nil
	}
	sessionActiveWithinFn = func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
		calls = append(calls, "activity")
		return true, nil
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
		return tmux.PaneSnapshot{Data: []byte("should not use")}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("history"), nil
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

	msg := m.reattachToSession(ws, TabID("tab-restore-busy"), "codex", "session-busy")()
	result, ok := msg.(ptyTabReattachResult)
	if !ok {
		t.Fatalf("expected ptyTabReattachResult, got %T", msg)
	}
	if result.CaptureFullPane {
		t.Fatal("expected busy restore reattach to skip authoritative pane snapshot")
	}
	if got := string(result.ScrollbackCapture); got != "history" {
		t.Fatalf("expected history-only fallback, got %q", got)
	}
	if result.Cols != 123 || result.Rows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", result.Cols, result.Rows)
	}
	assertCallOrder(t, calls, "state", "clients", "activity", "attach", "size", "scrollback")
	for _, call := range calls {
		if call == "snapshot" || call == "resize" {
			t.Fatalf("expected busy restore reattach to avoid pre-attach resize/snapshot, got %v", calls)
		}
	}
}
