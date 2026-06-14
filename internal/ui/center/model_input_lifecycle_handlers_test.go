package center

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
)

// findMsg returns the first message of type T in msgs, reporting whether one was
// found. It keeps the batch assertions below focused on the payload that matters
// rather than the (unspecified) ordering of the batched commands.
func findMsg[T any](msgs []tea.Msg) (T, bool) {
	for _, msg := range msgs {
		if typed, ok := msg.(T); ok {
			return typed, true
		}
	}
	var zero T
	return zero, false
}

// ---------------------------------------------------------------------------
// updateLaunchAgent
// ---------------------------------------------------------------------------

func TestUpdateLaunchAgent_NilWorkspaceReturnsError(t *testing.T) {
	m := newTestModel()

	got, cmd := m.updateLaunchAgent(messages.LaunchAgent{Assistant: "claude", Workspace: nil})
	if got != m {
		t.Fatal("expected the same model pointer to be returned")
	}
	if cmd == nil {
		t.Fatal("expected a command even when workspace is nil")
	}
	// createAgentTab with a nil workspace yields a pure error message (no tmux),
	// so running the command here does not touch any external process.
	errMsg, ok := cmd().(messages.Error)
	if !ok {
		t.Fatalf("expected messages.Error for nil workspace, got %T", cmd())
	}
	if errMsg.Context != "creating agent" {
		t.Fatalf("unexpected error context: %q", errMsg.Context)
	}
}

// ---------------------------------------------------------------------------
// updateOpenFileInVim
// ---------------------------------------------------------------------------

func TestUpdateOpenFileInVim_NilWorkspaceReturnsError(t *testing.T) {
	m := newTestModel()

	got, cmd := m.updateOpenFileInVim(messages.OpenFileInVim{Path: "main.go", Workspace: nil})
	if got != m {
		t.Fatal("expected the same model pointer to be returned")
	}
	if cmd == nil {
		t.Fatal("expected a command even when workspace is nil")
	}
	// createVimTab with a nil workspace yields a pure error message (no tmux).
	errMsg, ok := cmd().(messages.Error)
	if !ok {
		t.Fatalf("expected messages.Error for nil workspace, got %T", cmd())
	}
	if errMsg.Context != "creating vim viewer" {
		t.Fatalf("unexpected error context: %q", errMsg.Context)
	}
}

// ---------------------------------------------------------------------------
// updatePtyTabCreateResult
// ---------------------------------------------------------------------------

func TestUpdatePtyTabCreateResult_InvalidInputsReturnError(t *testing.T) {
	ws := newTestWorkspace("ws", "/repo/ws")
	agent := &appPty.Agent{Session: "amux-sess"}

	cases := []struct {
		name string
		msg  ptyTabCreateResult
	}{
		{
			name: "nil workspace",
			msg:  ptyTabCreateResult{Workspace: nil, Agent: agent, TabID: TabID("tab-1")},
		},
		{
			name: "nil agent",
			msg:  ptyTabCreateResult{Workspace: ws, Agent: nil, TabID: TabID("tab-1")},
		},
		{
			name: "missing tab id",
			msg:  ptyTabCreateResult{Workspace: ws, Agent: agent, TabID: TabID("")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel()
			got, cmd := m.updatePtyTabCreateResult(tc.msg)
			if got != m {
				t.Fatal("expected the same model pointer to be returned")
			}
			if cmd == nil {
				t.Fatal("expected an error command for invalid create result")
			}
			if _, ok := cmd().(messages.Error); !ok {
				t.Fatalf("expected messages.Error, got %T", cmd())
			}
			// No tab should have been registered on a rejected create result.
			if got := len(m.tabs.ByWorkspace[string(ws.ID())]); got != 0 {
				t.Fatalf("expected no tabs created on rejection, got %d", got)
			}
		})
	}
}

func TestUpdatePtyTabCreateResult_CreatesNewTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	// An Agent with a nil Terminal exercises the create path without a live PTY
	// (mirrors model_tabs_identity_test.go), so resizePTY/response-writer are
	// skipped while the tab is still registered and activated.
	got, cmd := m.updatePtyTabCreateResult(ptyTabCreateResult{
		Workspace: ws,
		Assistant: "codex",
		Agent:     &appPty.Agent{Session: "amux-create-sess"},
		TabID:     TabID("tab-created"),
		Rows:      24,
		Cols:      80,
		Activate:  true,
	})
	if got != m {
		t.Fatal("expected the same model pointer to be returned")
	}

	tabs := m.tabs.ByWorkspace[wsID]
	if len(tabs) != 1 {
		t.Fatalf("expected exactly one tab created, got %d", len(tabs))
	}
	tab := tabs[0]
	if tab.ID != TabID("tab-created") {
		t.Fatalf("expected new tab id preserved, got %q", tab.ID)
	}
	if tab.Assistant != "codex" {
		t.Fatalf("expected assistant codex, got %q", tab.Assistant)
	}
	if tab.SessionName != "amux-create-sess" {
		t.Fatalf("expected session name carried onto tab, got %q", tab.SessionName)
	}
	if !tab.Running {
		t.Fatal("expected freshly created tab to be Running")
	}
	if m.tabs.ActiveByWorkspace[wsID] != 0 {
		t.Fatalf("expected new tab to become active, got index %d", m.tabs.ActiveByWorkspace[wsID])
	}

	// The handler reports the new tab via messages.TabCreated.
	created, ok := cmd().(messages.TabCreated)
	if !ok {
		t.Fatalf("expected messages.TabCreated, got %T", cmd())
	}
	if created.Index != 0 {
		t.Fatalf("expected TabCreated index 0, got %d", created.Index)
	}
}

// ---------------------------------------------------------------------------
// updatePtyTabReattachFailed
// ---------------------------------------------------------------------------

func TestUpdatePtyTabReattachFailed_UnknownTabIsNoOp(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	m.tabs.ByWorkspace[wsID] = []*Tab{}

	got, cmd := m.updatePtyTabReattachFailed(ptyTabReattachFailed{
		WorkspaceID: wsID,
		TabID:       TabID("missing-tab"),
		Err:         errors.New("boom"),
	})
	if got != m {
		t.Fatal("expected same model pointer")
	}
	if cmd != nil {
		t.Fatal("expected nil command when the target tab is unknown")
	}
}

func TestUpdatePtyTabReattachFailed_MarksTabAndToasts(t *testing.T) {
	cases := []struct {
		name        string
		action      string
		stopped     bool
		wantLabel   string
		wantDetach  bool // expected Detached value after the failure
		startDetach bool
	}{
		{name: "default action reattach", action: "", stopped: false, wantLabel: "Reattach", startDetach: true, wantDetach: true},
		{name: "explicit reattach", action: "reattach", stopped: false, wantLabel: "Reattach", startDetach: true, wantDetach: true},
		{name: "restart action", action: "restart", stopped: false, wantLabel: "Restart", startDetach: true, wantDetach: true},
		{name: "stopped clears detached", action: "reattach", stopped: true, wantLabel: "Reattach", startDetach: true, wantDetach: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel()
			ws := newTestWorkspace("ws", "/repo/ws")
			wsID := string(ws.ID())
			tab := &Tab{
				ID:               TabID("tab-reattach"),
				Assistant:        "claude",
				Workspace:        ws,
				Running:          true,
				Detached:         tc.startDetach,
				reattachInFlight: true,
			}
			m.tabs.ByWorkspace[wsID] = []*Tab{tab}

			got, cmd := m.updatePtyTabReattachFailed(ptyTabReattachFailed{
				WorkspaceID: wsID,
				TabID:       tab.ID,
				Err:         errors.New("connection refused"),
				Stopped:     tc.stopped,
				Action:      tc.action,
			})
			if got != m {
				t.Fatal("expected same model pointer")
			}

			tab.mu.Lock()
			running, inFlight, detached := tab.Running, tab.reattachInFlight, tab.Detached
			tab.mu.Unlock()
			if running {
				t.Fatal("expected Running=false after reattach failure")
			}
			if inFlight {
				t.Fatal("expected reattachInFlight cleared after reattach failure")
			}
			if detached != tc.wantDetach {
				t.Fatalf("expected Detached=%v after failure, got %v", tc.wantDetach, detached)
			}

			msgs := drainBatch(cmd)
			if _, ok := findMsg[messages.TabStateChanged](msgs); !ok {
				t.Fatalf("expected a TabStateChanged message, got %+v", msgs)
			}
			toast, ok := findMsg[messages.Toast](msgs)
			if !ok {
				t.Fatalf("expected a Toast message, got %+v", msgs)
			}
			if toast.Level != messages.ToastWarning {
				t.Fatalf("expected warning toast, got %q", toast.Level)
			}
			wantPrefix := tc.wantLabel + " failed:"
			if len(toast.Message) < len(wantPrefix) || toast.Message[:len(wantPrefix)] != wantPrefix {
				t.Fatalf("expected toast prefixed %q, got %q", wantPrefix, toast.Message)
			}
		})
	}
}
