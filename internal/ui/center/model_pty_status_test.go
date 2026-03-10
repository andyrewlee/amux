package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/ui/diff"
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
	if term.IgnoreCursorVisibilityControls {
		t.Fatal("expected chat tabs to observe terminal cursor visibility controls")
	}
	if !term.TreatLFAsCRLF {
		t.Fatal("expected chat tabs to normalize LF as CRLF")
	}
}

func TestTerminalLayerLetsChatAppOwnSyntheticCursorWhenHidden(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 4)
	term.Write([]byte("\x1b[?25l"))
	term.CursorX = 2
	term.CursorY = 2
	term.Screen[2][2] = vterm.Cell{Rune: '█', Width: 1}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-chat-owned-cursor"),
			Assistant: "claude",
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
	if layer.Snap.ShowCursor {
		t.Fatal("expected chat tab to suppress amux cursor when the app already paints its own hidden-cursor glyph")
	}
	if layer.Snap.Screen[2][2].Rune != '█' {
		t.Fatal("expected synthetic app-owned cursor glyph to remain intact")
	}
}

func TestTerminalLayerLetsChatAppOwnSteadyBarCursorWhenHidden(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 4)
	term.Write([]byte("\x1b[?25l"))
	term.CursorX = 2
	term.CursorY = 2
	term.Screen[2][2] = vterm.Cell{
		Rune:  '▌',
		Width: 1,
		Style: vterm.Style{Reverse: true},
	}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-chat-owned-steady-bar-cursor"),
			Assistant: "claude",
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
	if layer.Snap.ShowCursor {
		t.Fatal("expected steady styled bar cursor glyph to suppress the amux cursor when the app hides DECTCEM")
	}
	if layer.Snap.Screen[2][2].Rune != '▌' {
		t.Fatal("expected steady app-owned cursor glyph to remain intact")
	}
}

func TestTerminalLayerPreservesCursorForLiteralBlockTextWhenHidden(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 4)
	term.Write([]byte("\x1b[?25l"))
	term.CursorX = 2
	term.CursorY = 2
	term.Screen[2][2] = vterm.Cell{Rune: '▌', Width: 1}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-chat-literal-block-text"),
			Assistant: "claude",
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
	if !layer.Snap.ShowCursor {
		t.Fatal("expected literal block text at the cursor cell not to suppress the amux cursor")
	}
}

func TestTerminalLayerPreservesCursorForStoredLiteralFullBlockWhenHidden(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 4)
	term.Write([]byte("\x1b[?25l"))
	term.CursorX = 0
	term.CursorY = 3
	term.Screen[2][2] = vterm.Cell{Rune: '█', Width: 1}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:              TabID("tab-chat-stored-literal-full-block"),
			Assistant:       "claude",
			Workspace:       ws,
			Terminal:        term,
			stableCursorSet: true,
			stableCursorX:   2,
			stableCursorY:   2,
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
		t.Fatal("expected stored literal full-block content not to suppress the amux cursor")
	}
}

func TestTerminalLayerIgnoresUnrelatedSyntheticGlyphWhenCursorHidden(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 4)
	term.Write([]byte("\x1b[?25l"))
	term.CursorX = 0
	term.CursorY = 3
	term.Screen[2][2] = vterm.Cell{Rune: '█', Width: 1}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-chat-unrelated-cursor-glyph"),
			Assistant: "claude",
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
	if !layer.Snap.ShowCursor {
		t.Fatal("expected unrelated synthetic block text to not suppress the amux cursor")
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
	if term.TreatLFAsCRLF {
		t.Fatal("expected non-chat tabs to preserve native LF behavior")
	}
}

func TestIsChatTabUsesConfigMapWhenPresent(t *testing.T) {
	m := newTestModel()
	tab := &Tab{Assistant: "cursor"}

	if m.isChatTab(tab) {
		t.Fatal("expected assistant missing from config map to be treated as non-chat when config is present")
	}
}

func TestIsChatTabFalseWhenDiffViewerPresent(t *testing.T) {
	m := newTestModel()
	tab := &Tab{
		Assistant:  "codex",
		DiffViewer: &diff.Model{},
	}

	if m.isChatTab(tab) {
		t.Fatal("expected diff viewer tabs not to be treated as chat tabs")
	}
}

func TestTerminalLayerShowsCursorForIdleBootstrapChatTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:                    TabID("tab-chat-bootstrap"),
			Assistant:             "codex",
			Workspace:             ws,
			Terminal:              term,
			Running:               true,
			bootstrapActivity:     true,
			bootstrapLastOutputAt: time.Now(),
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
		t.Fatal("expected cursor to remain visible for idle bootstrap tab without recent output")
	}
}

func TestTerminalLayerShowsCursorForChatTabWithRecentOutput(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:           TabID("tab-chat-recent-output"),
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
		t.Fatal("expected cursor to remain visible with recent output")
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
		Rune:  '▌',
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

func TestTerminalLayerPreservesNonBlinkingBlockElementTextAtLiveCursor(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)
	term.CursorX = 0
	term.CursorY = 0
	term.Screen[0][0] = vterm.Cell{
		Rune:  '▌',
		Width: 1,
	}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-chat-real-block-text"),
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
	if cell.Rune != '▌' {
		t.Fatalf("expected non-blinking block-element text to be preserved, got %q", cell.Rune)
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
