package center

import (
	"testing"

	appPty "github.com/andyrewlee/amux/internal/pty"
)

// TestUpdatePtyTabReattachResult_RejectsResultForDetachedTab proves a reattach
// result that lands after the user explicitly detached the tab does not
// resurrect it, and the freshly created agent is released rather than leaked.
func TestUpdatePtyTabReattachResult_RejectsResultForDetachedTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-detach-race"),
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "amux-ws-sess",
		Detached:    true,  // user detached...
		Running:     false, // ...
		// ...which cleared the in-flight flag (detachTab does this).
		reattachInFlight: false,
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0

	staleAgent := &appPty.Agent{Workspace: ws, Session: "amux-ws-sess"}
	m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Agent:       staleAgent,
	})

	tab.mu.Lock()
	detached, running := tab.Detached, tab.Running
	agent := tab.Agent
	tab.mu.Unlock()
	if !detached || running {
		t.Fatalf("a detached tab was resurrected by a stale reattach: Detached=%v Running=%v", detached, running)
	}
	if agent != nil {
		t.Fatal("the stale agent must not be bound to the detached tab")
	}
}

// TestUpdatePtyTabReattachResult_AppliesForLiveReattach proves a live reattach
// (reattachInFlight=true) still applies even when the tab is Detached, so the
// rejection guard does not break a legitimate reattach of a detached tab.
func TestUpdatePtyTabReattachResult_AppliesForLiveReattach(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:               TabID("tab-live-reattach"),
		Assistant:        "claude",
		Workspace:        ws,
		SessionName:      "amux-ws-sess",
		Detached:         true,
		Running:          false,
		reattachInFlight: true, // a reattach IS in flight
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0

	agent := &appPty.Agent{Workspace: ws, Session: "amux-ws-sess"}
	m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Agent:       agent,
	})

	tab.mu.Lock()
	running, detached := tab.Running, tab.Detached
	tab.mu.Unlock()
	if !running || detached {
		t.Fatalf("a live reattach should apply: Running=%v Detached=%v", running, detached)
	}
}

// TestUpdatePtyTabReattachResult_AppliesForDetachedRestart proves an explicit
// restart result can revive a detached tab while its restart is still in flight.
func TestUpdatePtyTabReattachResult_AppliesForDetachedRestart(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:               TabID("tab-detached-restart"),
		Assistant:        "claude",
		Workspace:        ws,
		SessionName:      "amux-ws-sess",
		Detached:         true,
		Running:          false,
		reattachInFlight: true,
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0

	agent := &appPty.Agent{Workspace: ws, Session: "amux-ws-sess"}
	m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Agent:       agent,
	})

	tab.mu.Lock()
	running, detached := tab.Running, tab.Detached
	gotAgent := tab.Agent
	tab.mu.Unlock()
	if !running || detached {
		t.Fatalf("a detached restart should apply: Running=%v Detached=%v", running, detached)
	}
	if gotAgent != agent {
		t.Fatal("restart agent was not bound to the tab")
	}
}

func TestRestartActiveTabMarksRestartInFlight(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-restart-inflight"),
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "amux-ws-sess",
		Detached:    true,
		Running:     false,
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.workspace = ws

	cmd := m.RestartActiveTab()
	if cmd == nil {
		t.Fatal("expected restart command for detached tab")
	}

	tab.mu.Lock()
	inFlight := tab.reattachInFlight
	tab.mu.Unlock()
	if !inFlight {
		t.Fatal("expected restart to mark reattachInFlight")
	}
}

// TestUpdatePtyTabReattachResult_ClosesAgentWhenTabGone proves a reattach
// result that lands after its tab was closed (getTabByID returns nil) releases
// the freshly created agent instead of leaking its PTY.
func TestUpdatePtyTabReattachResult_ClosesAgentWhenTabGone(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	// No tab registered for wsID: the tab was closed while reattach was in flight.

	term, err := appPty.New("cat >/dev/null", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("creating terminal: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })

	orphanAgent := &appPty.Agent{Workspace: ws, Session: "amux-ws-sess", Terminal: term}
	m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID: wsID,
		TabID:       TabID("tab-closed-mid-reattach"),
		Agent:       orphanAgent,
	})

	if !term.IsClosed() {
		t.Fatal("agent terminal must be closed when the reattach result has no tab to apply to")
	}
}
