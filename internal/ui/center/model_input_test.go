package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	appPty "github.com/andyrewlee/amux/internal/pty"
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

func TestUpdatePasteQueuedDoesNotStampLocalInputWindow(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-1"),
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "session-paste-queued",
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws
	m.focused = true

	m.setTabActorReady()
	m.tabEvents = make(chan tabEvent, 1)

	_, _ = m.Update(tea.PasteMsg{Content: "hello"})
	if !tab.lastUserInputAt.IsZero() {
		t.Fatal("expected queued paste not to stamp local-input window before PTY write")
	}
	if !tab.lastPromptInputAt.IsZero() {
		t.Fatal("expected queued paste not to stamp prompt-input window before PTY write")
	}
}

func TestUpdateQueuedKeyInputDoesNotStampLocalInputWindow(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-1"),
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "session-key-queued",
		Agent:       &appPty.Agent{Terminal: &appPty.Terminal{}},
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws
	m.focused = true

	m.setTabActorReady()
	m.tabEvents = make(chan tabEvent, 4)

	_, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !tab.lastUserInputAt.IsZero() {
		t.Fatal("expected queued key input not to stamp local-input window before PTY write")
	}
	if !tab.lastPromptInputAt.IsZero() {
		t.Fatal("expected queued key input not to stamp prompt-input window before PTY write")
	}
}
