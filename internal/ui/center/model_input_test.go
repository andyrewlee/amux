package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestUpdatePasteWithoutAttachedTerminalDoesNotTagActivity(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-1"),
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "session-1",
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws
	m.focused = true

	_, cmd := m.Update(tea.PasteMsg{Content: "hello"})
	if cmd != nil {
		t.Fatal("expected nil cmd when paste cannot be delivered to terminal")
	}
}

func TestUpdatePasteActorFallbackWithoutTerminalDoesNotTagActivity(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-1"),
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "session-1",
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws
	m.focused = true

	m.setTabActorReady()
	m.tabEvents = nil // Force sendTabEvent fallback path.

	_, cmd := m.Update(tea.PasteMsg{Content: "hello"})
	if cmd != nil {
		t.Fatal("expected nil cmd when paste fallback cannot be delivered to terminal")
	}
}
