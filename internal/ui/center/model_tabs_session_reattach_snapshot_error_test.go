package center

import (
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestReattachActiveTab_SnapshotCommandErrorFallsBackToHistoryOnly(t *testing.T) {
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
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

	calls := make([]string, 0, 6)
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
		return false, nil
	}
	sessionCreatedAtFn = func(sessionName string, opts tmux.Options) (int64, error) {
		return 123, nil
	}
	sessionPaneIDFn = func(sessionName string, opts tmux.Options) (string, error) {
		return "%1", nil
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
		return tmux.PaneSnapshot{}, errors.New("snapshot command failed")
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
		ID:          TabID("tab-snapshot-error"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "session-snapshot-error",
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
		t.Fatal("expected snapshot error to disable authoritative full-pane restore")
	}
	if got := string(result.ScrollbackCapture); got != "history" {
		t.Fatalf("expected history-only fallback, got %q", got)
	}
	if result.Cols != 123 || result.Rows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", result.Cols, result.Rows)
	}
	assertCallOrder(t, calls, "state", "clients", "activity", "info", "resize", "snapshot", "resize", "attach", "size", "scrollback")
}
