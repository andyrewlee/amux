package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestTerminalLayerShowsCursorWhileNonCodexChatTabStreaming(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:           TabID("tab-chat-streaming-claude"),
			Assistant:    "claude",
			Workspace:    ws,
			Terminal:     term,
			Running:      true,
			lastOutputAt: time.Now(),
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected cursor to remain visible while chat tab is actively streaming")
	}
}

func TestTerminalLayerShowsCursorWhileCodexChatTabStreaming(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:           TabID("tab-chat-streaming-codex-control"),
			Assistant:    "codex",
			Workspace:    ws,
			Terminal:     term,
			Running:      true,
			lastOutputAt: time.Now(),
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected codex cursor to remain visible while streaming")
	}
}

func TestTerminalLayerHidesChatCursorOutsideInputSectionWithoutStoredCursor(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	term.CursorX = 19
	term.CursorY = 0

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:                TabID("tab-chat-unanchored-top-right"),
			Assistant:         "codex",
			Workspace:         ws,
			Terminal:          term,
			Running:           true,
			lastVisibleOutput: time.Now(),
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if layer.Snap.ShowCursor {
		t.Fatal("expected chat cursor to hide when live cursor leaves the input section and no stored cursor exists")
	}
}

func TestTerminalLayerUsesLiveCursorInAltScreenChatTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	term.AltScreen = true
	term.CursorX = 10
	term.CursorY = 4

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:              TabID("tab-chat-alt-screen"),
			Assistant:       "codex",
			Workspace:       ws,
			Terminal:        term,
			stableCursorSet: true,
			stableCursorX:   2,
			stableCursorY:   11,
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected alt-screen chat cursor to remain visible")
	}
	if layer.Snap.CursorX != 10 || layer.Snap.CursorY != 4 {
		t.Fatalf("expected live alt-screen cursor at (10,4), got (%d,%d)", layer.Snap.CursorX, layer.Snap.CursorY)
	}
}

func TestTerminalLayerUsesStoredChatCursorWhenLiveCursorLeavesInputSection(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	term.CursorX = 19
	term.CursorY = 0

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:                TabID("tab-chat-anchored-input"),
			Assistant:         "codex",
			Workspace:         ws,
			Terminal:          term,
			Running:           true,
			lastVisibleOutput: time.Now(),
			stableCursorSet:   true,
			stableCursorX:     2,
			stableCursorY:     11,
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected stored chat cursor to remain visible")
	}
	if layer.Snap.CursorX != 2 || layer.Snap.CursorY != 11 {
		t.Fatalf("expected stored cursor at (2,11), got (%d,%d)", layer.Snap.CursorX, layer.Snap.CursorY)
	}
}

func TestTerminalLayerKeepsStoredChatCursorWhenLiveCursorFallsOnBlankCorner(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	term.CursorX = 19
	term.CursorY = 11
	term.Screen[11][19] = vterm.Cell{Rune: ' ', Width: 1}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:              TabID("tab-chat-corner-artifact"),
			Assistant:       "codex",
			Workspace:       ws,
			Terminal:        term,
			stableCursorSet: true,
			stableCursorX:   3,
			stableCursorY:   11,
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected stored cursor to remain visible")
	}
	if layer.Snap.CursorX != 3 || layer.Snap.CursorY != 11 {
		t.Fatalf("expected stored cursor at (3,11), got (%d,%d)", layer.Snap.CursorX, layer.Snap.CursorY)
	}
}

func TestTerminalLayerAllowsBlankCornerCursorAfterRecentLocalInput(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	term.CursorX = 0
	term.CursorY = 11
	term.Screen[11][0] = vterm.Cell{Rune: ' ', Width: 1}

	tab := &Tab{
		ID:                TabID("tab-chat-local-corner"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          term,
		lastPromptInputAt: time.Now(),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected recent local editing to allow corner cursor adoption")
	}
	if layer.Snap.CursorX != 0 || layer.Snap.CursorY != 11 {
		t.Fatalf("expected adopted cursor at (0,11), got (%d,%d)", layer.Snap.CursorX, layer.Snap.CursorY)
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if !tab.stableCursorSet || tab.stableCursorX != 0 || tab.stableCursorY != 11 {
		t.Fatal("expected recent local corner cursor to seed stored cursor position")
	}
}

func TestTerminalLayerShowsBlankCornerPromptWithoutRecentLocalInput(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	term.CursorX = 0
	term.CursorY = 11
	term.Screen[11][0] = vterm.Cell{Rune: ' ', Width: 1}

	tab := &Tab{
		ID:        TabID("tab-chat-idle-corner"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  term,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected blank-corner prompt cursor to remain visible without recent local input")
	}
	if layer.Snap.CursorX != 0 || layer.Snap.CursorY != 11 {
		t.Fatalf("expected live cursor at (0,11), got (%d,%d)", layer.Snap.CursorX, layer.Snap.CursorY)
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.stableCursorSet {
		t.Fatal("expected blank-corner prompt to stay visible without seeding stable cursor")
	}
}
