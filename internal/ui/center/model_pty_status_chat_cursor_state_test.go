package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestTerminalLayerUpdatesStoredCursorWhenIdlePromptMovesAfterVersionChange(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	term.CursorX = 1
	term.CursorY = 11

	tab := &Tab{
		ID:              TabID("tab-chat-idle-prompt-move"),
		Assistant:       "codex",
		Workspace:       ws,
		Terminal:        term,
		Running:         true,
		stableCursorSet: true,
		stableCursorX:   1,
		stableCursorY:   11,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	seed := m.TerminalLayer()
	if seed == nil || seed.Snap == nil {
		t.Fatal("expected initial terminal layer snapshot")
	}

	tab.lastOutputAt = time.Now().Add(-tabActiveWindow - time.Millisecond)
	tab.lastVisibleOutput = time.Now().Add(-tabActiveWindow - time.Millisecond)
	term.Write([]byte("\x1b[11;5H"))

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected updated terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected idle prompt cursor to remain visible after prompt movement")
	}
	if layer.Snap.CursorX != 4 || layer.Snap.CursorY != 10 {
		t.Fatalf("expected moved idle prompt cursor at (4,10), got (%d,%d)", layer.Snap.CursorX, layer.Snap.CursorY)
	}
	if !tab.stableCursorSet || tab.stableCursorX != 4 || tab.stableCursorY != 10 {
		t.Fatalf("expected stored cursor to update to moved idle prompt, got set=%v pos=(%d,%d)",
			tab.stableCursorSet, tab.stableCursorX, tab.stableCursorY)
	}
}

func TestTerminalLayerPreservesStoredCursorAcrossTemporaryScrollback(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	for i := 0; i < 20; i++ {
		term.Write([]byte("line\n"))
	}
	term.CursorX = 2
	term.CursorY = 11

	tab := &Tab{
		ID:                  TabID("tab-chat-scrollback-stable"),
		Assistant:           "codex",
		Workspace:           ws,
		Terminal:            term,
		Running:             true,
		stableCursorSet:     true,
		stableCursorX:       2,
		stableCursorY:       11,
		stableCursorVersion: term.Version(),
		lastVisibleOutput:   time.Now(),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	term.ScrollView(1)
	scrolled := m.TerminalLayer()
	if scrolled == nil || scrolled.Snap == nil {
		t.Fatal("expected scrolled terminal layer snapshot")
	}
	if !tab.stableCursorSet {
		t.Fatal("expected stored cursor to remain set while viewing scrollback")
	}

	term.ScrollViewToBottom()
	live := m.TerminalLayer()
	if live == nil || live.Snap == nil {
		t.Fatal("expected live terminal layer snapshot")
	}
	if !live.Snap.ShowCursor {
		t.Fatal("expected cursor to return after leaving scrollback")
	}
	if live.Snap.CursorX != 2 || live.Snap.CursorY != 11 {
		t.Fatalf("expected stored cursor at (2,11) after leaving scrollback, got (%d,%d)", live.Snap.CursorX, live.Snap.CursorY)
	}
}

func TestTerminalLayerAllowsBracketedPasteAsRecentLocalInput(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 4
	term.CursorY = 10
	term.Screen[10][4] = vterm.Cell{Rune: 'x', Width: 1}

	tab := &Tab{
		ID:           TabID("tab-chat-bracketed-paste"),
		Assistant:    "codex",
		Workspace:    ws,
		Terminal:     term,
		Running:      true,
		lastOutputAt: time.Now(),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	recordLocalInputEchoWindow(tab, "\x1b[200~hello\x1b[201~", time.Now())

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected bracketed paste to keep multiline cursor visible during recent output activity")
	}
	if layer.Snap.CursorX != 4 || layer.Snap.CursorY != 10 {
		t.Fatalf("expected live cursor at (4,10), got (%d,%d)", layer.Snap.CursorX, layer.Snap.CursorY)
	}
}

func TestTerminalLayerRelearnsStoredCursorFromIdleMultilinePromptAfterRestrictedOutput(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 1
	term.CursorY = 23

	tab := &Tab{
		ID:              TabID("tab-chat-idle-multiline-relearn"),
		Assistant:       "codex",
		Workspace:       ws,
		Terminal:        term,
		Running:         true,
		stableCursorSet: true,
		stableCursorX:   1,
		stableCursorY:   23,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	seed := m.TerminalLayer()
	if seed == nil || seed.Snap == nil {
		t.Fatal("expected seeded terminal layer snapshot")
	}

	term.Write([]byte("\x1b[11;5Hprompt"))
	tab.lastOutputAt = time.Now()
	tab.lastVisibleOutput = time.Now()

	restricted := m.TerminalLayer()
	if restricted == nil || restricted.Snap == nil {
		t.Fatal("expected restricted terminal layer snapshot")
	}
	if !restricted.Snap.ShowCursor {
		t.Fatal("expected stored cursor to remain visible while output is restricted")
	}
	if restricted.Snap.CursorX != 1 || restricted.Snap.CursorY != 23 {
		t.Fatalf("expected restricted render to keep stored cursor at (1,23), got (%d,%d)",
			restricted.Snap.CursorX, restricted.Snap.CursorY)
	}

	tab.lastOutputAt = time.Now().Add(-tabActiveWindow - time.Millisecond)
	tab.lastVisibleOutput = time.Now().Add(-tabActiveWindow - time.Millisecond)

	idle := m.TerminalLayer()
	if idle == nil || idle.Snap == nil {
		t.Fatal("expected idle terminal layer snapshot")
	}
	if !idle.Snap.ShowCursor {
		t.Fatal("expected idle multiline prompt cursor to become visible")
	}
	if idle.Snap.CursorX != term.CursorX || idle.Snap.CursorY != term.CursorY {
		t.Fatalf("expected idle render to re-learn live multiline prompt cursor at (%d,%d), got (%d,%d)",
			term.CursorX, term.CursorY, idle.Snap.CursorX, idle.Snap.CursorY)
	}
	if !tab.stableCursorSet || tab.stableCursorX != term.CursorX || tab.stableCursorY != term.CursorY {
		t.Fatalf("expected stored cursor to update to idle multiline prompt, got set=%v pos=(%d,%d)",
			tab.stableCursorSet, tab.stableCursorX, tab.stableCursorY)
	}
}

func TestTerminalLayerPreservesStoredMultilineCursorAcrossRestrictedArtifact(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 19
	term.CursorY = 23

	tab := &Tab{
		ID:              TabID("tab-chat-restricted-stored-multiline"),
		Assistant:       "codex",
		Workspace:       ws,
		Terminal:        term,
		Running:         true,
		stableCursorSet: true,
		stableCursorX:   4,
		stableCursorY:   10,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	tab.lastOutputAt = time.Now()
	tab.lastVisibleOutput = time.Now()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected stored multiline cursor to remain visible during restricted output")
	}
	if layer.Snap.CursorX != 4 || layer.Snap.CursorY != 10 {
		t.Fatalf("expected restricted render to keep stored cursor at (4,10), got (%d,%d)",
			layer.Snap.CursorX, layer.Snap.CursorY)
	}
	if !tab.stableCursorSet || tab.stableCursorX != 4 || tab.stableCursorY != 10 {
		t.Fatalf("expected stored multiline cursor anchor to survive restricted render, got set=%v pos=(%d,%d)",
			tab.stableCursorSet, tab.stableCursorX, tab.stableCursorY)
	}
}

func TestTerminalLayerTracksEnterAsRecentPromptInputForWrappedPrompt(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 4
	term.CursorY = 10
	term.Screen[10][4] = vterm.Cell{Rune: 'x', Width: 1}

	tab := &Tab{
		ID:                TabID("tab-chat-enter-wrapped-prompt"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          term,
		Running:           true,
		stableCursorSet:   true,
		stableCursorX:     2,
		stableCursorY:     9,
		lastOutputAt:      time.Now(),
		lastVisibleOutput: time.Now(),
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
		t.Fatal("expected wrapped prompt cursor to remain visible after Enter")
	}
	if layer.Snap.CursorX != 4 || layer.Snap.CursorY != 10 {
		t.Fatalf("expected Enter redraw to use live wrapped prompt cursor at (4,10), got (%d,%d)",
			layer.Snap.CursorX, layer.Snap.CursorY)
	}
	if !tab.stableCursorSet || tab.stableCursorX != 4 || tab.stableCursorY != 10 {
		t.Fatalf("expected Enter redraw to update stored cursor to (4,10), got set=%v pos=(%d,%d)",
			tab.stableCursorSet, tab.stableCursorX, tab.stableCursorY)
	}
}

func TestTerminalLayerTracksCtrlCAsRecentPromptInputForWrappedPrompt(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 4
	term.CursorY = 10
	term.Screen[10][4] = vterm.Cell{Rune: 'x', Width: 1}

	tab := &Tab{
		ID:                TabID("tab-chat-ctrlc-wrapped-prompt"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          term,
		Running:           true,
		stableCursorSet:   true,
		stableCursorX:     2,
		stableCursorY:     9,
		lastOutputAt:      time.Now(),
		lastVisibleOutput: time.Now(),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	recordLocalInputEchoWindow(tab, "\x03", time.Now())

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected wrapped prompt cursor to remain visible after Ctrl-C")
	}
	if layer.Snap.CursorX != 4 || layer.Snap.CursorY != 10 {
		t.Fatalf("expected Ctrl-C redraw to use live wrapped prompt cursor at (4,10), got (%d,%d)",
			layer.Snap.CursorX, layer.Snap.CursorY)
	}
	if !tab.stableCursorSet || tab.stableCursorX != 4 || tab.stableCursorY != 10 {
		t.Fatalf("expected Ctrl-C redraw to update stored cursor to (4,10), got set=%v pos=(%d,%d)",
			tab.stableCursorSet, tab.stableCursorX, tab.stableCursorY)
	}
}

func TestTerminalLayerKeepsRestrictedCursorAfterSubmitWhenOutputJumpsAway(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 18
	term.CursorY = 4
	term.Screen[4][18] = vterm.Cell{Rune: 'x', Width: 1}

	tab := &Tab{
		ID:              TabID("tab-chat-submit-output-restrict"),
		Assistant:       "codex",
		Workspace:       ws,
		Terminal:        term,
		Running:         true,
		stableCursorSet: true,
		stableCursorX:   2,
		stableCursorY:   9,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	recordLocalInputEchoWindow(tab, "\r", time.Now())
	tab.lastOutputAt = time.Now()
	tab.lastVisibleOutput = time.Now()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected stored cursor to remain visible during submit-triggered output")
	}
	if layer.Snap.CursorX != 2 || layer.Snap.CursorY != 9 {
		t.Fatalf("expected submit-time output to keep stored cursor at (2,9), got (%d,%d)",
			layer.Snap.CursorX, layer.Snap.CursorY)
	}
	if !tab.stableCursorSet || tab.stableCursorX != 2 || tab.stableCursorY != 9 {
		t.Fatalf("expected submit-time output not to overwrite stored cursor, got set=%v pos=(%d,%d)",
			tab.stableCursorSet, tab.stableCursorX, tab.stableCursorY)
	}
}

func TestTerminalLayerKeepsRestrictedCursorWhenSubmitOutputJumpsToLeftEdge(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 0
	term.CursorY = 10
	term.Screen[10][0] = vterm.Cell{Rune: 'x', Width: 1}

	tab := &Tab{
		ID:              TabID("tab-chat-submit-output-left-edge"),
		Assistant:       "codex",
		Workspace:       ws,
		Terminal:        term,
		Running:         true,
		stableCursorSet: true,
		stableCursorX:   10,
		stableCursorY:   9,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	recordLocalInputEchoWindow(tab, "\r", time.Now())
	tab.lastOutputAt = time.Now()
	tab.lastVisibleOutput = time.Now()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected stored cursor to remain visible during left-edge submit output")
	}
	if layer.Snap.CursorX != 10 || layer.Snap.CursorY != 9 {
		t.Fatalf("expected left-edge submit output to keep stored cursor at (10,9), got (%d,%d)",
			layer.Snap.CursorX, layer.Snap.CursorY)
	}
	if !tab.stableCursorSet || tab.stableCursorX != 10 || tab.stableCursorY != 9 {
		t.Fatalf("expected left-edge submit output not to overwrite stored cursor, got set=%v pos=(%d,%d)",
			tab.stableCursorSet, tab.stableCursorX, tab.stableCursorY)
	}
}

func TestTerminalLayerRelearnsCursorAfterControlOnlyMoveGoesIdle(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 2
	term.CursorY = 20
	term.Screen[20][2] = vterm.Cell{Rune: 'x', Width: 1}

	tab := &Tab{
		ID:                  TabID("tab-chat-control-only-idle-relearn"),
		Assistant:           "codex",
		Workspace:           ws,
		Terminal:            term,
		Running:             true,
		stableCursorSet:     true,
		stableCursorX:       2,
		stableCursorY:       20,
		stableCursorVersion: term.Version(),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	term.CursorX = 4
	term.CursorY = 10
	term.Screen[10][4] = vterm.Cell{Rune: 'x', Width: 1}
	tab.lastOutputAt = time.Now()

	restricted := m.TerminalLayer()
	if restricted == nil || restricted.Snap == nil {
		t.Fatal("expected restricted terminal layer snapshot")
	}
	if !restricted.Snap.ShowCursor {
		t.Fatal("expected restricted render to keep a visible stored cursor")
	}
	if restricted.Snap.CursorX != 2 || restricted.Snap.CursorY != 20 {
		t.Fatalf("expected restricted render to keep stored cursor at (2,20), got (%d,%d)",
			restricted.Snap.CursorX, restricted.Snap.CursorY)
	}
	if !tab.pendingIdleCursorRelearn {
		t.Fatal("expected control-only restricted render to arm idle cursor relearn")
	}

	tab.lastOutputAt = time.Now().Add(-tabActiveWindow - time.Millisecond)

	idle := m.TerminalLayer()
	if idle == nil || idle.Snap == nil {
		t.Fatal("expected idle terminal layer snapshot")
	}
	if !idle.Snap.ShowCursor {
		t.Fatal("expected idle cursor to become visible after control-only move")
	}
	if idle.Snap.CursorX != 4 || idle.Snap.CursorY != 10 {
		t.Fatalf("expected idle render to re-learn live cursor at (4,10), got (%d,%d)",
			idle.Snap.CursorX, idle.Snap.CursorY)
	}
	if !tab.stableCursorSet || tab.stableCursorX != 4 || tab.stableCursorY != 10 {
		t.Fatalf("expected stored cursor to update to re-learned live cursor, got set=%v pos=(%d,%d)",
			tab.stableCursorSet, tab.stableCursorX, tab.stableCursorY)
	}
	if tab.pendingIdleCursorRelearn {
		t.Fatal("expected idle cursor relearn to clear the pending flag")
	}
}
