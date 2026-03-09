package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestTerminalLayerTracksFreshSubmitPromptBeforeStableCursorLearned(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 4
	term.CursorY = 10
	term.Screen[10][4] = vterm.Cell{Rune: 'x', Width: 1}

	tab := &Tab{
		ID:                TabID("tab-chat-fresh-submit-before-anchor"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          term,
		Running:           true,
		lastOutputAt:      time.Now(),
		lastVisibleOutput: time.Now(),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	recordLocalInputEchoWindow(tab, "\r", time.Now())
	tab.lastOutputAt = time.Now()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected fresh submit to keep wrapped prompt cursor visible before learning an anchor")
	}
	if layer.Snap.CursorX != 4 || layer.Snap.CursorY != 10 {
		t.Fatalf("expected fresh submit to use live prompt cursor at (4,10), got (%d,%d)",
			layer.Snap.CursorX, layer.Snap.CursorY)
	}
	if !tab.stableCursorSet || tab.stableCursorX != 4 || tab.stableCursorY != 10 {
		t.Fatalf("expected fresh submit to learn prompt cursor anchor at (4,10), got set=%v pos=(%d,%d)",
			tab.stableCursorSet, tab.stableCursorX, tab.stableCursorY)
	}
}

func TestTerminalLayerTracksIndentedSubmitRedrawBeforePostSubmitOutput(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 0
	term.CursorY = 11
	term.Screen[11][0] = vterm.Cell{Rune: 'x', Width: 1}

	tab := &Tab{
		ID:                TabID("tab-chat-indented-submit-redraw"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          term,
		Running:           true,
		stableCursorSet:   true,
		stableCursorX:     10,
		stableCursorY:     10,
		lastOutputAt:      time.Now().Add(-time.Millisecond),
		lastVisibleOutput: time.Now().Add(-time.Millisecond),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	recordLocalInputEchoWindow(tab, "\r", time.Now())

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected submit redraw from indented prompt to keep the live cursor visible")
	}
	if layer.Snap.CursorX != 0 || layer.Snap.CursorY != 11 {
		t.Fatalf("expected submit redraw to use live cursor at (0,11), got (%d,%d)",
			layer.Snap.CursorX, layer.Snap.CursorY)
	}
	if !tab.stableCursorSet || tab.stableCursorX != 0 || tab.stableCursorY != 11 {
		t.Fatalf("expected submit redraw to update stored cursor to (0,11), got set=%v pos=(%d,%d)",
			tab.stableCursorSet, tab.stableCursorX, tab.stableCursorY)
	}
}

func TestTerminalLayerDoesNotLearnInitialCursorFromBlankControlOnlyJump(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 4
	term.CursorY = 4

	tab := &Tab{
		ID:           TabID("tab-chat-initial-blank-control-jump"),
		Assistant:    "codex",
		Workspace:    ws,
		Terminal:     term,
		Running:      true,
		lastOutputAt: time.Now().Add(-tabActiveWindow - time.Millisecond),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if layer.Snap.ShowCursor {
		t.Fatal("expected blank control-only cursor jump to stay hidden before prompt context appears")
	}
	if tab.stableCursorSet {
		t.Fatalf("expected blank control-only cursor jump not to learn a stable anchor, got (%d,%d)",
			tab.stableCursorX, tab.stableCursorY)
	}
}

func TestTerminalLayerAllowsSecondToLastRowCursorInShortRestrictedViewport(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 6)
	term.CursorX = 1
	term.CursorY = 4
	term.Screen[4][1] = vterm.Cell{Rune: 'x', Width: 1}

	tab := &Tab{
		ID:                TabID("tab-chat-short-pane-second-last-row"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          term,
		Running:           true,
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
		t.Fatal("expected second-to-last-row multiline prompt cursor to remain visible in a short restricted viewport")
	}
	if layer.Snap.CursorX != 1 || layer.Snap.CursorY != 4 {
		t.Fatalf("expected live cursor at (1,4), got (%d,%d)", layer.Snap.CursorX, layer.Snap.CursorY)
	}
}
