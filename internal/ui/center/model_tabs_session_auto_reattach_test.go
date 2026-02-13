package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/messages"
)

func TestAutoReattachActiveTabOnSelection_SkipsAttachedTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:        TabID("tab-attached"),
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
		Detached:  false,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	cmd := m.autoReattachActiveTabOnSelection()
	if cmd != nil {
		t.Fatalf("expected nil cmd for attached tab, got non-nil")
	}
}

func TestAutoReattachActiveTabOnSelection_ReattachesDetachedTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:          TabID("tab-detached"),
		Assistant:   "claude",
		Workspace:   ws,
		Running:     false,
		Detached:    true,
		SessionName: "sess-detached",
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	cmd := m.autoReattachActiveTabOnSelection()
	if cmd == nil {
		t.Fatalf("expected non-nil cmd for detached tab")
	}

	// The cmd should have set reattachInFlight
	tab.mu.Lock()
	inFlight := tab.reattachInFlight
	tab.mu.Unlock()
	if !inFlight {
		t.Fatalf("expected reattachInFlight=true after autoReattachActiveTabOnSelection")
	}
}

func TestReattachActiveTab_SkipsAttachedNonAssistantTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:        TabID("tab-running"),
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
		Detached:  false,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	cmd := m.ReattachActiveTab()
	if cmd != nil {
		t.Fatalf("expected nil cmd for non-detached tab, got non-nil")
	}
}

func TestReattachActiveTab_DeduplicatesInFlight(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:               TabID("tab-inflight"),
		Assistant:        "claude",
		Workspace:        ws,
		Running:          false,
		Detached:         true,
		reattachInFlight: true,
		SessionName:      "sess-inflight",
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	cmd := m.ReattachActiveTab()
	if cmd != nil {
		t.Fatalf("expected nil cmd when reattachInFlight is already true")
	}
}

func TestReattachActiveTab_ClearsInFlightOnFailureAndSuccess(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	// Test config-validation failure: no assistant configured
	tab := &Tab{
		ID:          TabID("tab-unknown"),
		Assistant:   "unknown-assistant",
		Workspace:   ws,
		Running:     false,
		Detached:    true,
		SessionName: "sess-unknown",
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	cmd := m.ReattachActiveTab()
	if cmd == nil {
		t.Fatalf("expected toast cmd for unknown assistant")
	}

	// Verify reattachInFlight was cleared
	tab.mu.Lock()
	inFlight := tab.reattachInFlight
	tab.mu.Unlock()
	if inFlight {
		t.Fatalf("expected reattachInFlight=false after config validation failure")
	}

	// Verify the cmd produces a Toast
	msg := cmd()
	if _, ok := msg.(messages.Toast); !ok {
		t.Fatalf("expected Toast message, got %T", msg)
	}
}

func TestTabSelectionChangedCmd_SkipsNoOpSelection(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:        TabID("tab-1"),
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	cmd := m.tabSelectionChangedCmd(false)
	if cmd != nil {
		t.Fatalf("expected nil cmd for no-op selection change")
	}
}

func TestTabSelectionChangedCmd_RunsOnSelectionChange(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab1 := &Tab{
		ID:        TabID("tab-1"),
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
	}
	tab2 := &Tab{
		ID:        TabID("tab-2"),
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab1, tab2}
	m.activeTabByWorkspace[wsID] = 1
	m.workspace = ws

	cmd := m.tabSelectionChangedCmd(true)
	if cmd == nil {
		t.Fatalf("expected non-nil cmd for actual selection change")
	}
}
