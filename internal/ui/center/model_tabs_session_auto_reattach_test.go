package center

import (
	"errors"
	"testing"

	appPty "github.com/andyrewlee/amux/internal/pty"
)

func TestAutoReattachActiveTabOnSelection_SkipsAttachedTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:        TabID("tab-a"),
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
		Detached:  false,
	}

	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	if cmd := m.autoReattachActiveTabOnSelection(); cmd != nil {
		t.Fatalf("expected nil command for attached tab")
	}
}

func TestAutoReattachActiveTabOnSelection_ReattachesDetachedTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:          TabID("tab-a"),
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "amux-ws-tab-a",
		Running:     false,
		Detached:    true,
	}

	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	if cmd := m.autoReattachActiveTabOnSelection(); cmd == nil {
		t.Fatalf("expected reattach command for detached tab")
	}
}

func TestReattachActiveTab_SkipsAttachedNonAssistantTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:        TabID("tab-viewer"),
		Assistant: "viewer",
		Workspace: ws,
		Running:   true,
		Detached:  false,
	}

	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	if cmd := m.ReattachActiveTab(); cmd != nil {
		t.Fatalf("expected nil command for already-attached non-assistant tab")
	}
}

func TestReattachActiveTab_DeduplicatesInFlight(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:          TabID("tab-reattach"),
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "amux-ws-tab-reattach",
		Detached:    true,
	}

	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	first := m.ReattachActiveTab()
	if first == nil {
		t.Fatalf("expected first reattach command")
	}
	tab.mu.Lock()
	inFlight := tab.reattachInFlight
	tab.mu.Unlock()
	if !inFlight {
		t.Fatalf("expected reattachInFlight=true after scheduling reattach")
	}

	if second := m.ReattachActiveTab(); second != nil {
		t.Fatalf("expected duplicate reattach to be suppressed while in flight")
	}
}

func TestReattachActiveTab_ClearsInFlightOnFailureAndSuccess(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:               TabID("tab-reattach"),
		Assistant:        "claude",
		Workspace:        ws,
		SessionName:      "amux-ws-tab-reattach",
		Detached:         true,
		reattachInFlight: true,
	}

	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_, _ = m.updatePtyTabReattachFailed(ptyTabReattachFailed{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Err:         errors.New("boom"),
		Action:      "reattach",
	})
	tab.mu.Lock()
	clearedAfterFailure := !tab.reattachInFlight
	tab.mu.Unlock()
	if !clearedAfterFailure {
		t.Fatalf("expected reattachInFlight to clear after failure")
	}

	tab.mu.Lock()
	tab.reattachInFlight = true
	tab.Detached = true
	tab.mu.Unlock()

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Agent: &appPty.Agent{
			Type:      appPty.AgentClaude,
			Workspace: ws,
			Session:   tab.SessionName,
		},
		Rows: 24,
		Cols: 80,
	})
	tab.mu.Lock()
	clearedAfterSuccess := !tab.reattachInFlight
	tab.mu.Unlock()
	if !clearedAfterSuccess {
		t.Fatalf("expected reattachInFlight to clear after success")
	}
}

func TestTabSelectionChangedCmd_SkipsNoOpSelection(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:          TabID("tab-a"),
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "amux-ws-tab-a",
		Detached:    true,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	if cmd := m.tabSelectionChangedCmd(false); cmd != nil {
		t.Fatalf("expected nil command for no-op selection")
	}
}

func TestTabSelectionChangedCmd_RunsOnSelectionChange(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:          TabID("tab-a"),
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "amux-ws-tab-a",
		Detached:    true,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	if cmd := m.tabSelectionChangedCmd(true); cmd == nil {
		t.Fatalf("expected command when selection changes")
	}
}
