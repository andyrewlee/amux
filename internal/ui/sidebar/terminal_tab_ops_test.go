package sidebar

import (
	"regexp"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

// ansiEscape matches CSI/SGR escape sequences emitted by VTerm.Render so tests
// can assert on the visible text of a rendered frame.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// visibleText strips ANSI escapes and surrounding whitespace from a rendered
// terminal frame, leaving only the glyphs a user would see.
func visibleText(rendered string) string {
	return strings.TrimSpace(ansiEscape.ReplaceAllString(rendered, ""))
}

// All target functions in terminal_tab_ops.go either operate purely on
// in-memory model state or RETURN a tea.Cmd closure (which is only invoked by
// the Bubble Tea runtime). These tests exercise the synchronous, observable
// behavior of every function and deliberately never invoke a returned command
// whose closure would exec tmux. closeSessionIfUnattached is the one function
// whose closure is invoked here, but only with an empty session name, which
// short-circuits before any tmux call (see SessionHasClients).

func TestSetWorkspaceCreatesTabs(t *testing.T) {
	tests := []struct {
		name        string
		ws          *data.Workspace
		seedTab     bool
		preMarkBusy bool
		wantCmd     bool
		wantPending bool
		wantWsNil   bool
	}{
		{
			name:      "nil workspace clears and returns no command",
			ws:        nil,
			wantCmd:   false,
			wantWsNil: true,
		},
		{
			name:        "fresh workspace schedules creation",
			ws:          &data.Workspace{Repo: "/repo", Root: "/repo/ws"},
			wantCmd:     true,
			wantPending: true,
		},
		{
			name:    "existing tabs short-circuit creation",
			ws:      &data.Workspace{Repo: "/repo", Root: "/repo/ws"},
			seedTab: true,
			wantCmd: false,
		},
		{
			name:        "pending creation in progress returns no command",
			ws:          &data.Workspace{Repo: "/repo", Root: "/repo/ws"},
			preMarkBusy: true,
			wantCmd:     false,
			wantPending: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTerminalModel()
			if tt.seedTab {
				m.setWorkspace(tt.ws)
				seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))
			}
			if tt.preMarkBusy {
				m.setWorkspace(tt.ws)
				m.pendingCreation[m.workspaceID()] = true
				m.setWorkspace(nil)
			}

			cmd := m.SetWorkspace(tt.ws)

			if tt.wantWsNil {
				if m.workspace != nil {
					t.Fatalf("expected workspace cleared to nil, got %+v", m.workspace)
				}
			} else if m.workspace != tt.ws {
				t.Fatal("expected workspace pointer stored")
			}
			if (cmd != nil) != tt.wantCmd {
				t.Fatalf("SetWorkspace returned cmd!=nil=%v, want %v", cmd != nil, tt.wantCmd)
			}
			if tt.ws != nil {
				got := m.pendingCreation[m.workspaceID()]
				if got != tt.wantPending {
					t.Fatalf("pendingCreation = %v, want %v", got, tt.wantPending)
				}
			}
		})
	}
}

func TestSetWorkspacePreviewDoesNotCreateTabs(t *testing.T) {
	m := NewTerminalModel()
	ws := &data.Workspace{Repo: "/repo", Root: "/repo/ws"}

	m.SetWorkspacePreview(ws)

	if m.workspace != ws {
		t.Fatal("SetWorkspacePreview did not store the workspace pointer")
	}
	if got := len(m.getTabs()); got != 0 {
		t.Fatalf("expected no tabs created by preview, got %d", got)
	}
	if m.pendingCreation[m.workspaceID()] {
		t.Fatal("SetWorkspacePreview must not schedule tab creation")
	}
}

func TestSetWorkspacePreviewAcceptsNil(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})

	m.SetWorkspacePreview(nil)

	if m.workspace != nil {
		t.Fatal("expected workspace cleared by preview(nil)")
	}
	if m.workspaceID() != "" {
		t.Fatalf("expected empty workspace id, got %q", m.workspaceID())
	}
}

func TestEnsureTerminalTab(t *testing.T) {
	tests := []struct {
		name        string
		setWs       bool
		seedTab     bool
		preMarkBusy bool
		wantCmd     bool
		wantPending bool
	}{
		{
			name:    "no workspace returns no command",
			setWs:   false,
			wantCmd: false,
		},
		{
			name:        "fresh workspace creates a tab",
			setWs:       true,
			wantCmd:     true,
			wantPending: true,
		},
		{
			name:    "existing tabs short-circuit",
			setWs:   true,
			seedTab: true,
			wantCmd: false,
		},
		{
			name:        "creation already pending returns no command",
			setWs:       true,
			preMarkBusy: true,
			wantCmd:     false,
			wantPending: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTerminalModel()
			if tt.setWs {
				m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
			}
			if tt.seedTab {
				seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))
			}
			if tt.preMarkBusy {
				m.pendingCreation[m.workspaceID()] = true
			}

			cmd := m.EnsureTerminalTab()

			if (cmd != nil) != tt.wantCmd {
				t.Fatalf("EnsureTerminalTab cmd!=nil=%v, want %v", cmd != nil, tt.wantCmd)
			}
			if tt.setWs {
				if got := m.pendingCreation[m.workspaceID()]; got != tt.wantPending {
					t.Fatalf("pendingCreation = %v, want %v", got, tt.wantPending)
				}
			}
		})
	}
}

func TestCreateNewTab(t *testing.T) {
	t.Run("no workspace returns no command", func(t *testing.T) {
		m := NewTerminalModel()
		if cmd := m.CreateNewTab(); cmd != nil {
			t.Fatal("expected nil command without a workspace")
		}
	})

	t.Run("workspace returns a creation command without marking pending", func(t *testing.T) {
		m := NewTerminalModel()
		m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})

		cmd := m.CreateNewTab()
		if cmd == nil {
			t.Fatal("expected a non-nil creation command")
		}
		// CreateNewTab is the explicit "+" path; unlike SetWorkspace/
		// EnsureTerminalTab it does not flip pendingCreation.
		if m.pendingCreation[m.workspaceID()] {
			t.Fatal("CreateNewTab must not set pendingCreation")
		}
	})

	t.Run("works even when tabs already exist", func(t *testing.T) {
		m := NewTerminalModel()
		m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
		seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))

		if cmd := m.CreateNewTab(); cmd == nil {
			t.Fatal("expected CreateNewTab to return a command even with existing tabs")
		}
	})
}

func TestCloseActiveTabNoTabs(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})

	if cmd := m.CloseActiveTab(); cmd != nil {
		t.Fatal("expected nil command when there are no tabs to close")
	}
}

func TestCloseActiveTabOutOfRangeActiveIndex(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))
	// Corrupt the active index past the end; CloseActiveTab must bail out and
	// leave the tab list untouched.
	m.setActiveTabIdx(7)

	if cmd := m.CloseActiveTab(); cmd != nil {
		t.Fatal("expected nil command for out-of-range active index")
	}
	if got := len(m.getTabs()); got != 1 {
		t.Fatalf("expected tab list untouched, got %d tabs", got)
	}
}

func TestCloseActiveTabRemovesTabAndStopsRunning(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	first := newWorkspaceTab(t, "Terminal 1")
	second := newWorkspaceTab(t, "Terminal 2")
	first.State.Running = true
	first.State.RestartBackoff = 3
	seedTabs(t, m, first, second)
	m.setActiveTabIdx(0)

	cmd := m.CloseActiveTab()

	// No SessionName was set on the closed tab, so there is no session to GC.
	if cmd != nil {
		t.Fatal("expected nil command when the closed tab has no session name")
	}
	tabs := m.getTabs()
	if len(tabs) != 1 {
		t.Fatalf("expected 1 remaining tab, got %d", len(tabs))
	}
	if tabs[0] != second {
		t.Fatal("expected the second tab to survive as the only remaining tab")
	}
	// The closed tab's state must have been torn down.
	first.State.mu.Lock()
	running := first.State.Running
	backoff := first.State.RestartBackoff
	first.State.mu.Unlock()
	if running {
		t.Fatal("expected closed tab Running flag cleared")
	}
	if backoff != 0 {
		t.Fatalf("expected closed tab RestartBackoff reset to 0, got %d", backoff)
	}
}

func TestCloseActiveTabClampsActiveIndexToLast(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	seedTabs(t, m,
		newWorkspaceTab(t, "Terminal 1"),
		newWorkspaceTab(t, "Terminal 2"),
	)
	// Closing the last tab while it is active must clamp the active index to the
	// new final index rather than dangling past the end.
	m.setActiveTabIdx(1)

	m.CloseActiveTab()

	if got := len(m.getTabs()); got != 1 {
		t.Fatalf("expected 1 remaining tab, got %d", got)
	}
	if got := m.getActiveTabIdx(); got != 0 {
		t.Fatalf("expected active index clamped to 0, got %d", got)
	}
}

func TestCloseActiveTabLastTabResetsActiveIndex(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))
	m.setActiveTabIdx(0)

	m.CloseActiveTab()

	if got := len(m.getTabs()); got != 0 {
		t.Fatalf("expected no tabs after closing the only tab, got %d", got)
	}
	if got := m.getActiveTabIdx(); got != 0 {
		t.Fatalf("expected active index reset to 0, got %d", got)
	}
}

func TestCloseActiveTabWithSessionNameReturnsCleanupCommand(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	tab := newWorkspaceTab(t, "Terminal 1")
	tab.State.SessionName = "amux-session-abc"
	seedTabs(t, m, tab)
	m.setActiveTabIdx(0)

	// A non-empty session name yields a GC command (the tmux call only happens
	// if the returned command is later executed by the runtime, which we do not
	// do here).
	if cmd := m.CloseActiveTab(); cmd == nil {
		t.Fatal("expected a cleanup command for a tab with a session name")
	}
	if got := len(m.getTabs()); got != 0 {
		t.Fatalf("expected the tab removed, got %d tabs", got)
	}
}

func TestCloseSessionIfUnattachedEmptySessionIsNoop(t *testing.T) {
	// The closure short-circuits on an empty session name before any tmux exec,
	// so it is safe to invoke directly here.
	cmd := closeSessionIfUnattached("", tmux.DefaultOptions())
	if cmd == nil {
		t.Fatal("expected a non-nil command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("expected nil message for empty session, got %#v", msg)
	}
}

func TestAddTerminalForHarness(t *testing.T) {
	t.Run("nil workspace is a no-op", func(t *testing.T) {
		m := NewTerminalModel()
		m.AddTerminalForHarness(nil)
		if m.workspace != nil {
			t.Fatal("expected workspace to remain nil")
		}
		if got := len(m.getTabs()); got != 0 {
			t.Fatalf("expected no tabs created, got %d", got)
		}
	})

	t.Run("creates a single running terminal tab", func(t *testing.T) {
		m := NewTerminalModel()
		ws := data.NewWorkspace("ws", "main", "main", "/repo", "/repo/ws")

		m.AddTerminalForHarness(ws)

		if m.workspace != ws {
			t.Fatal("expected workspace bound after AddTerminalForHarness")
		}
		tabs := m.getTabs()
		if len(tabs) != 1 {
			t.Fatalf("expected exactly 1 tab, got %d", len(tabs))
		}
		tab := tabs[0]
		if tab.Name != "Terminal 1" {
			t.Fatalf("expected tab name %q, got %q", "Terminal 1", tab.Name)
		}
		if tab.State == nil {
			t.Fatal("expected tab to carry terminal state")
		}
		if tab.State.VTerm == nil {
			t.Fatal("expected an in-memory VTerm for the harness tab")
		}
		if tab.State.Terminal != nil {
			t.Fatal("expected no live PTY for the harness tab")
		}
		if !tab.State.Running {
			t.Fatal("expected harness tab marked Running")
		}
		if got := m.getActiveTabIdx(); got != 0 {
			t.Fatalf("expected active index 0, got %d", got)
		}
	})

	t.Run("does not duplicate when a tab already exists", func(t *testing.T) {
		m := NewTerminalModel()
		ws := data.NewWorkspace("ws", "main", "main", "/repo", "/repo/ws")
		m.setWorkspace(ws)
		existing := newWorkspaceTab(t, "Terminal 1")
		seedTabs(t, m, existing)

		m.AddTerminalForHarness(ws)

		tabs := m.getTabs()
		if len(tabs) != 1 {
			t.Fatalf("expected the existing single tab preserved, got %d", len(tabs))
		}
		if tabs[0] != existing {
			t.Fatal("expected the pre-existing tab to be left untouched")
		}
	})
}

func TestWriteToTerminal(t *testing.T) {
	t.Run("no active terminal is a no-op", func(t *testing.T) {
		m := NewTerminalModel()
		m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
		// No tabs: getTerminal() is nil, so WriteToTerminal must early-return.
		m.WriteToTerminal([]byte("hello"))

		// The guard must not have allocated a tab as a side effect.
		if got := len(m.getTabs()); got != 0 {
			t.Fatalf("expected no tabs after writing without a terminal, got %d", got)
		}
	})

	t.Run("nil VTerm is a no-op", func(t *testing.T) {
		m := NewTerminalModel()
		m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
		tab := &TerminalTab{ID: generateTerminalTabID(), Name: "Terminal 1", State: &TerminalState{}}
		seedTabs(t, m, tab)
		// State.VTerm is nil; WriteToTerminal must not panic.
		m.WriteToTerminal([]byte("hello"))

		// The guard must not have allocated or mutated the VTerm: it stays nil.
		ts := m.getTerminal()
		if ts == nil {
			t.Fatal("expected the seeded tab's state to remain reachable")
		}
		ts.mu.Lock()
		stillNil := ts.VTerm == nil
		ts.mu.Unlock()
		if !stillNil {
			t.Fatal("expected VTerm to stay nil after a write with no VTerm")
		}
	})

	t.Run("writes bytes into the active VTerm", func(t *testing.T) {
		m := NewTerminalModel()
		m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
		seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))

		m.WriteToTerminal([]byte("ABC"))

		ts := m.getTerminal()
		ts.mu.Lock()
		rendered := ts.VTerm.Render()
		ts.mu.Unlock()
		if visible := visibleText(rendered); !strings.Contains(visible, "ABC") {
			t.Fatalf("expected written bytes to appear in render, got %q", visible)
		}
	})

	t.Run("empty payload is harmless", func(t *testing.T) {
		m := NewTerminalModel()
		m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
		seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))

		m.WriteToTerminal(nil)
		m.WriteToTerminal([]byte{})

		ts := m.getTerminal()
		ts.mu.Lock()
		rendered := ts.VTerm.Render()
		ts.mu.Unlock()
		// An empty write must not place any visible glyphs on the (blank) screen.
		// Render() still emits SGR/reset escapes and cell padding, so compare on
		// the visible text only.
		if visible := visibleText(rendered); visible != "" {
			t.Fatalf("expected no visible text after empty writes, got %q", visible)
		}
	})
}
