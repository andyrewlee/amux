package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
)

// livePTYTab returns a center tab whose Agent.Terminal is a zero-value
// *pty.Terminal. A freshly constructed terminal has closed==false, so
// IsClosed() reports false and updatePTYStopped takes the termAlive
// restart-backoff branch. This is the same fake the sidebar restart tests
// (terminal_update_pty_test.go) rely on.
func livePTYTab(id TabID, ws *data.Workspace) *Tab {
	return &Tab{
		ID:        id,
		Assistant: "codex",
		Workspace: ws,
		Agent:     &appPty.Agent{Terminal: &appPty.Terminal{}},
		Running:   true,
	}
}

func TestUpdatePTYStopped_RestartSchedulesTickUnderLimit(t *testing.T) {
	if (&appPty.Terminal{}).IsClosed() {
		t.Fatal("precondition: a zero-value *pty.Terminal must report IsClosed()==false")
	}

	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := livePTYTab(TabID("tab-restart"), ws)
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	// Each of the first ptyRestartMax stops must schedule a restart tick and
	// leave the tab attached. Backoff doubles until it saturates at the cap,
	// so it must stay positive and non-decreasing across calls.
	lastBackoff := time.Duration(-1) // sentinel below any real backoff
	for i := 0; i < ptyRestartMax; i++ {
		cmd := m.updatePTYStopped(PTYStopped{WorkspaceID: wsID, TabID: tab.ID})
		if cmd == nil {
			t.Fatalf("call %d: expected a non-nil restart tick cmd while under the limit", i+1)
		}
		if i == 0 {
			// Drive the first tick (shortest backoff) to prove the batch
			// really carries a PTYRestart for this tab, not just any cmd.
			var restart PTYRestart
			found := false
			for _, msg := range drainBatch(cmd) {
				if r, ok := msg.(PTYRestart); ok {
					restart = r
					found = true
					break
				}
			}
			if !found {
				t.Fatal("expected the restart tick to produce a PTYRestart message")
			}
			if restart.WorkspaceID != wsID || restart.TabID != tab.ID {
				t.Fatalf("expected PTYRestart for %s/%s, got %+v", wsID, tab.ID, restart)
			}
		}
		if tab.Detached {
			t.Fatalf("call %d: expected Detached==false while restarting, got true", i+1)
		}
		if !tab.Running {
			t.Fatalf("call %d: expected Running to stay true while restarting", i+1)
		}
		got := tab.RestartBackoff
		if got <= 0 {
			t.Fatalf("call %d: expected positive backoff, got %v", i+1, got)
		}
		if got < lastBackoff {
			t.Fatalf("call %d: expected backoff to grow or hold, got %v after %v", i+1, got, lastBackoff)
		}
		lastBackoff = got
	}
}

func TestUpdatePTYStopped_DetachesAfterRestartLimit(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := livePTYTab(TabID("tab-restart-limit"), ws)
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	// Exhaust the restart budget; these all schedule ticks (not driven).
	for i := 0; i < ptyRestartMax; i++ {
		if cmd := m.updatePTYStopped(PTYStopped{WorkspaceID: wsID, TabID: tab.ID}); cmd == nil {
			t.Fatalf("call %d: expected restart tick while under the limit", i+1)
		}
	}
	if tab.Detached {
		t.Fatal("expected tab to remain attached up to the restart limit")
	}

	// One more stop within the window exceeds ptyRestartMax: the handler
	// gives up, marks the tab detached, and emits TabStateChanged with no
	// restart tick.
	cmd := m.updatePTYStopped(PTYStopped{WorkspaceID: wsID, TabID: tab.ID})
	if cmd == nil {
		t.Fatal("expected a TabStateChanged cmd once the restart limit is exceeded")
	}
	if !tab.Detached {
		t.Fatal("expected Detached==true after exceeding the restart limit")
	}
	if tab.Running {
		t.Fatal("expected Running==false after exceeding the restart limit")
	}
	stateChanged := false
	for _, msg := range drainBatch(cmd) {
		switch got := msg.(type) {
		case PTYRestart:
			t.Fatalf("expected no restart tick after the limit, got %+v", got)
		case messages.TabStateChanged:
			if got.WorkspaceID != wsID || got.TabID != string(tab.ID) {
				t.Fatalf("expected TabStateChanged for %s/%s, got %+v", wsID, tab.ID, got)
			}
			stateChanged = true
		}
	}
	if !stateChanged {
		t.Fatal("expected TabStateChanged after exceeding the restart limit")
	}
}
