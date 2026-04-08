package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestReattachActiveTab_CapturesSnapshotBeforeAttach(t *testing.T) {
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionPaneSizeFn := sessionPaneSizeFn
	oldSessionActiveWithinFn := sessionActiveWithinFn
	oldSessionCreatedAtFn := sessionCreatedAtFn
	oldSessionPaneIDFn := sessionPaneIDFn
	oldSessionPaneSnapshotInfoFn := sessionPaneSnapshotInfoFn
	oldResizePaneToSizeFn := resizePaneToSizeFn
	oldCapturePaneSnapshotFn := capturePaneSnapshotFn
	oldCapturePaneFn := capturePaneFn
	oldCreateAgentWithTagsFn := createAgentWithTagsFn
	defer func() {
		sessionStateForFn = oldSessionStateForFn
		sessionHasClientsFn = oldSessionHasClientsFn
		sessionPaneSizeFn = oldSessionPaneSizeFn
		sessionActiveWithinFn = oldSessionActiveWithinFn
		sessionCreatedAtFn = oldSessionCreatedAtFn
		sessionPaneIDFn = oldSessionPaneIDFn
		sessionPaneSnapshotInfoFn = oldSessionPaneSnapshotInfoFn
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
		return 77, 19, true, nil
	}
	resizePaneToSizeFn = func(sessionName string, cols, rows int, opts tmux.Options) error {
		calls = append(calls, "resize")
		return nil
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("frame"), Cols: 77, Rows: 19}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("fallback"), nil
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
		ID:          TabID("tab-reattach"),
		Assistant:   "codex",
		Workspace:   ws,
		SessionName: "session-1",
		Detached:    true,
	}
	m.workspace = ws
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0

	cmd := m.ReattachActiveTab()
	if cmd == nil {
		t.Fatal("expected reattach command")
	}
	msg := cmd()
	result, ok := msg.(ptyTabReattachResult)
	if !ok {
		t.Fatalf("expected ptyTabReattachResult, got %T", msg)
	}
	if !result.CaptureFullPane {
		t.Fatal("expected authoritative pane capture on live reattach")
	}
	if got := string(result.ScrollbackCapture); got != "frame" {
		t.Fatalf("expected captured pane snapshot, got %q", got)
	}
	if got := string(result.PostAttachScrollbackCapture); got != "fallback" {
		t.Fatalf("expected post-attach history reconciliation capture, got %q", got)
	}
	if result.SnapshotCols != 77 || result.SnapshotRows != 19 {
		t.Fatalf("expected actual snapshot size 77x19, got %dx%d", result.SnapshotCols, result.SnapshotRows)
	}
	assertCallOrder(t, calls, "state", "clients", "activity", "info", "resize", "snapshot", "attach", "size", "scrollback")
	snapshotCount := 0
	scrollbackIdx := -1
	scrollbackCount := 0
	attachIdx := -1
	for _, call := range calls {
		if call == "snapshot" {
			snapshotCount++
		}
	}
	for i, call := range calls {
		if call == "scrollback" {
			scrollbackCount++
			if scrollbackIdx == -1 {
				scrollbackIdx = i
			}
		}
		if call == "attach" {
			attachIdx = i
		}
	}
	if snapshotCount != 1 {
		t.Fatalf("expected a single resized snapshot capture, got %v", calls)
	}
	if scrollbackCount != 1 {
		t.Fatalf("expected a single post-attach delta capture, got %v", calls)
	}
	if scrollbackIdx < attachIdx {
		t.Fatalf("expected reconciliation capture after attach, got %v", calls)
	}
}

func TestReattachToSession_CapturesSnapshotBeforeAttach(t *testing.T) {
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionPaneSizeFn := sessionPaneSizeFn
	oldSessionActiveWithinFn := sessionActiveWithinFn
	oldSessionCreatedAtFn := sessionCreatedAtFn
	oldSessionPaneIDFn := sessionPaneIDFn
	oldSessionPaneSnapshotInfoFn := sessionPaneSnapshotInfoFn
	oldResizePaneToSizeFn := resizePaneToSizeFn
	oldCapturePaneSnapshotFn := capturePaneSnapshotFn
	oldCapturePaneFn := capturePaneFn
	oldCreateAgentWithTagsFn := createAgentWithTagsFn
	defer func() {
		sessionStateForFn = oldSessionStateForFn
		sessionHasClientsFn = oldSessionHasClientsFn
		sessionPaneSizeFn = oldSessionPaneSizeFn
		sessionActiveWithinFn = oldSessionActiveWithinFn
		sessionCreatedAtFn = oldSessionCreatedAtFn
		sessionPaneIDFn = oldSessionPaneIDFn
		sessionPaneSnapshotInfoFn = oldSessionPaneSnapshotInfoFn
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
		return 77, 19, true, nil
	}
	resizePaneToSizeFn = func(sessionName string, cols, rows int, opts tmux.Options) error {
		calls = append(calls, "resize")
		return nil
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("frame"), Cols: 77, Rows: 19}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("fallback"), nil
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

	msg := m.reattachToSession(ws, TabID("tab-restore"), "codex", "session-restore")()
	result, ok := msg.(ptyTabReattachResult)
	if !ok {
		t.Fatalf("expected ptyTabReattachResult, got %T", msg)
	}
	if !result.CaptureFullPane {
		t.Fatal("expected authoritative pane capture during restore reattach")
	}
	if got := string(result.ScrollbackCapture); got != "frame" {
		t.Fatalf("expected captured pane snapshot, got %q", got)
	}
	if got := string(result.PostAttachScrollbackCapture); got != "fallback" {
		t.Fatalf("expected post-attach history reconciliation capture, got %q", got)
	}
	if result.SnapshotCols != 77 || result.SnapshotRows != 19 {
		t.Fatalf("expected actual snapshot size 77x19, got %dx%d", result.SnapshotCols, result.SnapshotRows)
	}
	assertCallOrder(t, calls, "state", "clients", "activity", "info", "resize", "snapshot", "attach", "size", "scrollback")
	snapshotCount := 0
	scrollbackIdx := -1
	scrollbackCount := 0
	attachIdx := -1
	for _, call := range calls {
		if call == "snapshot" {
			snapshotCount++
		}
	}
	for i, call := range calls {
		if call == "scrollback" {
			scrollbackCount++
			if scrollbackIdx == -1 {
				scrollbackIdx = i
			}
		}
		if call == "attach" {
			attachIdx = i
		}
	}
	if snapshotCount != 1 {
		t.Fatalf("expected a single resized snapshot capture, got %v", calls)
	}
	if scrollbackCount != 1 {
		t.Fatalf("expected a single post-attach delta capture, got %v", calls)
	}
	if scrollbackIdx < attachIdx {
		t.Fatalf("expected reconciliation capture after attach, got %v", calls)
	}
}

func assertCallOrder(t *testing.T, calls []string, expected ...string) {
	t.Helper()
	last := -1
	for _, want := range expected {
		idx := -1
		for i := last + 1; i < len(calls); i++ {
			call := calls[i]
			if call == want {
				idx = i
				break
			}
		}
		if idx == -1 {
			t.Fatalf("expected call %q, got %v", want, calls)
		}
		last = idx
	}
}
