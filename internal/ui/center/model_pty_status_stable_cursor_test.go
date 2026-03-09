package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestTerminalLayerSanitizesStoredSyntheticCursorGlyph(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	term.CursorX = 18
	term.CursorY = 2
	term.Screen[2][18] = vterm.Cell{Rune: 'x', Width: 1}
	term.Screen[10][4] = vterm.Cell{
		Rune:  '▌',
		Width: 1,
		Style: vterm.Style{Blink: true},
	}

	tab := &Tab{
		ID:                TabID("tab-chat-stored-synthetic-cursor"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          term,
		Running:           true,
		stableCursorSet:   true,
		stableCursorX:     4,
		stableCursorY:     10,
		lastOutputAt:      time.Now(),
		lastVisibleOutput: time.Now(),
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
		t.Fatal("expected stable cursor fallback to remain visible")
	}
	if layer.Snap.CursorX != 4 || layer.Snap.CursorY != 10 {
		t.Fatalf("expected stored cursor at (4,10), got (%d,%d)", layer.Snap.CursorX, layer.Snap.CursorY)
	}
	cell := layer.Snap.Screen[10][4]
	if cell.Rune != ' ' {
		t.Fatalf("expected stored synthetic cursor glyph to be sanitized, got %q", cell.Rune)
	}
	if cell.Style.Blink {
		t.Fatal("expected stored synthetic cursor cell blink attribute to be cleared")
	}
}
