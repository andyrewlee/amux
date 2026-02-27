package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestTerminalLayerForcesVisibleCursorForChatTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)
	term.Write([]byte("\x1b[?25l"))

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-chat"),
			Assistant: "codex",
			Workspace: ws,
			Terminal:  term,
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if layer.Snap.CursorHidden {
		t.Fatal("expected chat tab cursor to remain visible despite DECTCEM hide")
	}
	if !term.IgnoreCursorVisibilityControls {
		t.Fatal("expected chat tabs to ignore terminal cursor visibility controls")
	}
}

func TestTerminalLayerPreservesCursorHiddenForNonChatTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)
	term.Write([]byte("\x1b[?25l"))

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-non-chat"),
			Assistant: "bash",
			Workspace: ws,
			Terminal:  term,
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.CursorHidden {
		t.Fatal("expected non-chat tab to honor DECTCEM hide")
	}
	if term.IgnoreCursorVisibilityControls {
		t.Fatal("expected non-chat tabs to honor terminal cursor visibility controls")
	}
}

func TestTerminalLayerNormalizesSyntheticCursorCellForChatTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)
	term.CursorX = 0
	term.CursorY = 0
	term.Screen[0][0] = vterm.Cell{
		Rune:  '█',
		Width: 1,
		Style: vterm.Style{Blink: true},
	}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-chat-artifact"),
			Assistant: "codex",
			Workspace: ws,
			Terminal:  term,
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	cell := layer.Snap.Screen[0][0]
	if cell.Rune != ' ' {
		t.Fatalf("expected chat cursor artifact glyph to be normalized, got %q", cell.Rune)
	}
	if cell.Style.Blink {
		t.Fatal("expected chat cursor cell blink attribute to be cleared")
	}
}

func TestTerminalLayerKeepsSyntheticCursorCellForNonChatTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)
	term.CursorX = 0
	term.CursorY = 0
	term.Screen[0][0] = vterm.Cell{
		Rune:  '█',
		Width: 1,
		Style: vterm.Style{Blink: true},
	}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-non-chat-artifact"),
			Assistant: "bash",
			Workspace: ws,
			Terminal:  term,
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	cell := layer.Snap.Screen[0][0]
	if cell.Rune != '█' {
		t.Fatalf("expected non-chat cursor artifact glyph to be preserved, got %q", cell.Rune)
	}
	if !cell.Style.Blink {
		t.Fatal("expected non-chat cursor cell blink attribute to be preserved")
	}
}

func TestTerminalLayerClearsBlinkAttributesForChatTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)
	term.Screen[0][1] = vterm.Cell{
		Rune:  'x',
		Width: 1,
		Style: vterm.Style{Blink: true},
	}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-chat-blink"),
			Assistant: "codex",
			Workspace: ws,
			Terminal:  term,
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if layer.Snap.Screen[0][1].Style.Blink {
		t.Fatal("expected blink attributes to be cleared for chat tabs")
	}
}

func TestTerminalLayerPreservesBlinkAttributesForNonChatTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)
	term.Screen[0][1] = vterm.Cell{
		Rune:  'x',
		Width: 1,
		Style: vterm.Style{Blink: true},
	}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-non-chat-blink"),
			Assistant: "bash",
			Workspace: ws,
			Terminal:  term,
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.Screen[0][1].Style.Blink {
		t.Fatal("expected blink attributes to be preserved for non-chat tabs")
	}
}
