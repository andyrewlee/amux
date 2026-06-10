package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/vterm"
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
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
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
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
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
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
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
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
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

func TestUpdateKeyPgUpScrollsOneLineOnShortTerminal(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(80, 3)
	for i := 0; i < 10; i++ {
		term.Write([]byte("line\n"))
	}
	tab := &Tab{
		ID:        TabID("tab-short-page-scroll"),
		Assistant: "claude",
		Workspace: ws,
		Terminal:  term,
		Agent:     &appPty.Agent{Terminal: &appPty.Terminal{}},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.workspace = ws
	m.focused = true

	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})

	tab.mu.Lock()
	offset, _ := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()
	if offset != 1 {
		t.Fatalf("expected PgUp on a short terminal to scroll by 1 line, got %d", offset)
	}
}

func TestTabActorScrollPageScrollsOneLineOnShortTerminal(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(80, 3)
	for i := 0; i < 10; i++ {
		term.Write([]byte("line\n"))
	}
	tab := &Tab{
		ID:        TabID("tab-short-page-scroll-actor"),
		Assistant: "claude",
		Workspace: ws,
		Terminal:  term,
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.workspace = ws
	m.focused = true

	m.handleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: wsID,
		tabID:       tab.ID,
		kind:        tabEventScrollPage,
		scrollPage:  1,
	})

	tab.mu.Lock()
	offset, _ := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()
	if offset != 1 {
		t.Fatalf("expected actor page scroll on a short terminal to scroll by 1 line, got %d", offset)
	}
}
