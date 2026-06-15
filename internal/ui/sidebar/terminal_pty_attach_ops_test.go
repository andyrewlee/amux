package sidebar

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

// registerActiveTab installs a single terminal tab for the workspace and makes
// it the active tab, mirroring the setup used across the reattach tests.
func registerActiveTab(m *TerminalModel, ws *data.Workspace, tab *TerminalTab) {
	wsID := string(ws.ID())
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.workspace = ws
}

func TestSessionHistoryCaptureSize(t *testing.T) {
	tests := []struct {
		name         string
		paneCols     int
		paneRows     int
		paneHasSize  bool
		paneErr      error
		fallbackCols int
		fallbackRows int
		wantCols     int
		wantRows     int
	}{
		{
			name:         "uses live pane size when available",
			paneCols:     123,
			paneRows:     45,
			paneHasSize:  true,
			fallbackCols: 80,
			fallbackRows: 24,
			wantCols:     123,
			wantRows:     45,
		},
		{
			name:         "falls back when pane size query errors",
			paneCols:     123,
			paneRows:     45,
			paneHasSize:  true,
			paneErr:      errors.New("no such session"),
			fallbackCols: 80,
			fallbackRows: 24,
			wantCols:     80,
			wantRows:     24,
		},
		{
			name:         "falls back when pane size is not reported",
			paneCols:     123,
			paneRows:     45,
			paneHasSize:  false,
			fallbackCols: 80,
			fallbackRows: 24,
			wantCols:     80,
			wantRows:     24,
		},
		{
			name:         "falls back when pane cols are non-positive",
			paneCols:     0,
			paneRows:     45,
			paneHasSize:  true,
			fallbackCols: 80,
			fallbackRows: 24,
			wantCols:     80,
			wantRows:     24,
		},
		{
			name:         "falls back when pane rows are non-positive",
			paneCols:     123,
			paneRows:     0,
			paneHasSize:  true,
			fallbackCols: 80,
			fallbackRows: 24,
			wantCols:     80,
			wantRows:     24,
		},
		{
			name:         "falls back when pane rows are negative",
			paneCols:     123,
			paneRows:     -5,
			paneHasSize:  true,
			fallbackCols: 17,
			fallbackRows: 9,
			wantCols:     17,
			wantRows:     9,
		},
	}

	oldSessionPaneSizeFn := sessionPaneSizeFn
	defer func() { sessionPaneSizeFn = oldSessionPaneSizeFn }()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotSession string
			var gotOpts tmux.Options
			sessionPaneSizeFn = func(sessionName string, opts tmux.Options) (int, int, bool, error) {
				gotSession = sessionName
				gotOpts = opts
				return tt.paneCols, tt.paneRows, tt.paneHasSize, tt.paneErr
			}

			opts := tmux.DefaultOptions()
			cols, rows := sessionHistoryCaptureSize("session-xyz", tt.fallbackCols, tt.fallbackRows, opts)
			if cols != tt.wantCols || rows != tt.wantRows {
				t.Fatalf("expected %dx%d, got %dx%d", tt.wantCols, tt.wantRows, cols, rows)
			}
			if gotSession != "session-xyz" {
				t.Fatalf("expected pane size query for session %q, got %q", "session-xyz", gotSession)
			}
			if gotOpts != opts {
				t.Fatalf("expected pane size query to forward tmux options, got %+v", gotOpts)
			}
		})
	}
}

func TestDetachActiveTab_NoActiveTab(t *testing.T) {
	m := NewTerminalModel()
	// No workspace and no tabs registered: getActiveTab returns nil.
	if cmd := m.DetachActiveTab(); cmd != nil {
		t.Fatalf("expected nil cmd when there is no active tab, got %T", cmd)
	}
}

func TestDetachActiveTab_NilState(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	registerActiveTab(m, ws, &TerminalTab{ID: generateTerminalTabID(), State: nil})

	if cmd := m.DetachActiveTab(); cmd != nil {
		t.Fatalf("expected nil cmd when active tab has no state, got %T", cmd)
	}
}

func TestDetachActiveTab_DetachesRunningSession(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	ts := &TerminalState{
		SessionName:  "session-1",
		Running:      true,
		Detached:     false,
		UserDetached: false,
	}
	registerActiveTab(m, ws, &TerminalTab{ID: generateTerminalTabID(), State: ts})

	cmd := m.DetachActiveTab()
	if cmd != nil {
		t.Fatalf("expected nil cmd from DetachActiveTab, got %T", cmd)
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.Running {
		t.Fatal("expected Running to be cleared after detach")
	}
	if !ts.Detached {
		t.Fatal("expected Detached to be set after detach")
	}
	if !ts.UserDetached {
		t.Fatal("expected UserDetached to be set for a user-initiated detach")
	}
	if ts.Terminal != nil {
		t.Fatal("expected Terminal to be cleared after detach")
	}
}

func TestReattachActiveTab_NoActiveTab(t *testing.T) {
	m := NewTerminalModel()
	if cmd := m.ReattachActiveTab(); cmd != nil {
		t.Fatalf("expected nil cmd when there is no active tab, got %T", cmd)
	}
}

func TestReattachActiveTab_NilWorkspace(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	registerActiveTab(m, ws, &TerminalTab{
		ID:    generateTerminalTabID(),
		State: &TerminalState{SessionName: "session-1", Detached: true},
	})
	// Clear the workspace after registering the tab so the active tab exists
	// but the workspace guard trips.
	m.workspace = nil

	if cmd := m.ReattachActiveTab(); cmd != nil {
		t.Fatalf("expected nil cmd when workspace is nil, got %T", cmd)
	}
}

func TestReattachActiveTab_RunningEmitsToast(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	registerActiveTab(m, ws, &TerminalTab{
		ID:    generateTerminalTabID(),
		State: &TerminalState{SessionName: "session-1", Running: true},
	})

	cmd := m.ReattachActiveTab()
	if cmd == nil {
		t.Fatal("expected a toast cmd when the terminal is still running")
	}
	toast, ok := cmd().(messages.Toast)
	if !ok {
		t.Fatalf("expected messages.Toast, got %T", cmd())
	}
	if toast.Message != "Terminal is still running" {
		t.Fatalf("unexpected toast message: %q", toast.Message)
	}
	if toast.Level != messages.ToastInfo {
		t.Fatalf("expected info-level toast, got %q", toast.Level)
	}
}

func TestReattachActiveTab_DetachedReturnsAttachCmd(t *testing.T) {
	withReattachSeams(t, "history only")

	m := NewTerminalModel()
	m.width = 20
	m.height = 5
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	tabID := generateTerminalTabID()
	registerActiveTab(m, ws, &TerminalTab{
		ID:    tabID,
		State: &TerminalState{SessionName: "session-reattach", Running: false, Detached: true},
	})

	cmd := m.ReattachActiveTab()
	if cmd == nil {
		t.Fatal("expected a non-nil attach cmd for a detached session")
	}
	msg := cmd()
	reattach, ok := msg.(SidebarTerminalReattachResult)
	if !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
	if reattach.SessionName != "session-reattach" {
		t.Fatalf("expected stored session name to be reused, got %q", reattach.SessionName)
	}
	if reattach.TabID != tabID {
		t.Fatalf("expected reattach result to target the active tab, got %q", reattach.TabID)
	}
}

func TestReattachActiveTab_EmptySessionNameDerivesFromIDs(t *testing.T) {
	withReattachSeams(t, "history only")

	m := NewTerminalModel()
	m.width = 20
	m.height = 5
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	tabID := generateTerminalTabID()
	registerActiveTab(m, ws, &TerminalTab{
		ID:    tabID,
		State: &TerminalState{SessionName: "", Running: false, Detached: true},
	})

	cmd := m.ReattachActiveTab()
	if cmd == nil {
		t.Fatal("expected a non-nil attach cmd when deriving the session name")
	}
	msg := cmd()
	reattach, ok := msg.(SidebarTerminalReattachResult)
	if !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
	want := tmux.SessionName("amux", string(ws.ID()), string(tabID))
	if reattach.SessionName != want {
		t.Fatalf("expected derived session name %q, got %q", want, reattach.SessionName)
	}
	if want == "" {
		t.Fatal("derived session name should not be empty")
	}
}

func TestRestartActiveTab_NoActiveTab(t *testing.T) {
	m := NewTerminalModel()
	if cmd := m.RestartActiveTab(); cmd != nil {
		t.Fatalf("expected nil cmd when there is no active tab, got %T", cmd)
	}
}

func TestRestartActiveTab_NilState(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	registerActiveTab(m, ws, &TerminalTab{ID: generateTerminalTabID(), State: nil})

	if cmd := m.RestartActiveTab(); cmd != nil {
		t.Fatalf("expected nil cmd when active tab has no state, got %T", cmd)
	}
}

func TestRestartActiveTab_NilWorkspace(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	registerActiveTab(m, ws, &TerminalTab{
		ID:    generateTerminalTabID(),
		State: &TerminalState{SessionName: "session-1", Detached: true},
	})
	m.workspace = nil

	if cmd := m.RestartActiveTab(); cmd != nil {
		t.Fatalf("expected nil cmd when workspace is nil, got %T", cmd)
	}
}

func TestRestartActiveTab_RunningEmitsToastWithoutKillingSession(t *testing.T) {
	// A still-running terminal must short-circuit before RestartActiveTab reaches
	// the (unmocked) tmux.KillSession exec. We assert the toast and that the
	// session state was left untouched (no detach side effects).
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	ts := &TerminalState{SessionName: "session-1", Running: true, Detached: false}
	registerActiveTab(m, ws, &TerminalTab{ID: generateTerminalTabID(), State: ts})

	cmd := m.RestartActiveTab()
	if cmd == nil {
		t.Fatal("expected a toast cmd when the terminal is still running")
	}
	toast, ok := cmd().(messages.Toast)
	if !ok {
		t.Fatalf("expected messages.Toast, got %T", cmd())
	}
	if toast.Message != "Terminal is still running" {
		t.Fatalf("unexpected toast message: %q", toast.Message)
	}
	if toast.Level != messages.ToastInfo {
		t.Fatalf("expected info-level toast, got %q", toast.Level)
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !ts.Running || ts.Detached {
		t.Fatalf("expected running session to be left untouched, got Running=%v Detached=%v", ts.Running, ts.Detached)
	}
}

// withReattachSeams installs no-op/successful tmux seams sufficient for a
// "reattach" attachToSession command to run without touching real processes,
// returning the given scrollback as the pane history. Every tmux seam reached
// on the bootstrap/history-only path is stubbed (including the activity,
// creation-time, and pane-id probes that fire whenever a live session is
// present), so the command never shells out to a real tmux. Originals are
// restored on test cleanup.
func withReattachSeams(t *testing.T, scrollback string) {
	t.Helper()
	oldEnsureTmuxAvailableFn := ensureTmuxAvailableFn
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionActiveWithinFn := sessionActiveWithinFn
	oldSessionCreatedAtFn := sessionCreatedAtFn
	oldSessionPaneIDFn := sessionPaneIDFn
	oldSessionPaneSnapshotInfoFn := sessionPaneSnapshotInfoFn
	oldSessionPaneSizeFn := sessionPaneSizeFn
	oldNewPTYWithSizeFn := newPTYWithSizeFn
	oldCapturePaneSnapshotFn := capturePaneSnapshotFn
	oldCapturePaneFn := capturePaneFn
	oldVerifyTerminalSessionTagsFn := verifyTerminalSessionTagsFn
	t.Cleanup(func() {
		ensureTmuxAvailableFn = oldEnsureTmuxAvailableFn
		sessionStateForFn = oldSessionStateForFn
		sessionHasClientsFn = oldSessionHasClientsFn
		sessionActiveWithinFn = oldSessionActiveWithinFn
		sessionCreatedAtFn = oldSessionCreatedAtFn
		sessionPaneIDFn = oldSessionPaneIDFn
		sessionPaneSnapshotInfoFn = oldSessionPaneSnapshotInfoFn
		sessionPaneSizeFn = oldSessionPaneSizeFn
		newPTYWithSizeFn = oldNewPTYWithSizeFn
		capturePaneSnapshotFn = oldCapturePaneSnapshotFn
		capturePaneFn = oldCapturePaneFn
		verifyTerminalSessionTagsFn = oldVerifyTerminalSessionTagsFn
	})

	ensureTmuxAvailableFn = func() error { return nil }
	sessionStateForFn = func(string, tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionHasClientsFn = func(string, tmux.Options) (bool, error) { return false, nil }
	// Stub the live-session probes (activity window, creation time, pane id)
	// so the bootstrap path stays entirely off real tmux even when a session
	// is reported present.
	sessionActiveWithinFn = func(string, time.Duration, tmux.Options) (bool, error) {
		return false, nil
	}
	sessionCreatedAtFn = func(string, tmux.Options) (int64, error) { return 0, nil }
	sessionPaneIDFn = func(string, tmux.Options) (string, error) { return "%0", nil }
	// Make the pre-attach snapshot ineligible so the command takes the
	// history-only path (no resize/snapshot exec, just capturePane).
	sessionPaneSnapshotInfoFn = func(string, tmux.Options) (int, int, bool, error) {
		return 0, 0, false, nil
	}
	sessionPaneSizeFn = func(string, tmux.Options) (int, int, bool, error) {
		return 80, 24, true, nil
	}
	capturePaneSnapshotFn = func(string, tmux.Options) (tmux.PaneSnapshot, error) {
		return tmux.PaneSnapshot{}, errors.New("not whole window")
	}
	capturePaneFn = func(string, tmux.Options) ([]byte, error) {
		return []byte(scrollback), nil
	}
	newPTYWithSizeFn = func(string, string, []string, uint16, uint16) (*pty.Terminal, error) {
		return &pty.Terminal{}, nil
	}
	verifyTerminalSessionTagsFn = func(string, tmux.SessionTags, tmux.Options) error { return nil }
}

// compile-time assurance the ops under test keep the (*TerminalModel) -> tea.Cmd
// signature the callers depend on.
var (
	_ func(*TerminalModel) tea.Cmd = (*TerminalModel).DetachActiveTab
	_ func(*TerminalModel) tea.Cmd = (*TerminalModel).ReattachActiveTab
	_ func(*TerminalModel) tea.Cmd = (*TerminalModel).RestartActiveTab
)
