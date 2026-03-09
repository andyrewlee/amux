package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestTerminalLayerRecomputesCachedBlankCornerPromptAfterRecentLocalInput(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	term.CursorX = 0
	term.CursorY = 11
	term.Screen[11][0] = vterm.Cell{Rune: ' ', Width: 1}

	tab := &Tab{
		ID:        TabID("tab-chat-cached-corner-local-edit"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  term,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	first := m.TerminalLayer()
	if first == nil || first.Snap == nil {
		t.Fatal("expected initial terminal layer snapshot")
	}
	if !first.Snap.ShowCursor {
		t.Fatal("expected blank-corner prompt cursor to remain visible before local input")
	}
	if tab.stableCursorSet {
		t.Fatal("expected initial blank-corner prompt to remain unstabilized")
	}

	tab.mu.Lock()
	tab.lastUserInputAt = time.Now()
	tab.lastPromptInputAt = tab.lastUserInputAt
	tab.mu.Unlock()

	second := m.TerminalLayer()
	if second == nil || second.Snap == nil {
		t.Fatal("expected refreshed terminal layer snapshot")
	}
	if !second.Snap.ShowCursor {
		t.Fatal("expected blank-corner prompt cursor to remain visible after local input")
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if !tab.stableCursorSet || tab.stableCursorX != 0 || tab.stableCursorY != 11 {
		t.Fatalf("expected recent local input to bypass cache and learn stable cursor, got set=%v pos=(%d,%d)",
			tab.stableCursorSet, tab.stableCursorX, tab.stableCursorY)
	}
}

func TestTerminalLayerRecomputesCachedPromptWhenVisibleActivityEnds(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 4
	term.CursorY = 10
	term.Screen[10][4] = vterm.Cell{Rune: 'x', Width: 1}

	tab := &Tab{
		ID:                TabID("tab-chat-cached-activity-idle"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          term,
		Running:           true,
		lastVisibleOutput: time.Now(),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	first := m.TerminalLayer()
	if first == nil || first.Snap == nil {
		t.Fatal("expected initial terminal layer snapshot")
	}
	if first.Snap.ShowCursor {
		t.Fatal("expected visibly active mid-screen prompt cursor to remain hidden")
	}

	tab.mu.Lock()
	tab.lastVisibleOutput = time.Now().Add(-tabActiveWindow - time.Millisecond)
	tab.mu.Unlock()

	second := m.TerminalLayer()
	if second == nil || second.Snap == nil {
		t.Fatal("expected refreshed terminal layer snapshot")
	}
	if !second.Snap.ShowCursor {
		t.Fatal("expected idle prompt cursor to become visible after visible activity ends")
	}
	if second.Snap.CursorX != 4 || second.Snap.CursorY != 10 {
		t.Fatalf("expected live cursor at (4,10), got (%d,%d)", second.Snap.CursorX, second.Snap.CursorY)
	}
}

func TestTerminalLayerHidesBlankCornerArtifactWhileVisiblyActiveWithoutStoredCursor(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	term.CursorX = 0
	term.CursorY = 11
	term.Screen[11][0] = vterm.Cell{Rune: ' ', Width: 1}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:                TabID("tab-chat-active-corner-artifact"),
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
		t.Fatal("expected visibly active blank-corner cursor artifact to stay hidden before anchoring")
	}
}

func TestTerminalLayerRestrictsMidScreenCursorDuringControlOnlyOutput(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	term.CursorX = 4
	term.CursorY = 4
	term.Screen[4][4] = vterm.Cell{Rune: 'x', Width: 1}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:           TabID("tab-chat-control-only-output"),
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
	if layer.Snap.ShowCursor {
		t.Fatal("expected recent control-only output to keep mid-screen chat cursor hidden")
	}
}

func TestTerminalLayerAllowsRecentLocalInputDespiteRecentControlOnlyOutput(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 4
	term.CursorY = 10
	term.Screen[10][4] = vterm.Cell{Rune: 'x', Width: 1}

	tab := &Tab{
		ID:                TabID("tab-chat-control-only-local-edit"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          term,
		Running:           true,
		lastOutputAt:      time.Now(),
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
		t.Fatal("expected recent local input to keep multiline chat cursor visible")
	}
	if layer.Snap.CursorX != 4 || layer.Snap.CursorY != 10 {
		t.Fatalf("expected live cursor at (4,10), got (%d,%d)", layer.Snap.CursorX, layer.Snap.CursorY)
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if !tab.stableCursorSet || tab.stableCursorX != 4 || tab.stableCursorY != 10 {
		t.Fatalf("expected recent local input to seed stable cursor, got set=%v pos=(%d,%d)",
			tab.stableCursorSet, tab.stableCursorX, tab.stableCursorY)
	}
}

func TestTerminalLayerIgnoresRecentLocalEchoOutputAfterInputWindowExpires(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	now := time.Now()
	term := vterm.New(20, 24)
	term.CursorX = 4
	term.CursorY = 10
	term.Screen[10][4] = vterm.Cell{Rune: 'x', Width: 1}

	tab := &Tab{
		ID:                TabID("tab-chat-local-echo-expired"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          term,
		Running:           true,
		lastUserInputAt:   now.Add(-600 * time.Millisecond),
		lastPromptInputAt: now.Add(-600 * time.Millisecond),
		lastOutputAt:      now.Add(-550 * time.Millisecond),
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
		t.Fatal("expected recent local-echo output to stop restricting multiline prompt cursor after the input window expires")
	}
	if layer.Snap.CursorX != 4 || layer.Snap.CursorY != 10 {
		t.Fatalf("expected live cursor at (4,10), got (%d,%d)", layer.Snap.CursorX, layer.Snap.CursorY)
	}
}

func TestTerminalLayerPreservesRealBlockGlyphAtStoredCursorPosition(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	term.CursorX = 19
	term.CursorY = 0
	term.Screen[11][3] = vterm.Cell{Rune: '█', Width: 1}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:              TabID("tab-chat-stored-block-glyph"),
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
	if got := layer.Snap.Screen[11][3].Rune; got != '█' {
		t.Fatalf("expected stored cursor cell block glyph to be preserved, got %q", got)
	}
}

func TestTerminalLayerTracksStoredChatCursorAcrossLiveCursorJumps(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	term.CursorX = 1
	term.CursorY = 11

	tab := &Tab{
		ID:           TabID("tab-chat-anchor-update"),
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

	first := m.TerminalLayer()
	if first == nil || first.Snap == nil {
		t.Fatal("expected initial terminal layer snapshot")
	}
	if !first.Snap.ShowCursor {
		t.Fatal("expected initial cursor in input section to be visible")
	}
	if first.Snap.CursorX != 1 || first.Snap.CursorY != 11 {
		t.Fatalf("expected initial cursor at (1,11), got (%d,%d)", first.Snap.CursorX, first.Snap.CursorY)
	}
	tab.mu.Lock()
	if !tab.stableCursorSet || tab.stableCursorX != 1 || tab.stableCursorY != 11 {
		tab.mu.Unlock()
		t.Fatal("expected initial input cursor to seed stored cursor position")
	}
	tab.mu.Unlock()

	term.Write([]byte("a"))
	tab.lastOutputAt = time.Now()
	tab.lastUserInputAt = time.Now()
	tab.lastPromptInputAt = tab.lastUserInputAt
	second := m.TerminalLayer()
	if second == nil || second.Snap == nil {
		t.Fatal("expected updated terminal layer snapshot")
	}
	if !second.Snap.ShowCursor {
		t.Fatal("expected cursor to stay visible after input-section update")
	}
	if second.Snap.CursorX != term.CursorX || second.Snap.CursorY != term.CursorY {
		t.Fatalf("expected stored cursor to update from input-section cursor: want=(%d,%d) got=(%d,%d)",
			term.CursorX, term.CursorY, second.Snap.CursorX, second.Snap.CursorY)
	}

	term.Write([]byte("\x1b[1;20H"))
	tab.lastOutputAt = time.Now()
	tab.lastVisibleOutput = time.Now()
	tab.lastUserInputAt = time.Now().Add(-localInputEchoSuppressWindow - time.Millisecond)
	tab.lastPromptInputAt = tab.lastUserInputAt
	third := m.TerminalLayer()
	if third == nil || third.Snap == nil {
		t.Fatal("expected third terminal layer snapshot")
	}
	if !third.Snap.ShowCursor {
		t.Fatal("expected stored input cursor to remain visible after out-of-input jump")
	}
	if third.Snap.CursorX != second.Snap.CursorX || third.Snap.CursorY != second.Snap.CursorY {
		t.Fatalf("expected stored cursor to stay at (%d,%d), got (%d,%d)",
			second.Snap.CursorX, second.Snap.CursorY, third.Snap.CursorX, third.Snap.CursorY)
	}

	tab.lastOutputAt = time.Now().Add(-tabActiveWindow - time.Millisecond)
	tab.lastVisibleOutput = time.Now().Add(-tabActiveWindow - time.Millisecond)
	fourth := m.TerminalLayer()
	if fourth == nil || fourth.Snap == nil {
		t.Fatal("expected fourth terminal layer snapshot")
	}
	if !fourth.Snap.ShowCursor {
		t.Fatal("expected stored input cursor to remain visible after output activity ages out")
	}
	if fourth.Snap.CursorX != second.Snap.CursorX || fourth.Snap.CursorY != second.Snap.CursorY {
		t.Fatalf("expected stored cursor to remain at (%d,%d) after output idles, got (%d,%d)",
			second.Snap.CursorX, second.Snap.CursorY, fourth.Snap.CursorX, fourth.Snap.CursorY)
	}
}

func TestTerminalLayerTreatsWrappedMultilinePromptAsInputSection(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 4
	term.CursorY = 10
	term.Screen[10][4] = vterm.Cell{Rune: 'x', Width: 1}

	tab := &Tab{
		ID:                TabID("tab-chat-multiline-prompt"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          term,
		Running:           true,
		lastVisibleOutput: time.Now().Add(-tabActiveWindow - time.Millisecond),
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
		t.Fatal("expected wrapped multiline prompt cursor to remain visible")
	}
	if layer.Snap.CursorX != 4 || layer.Snap.CursorY != 10 {
		t.Fatalf("expected live cursor at (4,10), got (%d,%d)", layer.Snap.CursorX, layer.Snap.CursorY)
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if !tab.stableCursorSet || tab.stableCursorX != 4 || tab.stableCursorY != 10 {
		t.Fatalf("expected wrapped multiline prompt cursor to seed stored cursor, got set=%v pos=(%d,%d)",
			tab.stableCursorSet, tab.stableCursorX, tab.stableCursorY)
	}
}

func TestTerminalLayerKeepsVisiblyActiveChatCursorRestrictedToBottomInputBand(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 24)
	term.CursorX = 4
	term.CursorY = 10
	term.Screen[10][4] = vterm.Cell{Rune: 'x', Width: 1}

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:                TabID("tab-chat-running-midscreen-cursor"),
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
		t.Fatal("expected running chat cursor outside bottom input band to stay hidden")
	}
}
