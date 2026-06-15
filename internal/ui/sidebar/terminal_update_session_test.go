package sidebar

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
)

// reattachFailedModel returns a model with a single detached, reattach-in-flight
// tab for the given workspace. The tab carries a live (zero-value) Terminal so
// teardown paths that close it are exercised. No tmux/PTY process is involved.
func reattachFailedModel(t *testing.T) (*TerminalModel, *data.Workspace, TerminalTabID, *TerminalState) {
	t.Helper()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	state := &TerminalState{
		SessionName:      "session-1",
		Running:          true,
		Detached:         true,
		reattachInFlight: true,
	}
	m := NewTerminalModel()
	m.workspace = ws
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, Name: "Terminal 1", State: state}}
	m.tabs.ActiveByWorkspace[wsID] = 0
	return m, ws, tabID, state
}

// --- handleReattachFailed -----------------------------------------------

func TestHandleReattachFailedClearsRunningAndInFlight(t *testing.T) {
	// A failed reattach must clear Running and reattachInFlight on the targeted
	// tab so the user can retry, regardless of the Stopped/Action fields.
	tests := []struct {
		name         string
		stopped      bool
		action       string
		wantDetached bool
		wantLabel    string
	}{
		{
			name:         "default reattach keeps detached set",
			stopped:      false,
			action:       "",
			wantDetached: true, // Detached only flips when Stopped is true.
			wantLabel:    "Reattach failed",
		},
		{
			name:         "explicit reattach action",
			stopped:      false,
			action:       "reattach",
			wantDetached: true,
			wantLabel:    "Reattach failed",
		},
		{
			name:         "restart action uses Restart label",
			stopped:      false,
			action:       "restart",
			wantDetached: true,
			wantLabel:    "Restart failed",
		},
		{
			name:         "stopped clears the detached flag",
			stopped:      true,
			action:       "reattach",
			wantDetached: false,
			wantLabel:    "Reattach failed",
		},
		{
			name:         "restart action while stopped",
			stopped:      true,
			action:       "restart",
			wantDetached: false,
			wantLabel:    "Restart failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, ws, tabID, state := reattachFailedModel(t)

			cmd := m.handleReattachFailed(SidebarTerminalReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         errors.New("boom"),
				Stopped:     tt.stopped,
				Action:      tt.action,
			})

			state.mu.Lock()
			running := state.Running
			inFlight := state.reattachInFlight
			detached := state.Detached
			state.mu.Unlock()

			if running {
				t.Fatal("expected Running cleared after a failed reattach")
			}
			if inFlight {
				t.Fatal("expected reattachInFlight cleared after a failed reattach")
			}
			if detached != tt.wantDetached {
				t.Fatalf("expected Detached=%v, got %v", tt.wantDetached, detached)
			}

			if cmd == nil {
				t.Fatal("expected a toast command from a failed reattach")
			}
			toast, ok := cmd().(messages.Toast)
			if !ok {
				t.Fatalf("expected messages.Toast, got %T", cmd())
			}
			if toast.Level != messages.ToastWarning {
				t.Fatalf("expected warning-level toast, got %q", toast.Level)
			}
			if !strings.HasPrefix(toast.Message, tt.wantLabel) {
				t.Fatalf("expected toast to start with %q, got %q", tt.wantLabel, toast.Message)
			}
			if !strings.Contains(toast.Message, "boom") {
				t.Fatalf("expected toast to surface the underlying error, got %q", toast.Message)
			}
		})
	}
}

func TestHandleReattachFailedNilErrStillToasts(t *testing.T) {
	// A nil error must not panic; the toast still renders with the formatted
	// <nil> verb so the failure is never silently swallowed.
	m, ws, tabID, _ := reattachFailedModel(t)

	cmd := m.handleReattachFailed(SidebarTerminalReattachFailed{
		WorkspaceID: string(ws.ID()),
		TabID:       tabID,
		Err:         nil,
	})

	if cmd == nil {
		t.Fatal("expected a toast command even with a nil error")
	}
	toast, ok := cmd().(messages.Toast)
	if !ok {
		t.Fatalf("expected messages.Toast, got %T", cmd())
	}
	if !strings.Contains(toast.Message, "<nil>") {
		t.Fatalf("expected nil error formatted into the toast, got %q", toast.Message)
	}
}

func TestHandleReattachFailedUnknownTabStillToasts(t *testing.T) {
	// When the targeted tab cannot be found (unknown workspace/tab, or nil
	// State), the state-mutation block must be skipped without panicking and a
	// toast must still be emitted so the user learns the operation failed.
	tests := []struct {
		name  string
		setup func(t *testing.T) (*TerminalModel, SidebarTerminalReattachFailed)
	}{
		{
			name: "unknown workspace",
			setup: func(t *testing.T) (*TerminalModel, SidebarTerminalReattachFailed) {
				m, _, tabID, _ := reattachFailedModel(t)
				return m, SidebarTerminalReattachFailed{
					WorkspaceID: "no-such-ws",
					TabID:       tabID,
					Err:         errors.New("nope"),
				}
			},
		},
		{
			name: "unknown tab id",
			setup: func(t *testing.T) (*TerminalModel, SidebarTerminalReattachFailed) {
				m, ws, _, _ := reattachFailedModel(t)
				return m, SidebarTerminalReattachFailed{
					WorkspaceID: string(ws.ID()),
					TabID:       generateTerminalTabID(),
					Err:         errors.New("nope"),
				}
			},
		},
		{
			name: "tab with nil State",
			setup: func(t *testing.T) (*TerminalModel, SidebarTerminalReattachFailed) {
				ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
				wsID := string(ws.ID())
				tabID := generateTerminalTabID()
				m := NewTerminalModel()
				m.workspace = ws
				m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, Name: "Terminal 1", State: nil}}
				m.tabs.ActiveByWorkspace[wsID] = 0
				return m, SidebarTerminalReattachFailed{
					WorkspaceID: wsID,
					TabID:       tabID,
					Err:         errors.New("nope"),
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, msg := tt.setup(t)
			cmd := m.handleReattachFailed(msg)
			if cmd == nil {
				t.Fatal("expected a toast command even when the tab is missing")
			}
			toast, ok := cmd().(messages.Toast)
			if !ok {
				t.Fatalf("expected messages.Toast, got %T", cmd())
			}
			if toast.Level != messages.ToastWarning {
				t.Fatalf("expected warning-level toast, got %q", toast.Level)
			}
			if !strings.Contains(toast.Message, "nope") {
				t.Fatalf("expected the error surfaced in the toast, got %q", toast.Message)
			}
		})
	}
}

func TestHandleReattachFailedRoutedThroughUpdate(t *testing.T) {
	// Update must dispatch SidebarTerminalReattachFailed to handleReattachFailed,
	// so routing the message clears the in-flight flag the same way a direct call
	// does.
	m, ws, tabID, state := reattachFailedModel(t)

	_, _ = m.Update(SidebarTerminalReattachFailed{
		WorkspaceID: string(ws.ID()),
		TabID:       tabID,
		Err:         errors.New("routed"),
		Stopped:     true,
	})

	state.mu.Lock()
	running := state.Running
	inFlight := state.reattachInFlight
	detached := state.Detached
	state.mu.Unlock()

	if running || inFlight {
		t.Fatal("expected Update to route the failure and clear Running/in-flight")
	}
	if detached {
		t.Fatal("expected Stopped failure routed through Update to clear Detached")
	}
}

// --- handleCreateFailed -------------------------------------------------

func TestHandleCreateFailedClearsPendingCreation(t *testing.T) {
	// A create failure must drop the pending-creation marker so the user can
	// retry, and must surface the error (non-nil command) when an error is set.
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	m := NewTerminalModel()
	m.pendingCreation[wsID] = true

	cmd := m.handleCreateFailed(SidebarTerminalCreateFailed{
		WorkspaceID: wsID,
		Err:         errors.New("spawn failed"),
	})

	if _, pending := m.pendingCreation[wsID]; pending {
		t.Fatal("expected the pending-creation marker cleared on failure")
	}
	if cmd == nil {
		t.Fatal("expected a non-nil error-reporting command for a real error")
	}
}

func TestHandleCreateFailedNilErrReturnsNilCmd(t *testing.T) {
	// common.ReportError short-circuits to nil on a nil error, but the pending
	// flag must still be cleared so a no-op create result does not wedge the
	// workspace into a permanent "creating" state.
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	m := NewTerminalModel()
	m.pendingCreation[wsID] = true

	cmd := m.handleCreateFailed(SidebarTerminalCreateFailed{WorkspaceID: wsID, Err: nil})

	if _, pending := m.pendingCreation[wsID]; pending {
		t.Fatal("expected the pending-creation marker cleared even with a nil error")
	}
	if cmd != nil {
		t.Fatalf("expected nil command when there is no error to report, got %T", cmd())
	}
}

func TestHandleCreateFailedUnknownWorkspaceIsSafe(t *testing.T) {
	// Deleting a missing key is a no-op in Go; the handler must not panic and
	// must leave unrelated pending markers untouched while still reporting the
	// error.
	other := "other-ws"
	m := NewTerminalModel()
	m.pendingCreation[other] = true

	cmd := m.handleCreateFailed(SidebarTerminalCreateFailed{
		WorkspaceID: "ghost-ws",
		Err:         errors.New("boom"),
	})

	if _, pending := m.pendingCreation[other]; !pending {
		t.Fatal("expected an unrelated pending marker to be preserved")
	}
	if cmd == nil {
		t.Fatal("expected an error-reporting command for a real error")
	}
}

func TestHandleCreateFailedRoutedThroughUpdate(t *testing.T) {
	// Update must dispatch SidebarTerminalCreateFailed to handleCreateFailed.
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	m := NewTerminalModel()
	m.pendingCreation[wsID] = true

	_, _ = m.Update(SidebarTerminalCreateFailed{WorkspaceID: wsID, Err: errors.New("routed")})

	if _, pending := m.pendingCreation[wsID]; pending {
		t.Fatal("expected Update to route the create failure and clear the pending marker")
	}
}

// --- handleWorkspaceDeleted ---------------------------------------------

func TestHandleWorkspaceDeletedNilWorkspaceIsNoop(t *testing.T) {
	// A delete message with a nil Workspace must short-circuit to a nil command
	// and must not touch any tracking maps.
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	m := NewTerminalModel()
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: generateTerminalTabID(), State: &TerminalState{}}}
	m.pendingCreation[wsID] = true

	cmd := m.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: nil})

	if cmd != nil {
		t.Fatalf("expected nil command for a nil workspace, got %T", cmd())
	}
	if _, ok := m.tabs.ByWorkspace[wsID]; !ok {
		t.Fatal("expected existing tabs to be left untouched for a nil-workspace message")
	}
	if _, ok := m.pendingCreation[wsID]; !ok {
		t.Fatal("expected pending-creation marker untouched for a nil-workspace message")
	}
}

func TestHandleWorkspaceDeletedTearsDownTabs(t *testing.T) {
	// Deleting a workspace must close each tab's terminal, clear Running, reset
	// the restart backoff, and drop the workspace from both tracking maps.
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())

	state1 := &TerminalState{Terminal: &appPty.Terminal{}, Running: true}
	state1.RestartBackoff = 5 * time.Second
	state2 := &TerminalState{Terminal: &appPty.Terminal{}, Running: true}
	state2.RestartBackoff = 9 * time.Second

	m := NewTerminalModel()
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{
		{ID: generateTerminalTabID(), Name: "Terminal 1", State: state1},
		{ID: generateTerminalTabID(), Name: "Terminal 2", State: state2},
	}
	m.tabs.ActiveByWorkspace[wsID] = 1
	m.pendingCreation[wsID] = true

	cmd := m.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: ws})

	if cmd != nil {
		t.Fatalf("expected nil command after teardown, got %T", cmd())
	}

	for i, st := range []*TerminalState{state1, state2} {
		st.mu.Lock()
		running := st.Running
		backoff := st.RestartBackoff
		term := st.Terminal
		st.mu.Unlock()
		if running {
			t.Fatalf("tab %d: expected Running cleared after workspace delete", i)
		}
		if backoff != 0 {
			t.Fatalf("tab %d: expected RestartBackoff reset to 0, got %d", i, backoff)
		}
		// Close() nils out the underlying pty file but the wrapper pointer is
		// retained; assert the wrapper reports itself closed instead.
		if term == nil {
			t.Fatalf("tab %d: expected the Terminal wrapper to remain set", i)
		}
		if !term.IsClosed() {
			t.Fatalf("tab %d: expected the closed terminal to report not running", i)
		}
	}

	if _, ok := m.tabs.ByWorkspace[wsID]; ok {
		t.Fatal("expected the workspace dropped from the tabs map")
	}
	if _, ok := m.tabs.ActiveByWorkspace[wsID]; ok {
		t.Fatal("expected the workspace dropped from the active-index map")
	}
	if _, ok := m.pendingCreation[wsID]; ok {
		t.Fatal("expected the pending-creation marker cleared")
	}
}

func TestHandleWorkspaceDeletedHandlesNilStateAndTerminal(t *testing.T) {
	// Tabs with a nil State, or a non-nil State without a live Terminal, must be
	// torn down without panicking on the nil guards.
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())

	stateNoTerm := &TerminalState{Running: true}
	stateNoTerm.RestartBackoff = 3 * time.Second

	m := NewTerminalModel()
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{
		{ID: generateTerminalTabID(), Name: "nil state", State: nil},
		{ID: generateTerminalTabID(), Name: "no terminal", State: stateNoTerm},
	}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.pendingCreation[wsID] = true

	cmd := m.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: ws})

	if cmd != nil {
		t.Fatalf("expected nil command, got %T", cmd())
	}
	stateNoTerm.mu.Lock()
	running := stateNoTerm.Running
	backoff := stateNoTerm.RestartBackoff
	stateNoTerm.mu.Unlock()
	if running {
		t.Fatal("expected Running cleared on the terminal-less tab")
	}
	if backoff != 0 {
		t.Fatalf("expected RestartBackoff reset on the terminal-less tab, got %d", backoff)
	}
	if _, ok := m.tabs.ByWorkspace[wsID]; ok {
		t.Fatal("expected the workspace dropped from the tabs map")
	}
}

func TestHandleWorkspaceDeletedUnknownWorkspaceIsSafe(t *testing.T) {
	// Deleting a workspace that has no tracked tabs must not panic and must leave
	// other workspaces' tabs intact while still clearing any (absent) pending
	// marker.
	tracked := data.NewWorkspace("keep", "main", "main", "/repo/keep", "/repo/keep")
	keepID := string(tracked.ID())
	gone := data.NewWorkspace("gone", "main", "main", "/repo/gone", "/repo/gone")

	keepState := &TerminalState{Running: true}
	m := NewTerminalModel()
	m.tabs.ByWorkspace[keepID] = []*TerminalTab{{ID: generateTerminalTabID(), State: keepState}}
	m.tabs.ActiveByWorkspace[keepID] = 0

	cmd := m.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: gone})

	if cmd != nil {
		t.Fatalf("expected nil command for an untracked workspace, got %T", cmd())
	}
	if _, ok := m.tabs.ByWorkspace[keepID]; !ok {
		t.Fatal("expected an unrelated workspace's tabs to be preserved")
	}
	keepState.mu.Lock()
	running := keepState.Running
	keepState.mu.Unlock()
	if !running {
		t.Fatal("expected an unrelated tab's Running flag left untouched")
	}
}

func TestHandleWorkspaceDeletedRoutedThroughUpdate(t *testing.T) {
	// Update must dispatch messages.WorkspaceDeleted to handleWorkspaceDeleted.
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	m := NewTerminalModel()
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{
		{ID: generateTerminalTabID(), State: &TerminalState{Running: true}},
	}
	m.tabs.ActiveByWorkspace[wsID] = 0

	_, _ = m.Update(messages.WorkspaceDeleted{Workspace: ws})

	if _, ok := m.tabs.ByWorkspace[wsID]; ok {
		t.Fatal("expected Update to route the delete and drop the workspace's tabs")
	}
}
