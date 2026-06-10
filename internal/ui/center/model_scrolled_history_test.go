package center

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestTerminalLayerUsesHistoryOnlyViewWhenChatTabIsScrolled(t *testing.T) {
	m, tab := setupScrolledChatHistoryModel()
	term := tab.Terminal

	term.ScrollView(1)
	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}

	lines := snapshotLines(layer.Snap.Screen)
	if strings.Join(lines, "\n") != "old0\nold1\nold2\nold3" {
		t.Fatalf("expected scrolled chat view to render history only, got %q", lines)
	}
	for _, line := range lines {
		if strings.Contains(line, "prompt") || strings.Contains(line, "reply") {
			t.Fatalf("expected scrolled history to hide live prompt/output rows, got %q", lines)
		}
	}
}

func TestMouseSelectionOnScrolledChatUsesVisibleHistoryLines(t *testing.T) {
	m, tab := setupScrolledChatHistoryModel()
	term := tab.Terminal
	m.SetSize(40, 10)
	m.SetOffset(0)
	term.ScrollView(1)

	tm := m.terminalMetrics()
	startX := tm.ContentStartX
	startY := tm.ContentStartY
	endX := startX + term.Width - 1
	endY := startY + 1

	_, _ = m.Update(tea.MouseClickMsg{X: startX, Y: startY, Button: tea.MouseLeft})
	_, _ = m.Update(tea.MouseMotionMsg{X: endX, Y: endY, Button: tea.MouseLeft})

	tab.mu.Lock()
	got := tab.Terminal.SelectedText()
	tab.mu.Unlock()

	if got != "old0\nold1" {
		t.Fatalf("expected scrolled chat selection to follow visible history, got %q", got)
	}
}

func TestTabActorSelectionOnScrolledChatUsesVisibleHistoryLines(t *testing.T) {
	m, tab := setupScrolledChatHistoryModel()
	term := tab.Terminal
	term.ScrollView(1)
	workspaceID := string(tab.Workspace.ID())

	m.handleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: workspaceID,
		tabID:       tab.ID,
		kind:        tabEventSelectionStart,
		termX:       0,
		termY:       0,
		inBounds:    true,
	})
	m.handleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: workspaceID,
		tabID:       tab.ID,
		kind:        tabEventSelectionUpdate,
		termX:       term.Width - 1,
		termY:       1,
	})

	tab.mu.Lock()
	got := tab.Terminal.SelectedText()
	tab.mu.Unlock()

	if got != "old0\nold1" {
		t.Fatalf("expected tab actor scrolled chat selection to follow visible history, got %q", got)
	}
}

func TestCenterViewUsesHistoryOnlyRenderWhenChatTabIsScrolled(t *testing.T) {
	m, tab := setupScrolledChatHistoryModel()
	tab.Terminal.ScrollView(1)
	m.showKeymapHints = false
	m.SetSize(40, 10)
	m.SetOffset(0)

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "old0") {
		t.Fatalf("expected scrolled center view to include history rows, got %q", view)
	}
	if strings.Contains(view, "> prompt") || strings.Contains(view, "reply1") || strings.Contains(view, "reply2") {
		t.Fatalf("expected scrolled center view to hide live prompt/output rows, got %q", view)
	}
}

func TestTerminalLayerUsesFrozenHistoryViewDuringSyncOutput(t *testing.T) {
	m, tab := setupScrolledChatHistoryModel()
	term := tab.Terminal
	term.ScrollView(1)

	before := layerLines(t, m)

	term.Write([]byte("\x1b[?2026h"))
	if !term.SyncActive() {
		t.Fatal("expected synchronized output mode to be active")
	}
	term.Write([]byte("reply3\nreply4\nreply5\n"))

	during := layerLines(t, m)
	if strings.Join(during, "\n") != strings.Join(before, "\n") {
		t.Fatalf("expected scrolled chat layer to stay frozen during sync, before=%q during=%q", before, during)
	}

	term.Write([]byte("\x1b[?2026l"))
	after := layerLines(t, m)
	if strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("expected scrolled chat layer to preserve anchor after sync, before=%q after=%q", before, after)
	}
}

func TestMouseSelectionOnScrolledChatUsesFrozenHistoryDuringSyncOutput(t *testing.T) {
	m, tab := setupScrolledChatHistoryModel()
	term := tab.Terminal
	m.SetSize(40, 10)
	m.SetOffset(0)
	term.ScrollView(1)

	term.Write([]byte("\x1b[?2026h"))
	if !term.SyncActive() {
		t.Fatal("expected synchronized output mode to be active")
	}
	term.Write([]byte("reply3\nreply4\nreply5\n"))

	tm := m.terminalMetrics()
	startX := tm.ContentStartX
	startY := tm.ContentStartY
	endX := startX + term.Width - 1
	endY := startY + 1

	_, _ = m.Update(tea.MouseClickMsg{X: startX, Y: startY, Button: tea.MouseLeft})
	_, _ = m.Update(tea.MouseMotionMsg{X: endX, Y: endY, Button: tea.MouseLeft})

	tab.mu.Lock()
	got := tab.Terminal.SelectedText()
	tab.mu.Unlock()

	if got != "old0\nold1" {
		t.Fatalf("expected scrolled chat selection to follow frozen history during sync, got %q", got)
	}
}

func TestScrolledChatHistoryScreenYToAbsoluteLineRejectsPaddedRows(t *testing.T) {
	_, tab := setupScrolledChatHistoryModelWithBuffers(
		[]string{"old0", "old1"},
		[]string{"old2", "> prompt", "reply1", "reply2"},
	)
	tab.Terminal.ScrollView(1)

	if absLine, ok := scrolledChatHistoryScreenYToAbsoluteLine(tab.Terminal, 1); !ok || absLine != 1 {
		t.Fatalf("expected last real history row to map to line 1, got line=%d ok=%v", absLine, ok)
	}
	if absLine, ok := scrolledChatHistoryScreenYToAbsoluteLine(tab.Terminal, 2); ok || absLine != 1 {
		t.Fatalf("expected padded history row to be invalid and clamp to line 1, got line=%d ok=%v", absLine, ok)
	}
}

func TestMouseSelectionOnScrolledChatIgnoresPaddedHistoryRows(t *testing.T) {
	m, tab := setupScrolledChatHistoryModelWithBuffers(
		[]string{"old0", "old1"},
		[]string{"old2", "> prompt", "reply1", "reply2"},
	)
	term := tab.Terminal
	term.ScrollView(1)

	tm := m.terminalMetrics()
	startX := tm.ContentStartX
	startY := tm.ContentStartY + 2
	endX := startX + term.Width - 1
	endY := startY + 1

	_, _ = m.Update(tea.MouseClickMsg{X: startX, Y: startY, Button: tea.MouseLeft})
	_, _ = m.Update(tea.MouseMotionMsg{X: endX, Y: endY, Button: tea.MouseLeft})

	tab.mu.Lock()
	active := tab.Selection.Active
	hasSelection := tab.Terminal.HasSelection()
	tab.mu.Unlock()

	if active || hasSelection {
		t.Fatalf("expected padded history rows to avoid starting a selection, active=%v hasSelection=%v", active, hasSelection)
	}
}

func TestTabActorSelectionOnScrolledChatIgnoresPaddedHistoryRows(t *testing.T) {
	m, tab := setupScrolledChatHistoryModelWithBuffers(
		[]string{"old0", "old1"},
		[]string{"old2", "> prompt", "reply1", "reply2"},
	)
	term := tab.Terminal
	term.ScrollView(1)
	workspaceID := string(tab.Workspace.ID())

	m.handleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: workspaceID,
		tabID:       tab.ID,
		kind:        tabEventSelectionStart,
		termX:       0,
		termY:       2,
		inBounds:    true,
	})
	m.handleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: workspaceID,
		tabID:       tab.ID,
		kind:        tabEventSelectionUpdate,
		termX:       term.Width - 1,
		termY:       3,
	})

	tab.mu.Lock()
	active := tab.Selection.Active
	hasSelection := tab.Terminal.HasSelection()
	tab.mu.Unlock()

	if active || hasSelection {
		t.Fatalf("expected padded history rows to avoid starting an actor selection, active=%v hasSelection=%v", active, hasSelection)
	}
}

func TestChatHistoryScrollStopsAtLastDistinctFrame(t *testing.T) {
	m, tab := setupScrolledChatHistoryModelWithBuffers(
		[]string{"old0", "old1", "old2", "old3", "old4"},
		[]string{"old5", "> prompt", "reply1", "reply2"},
	)
	m.showKeymapHints = false

	tab.mu.Lock()
	for i := 0; i < 6; i++ {
		m.scrollTerminalViewLocked(tab, 1)
	}
	offset, maxOffset := m.displayedScrollInfoLocked(tab)
	tab.mu.Unlock()
	if offset != 2 || maxOffset != 2 {
		t.Fatalf("expected chat history scroll to clamp at 2/2, got %d/%d", offset, maxOffset)
	}

	lines := layerLines(t, m)
	if strings.Join(lines, "\n") != "old0\nold1\nold2\nold3" {
		t.Fatalf("expected top history frame after repeated wheel-up, got %q", lines)
	}

	status := ansi.Strip(m.ActiveTerminalStatusLine())
	if !strings.Contains(status, "2/2 lines up") {
		t.Fatalf("expected capped chat scroll status, got %q", status)
	}
}

func setupScrolledChatHistoryModel() (*Model, *Tab) {
	return setupScrolledChatHistoryModelWithBuffers(
		[]string{"old0", "old1", "old2", "old3"},
		[]string{"old4", "> prompt", "reply1", "reply2"},
	)
}

func setupScrolledChatHistoryModelWithBuffers(scrollbackLines, screenLines []string) (*Model, *Tab) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 4)
	term.Scrollback = make([][]vterm.Cell, 0, len(scrollbackLines))
	for _, line := range scrollbackLines {
		term.Scrollback = append(term.Scrollback, testHistoryLine(term.Width, line))
	}
	term.Screen = make([][]vterm.Cell, term.Height)
	for i := 0; i < term.Height; i++ {
		text := ""
		if i < len(screenLines) {
			text = screenLines[i]
		}
		term.Screen[i] = testHistoryLine(term.Width, text)
	}

	tab := &Tab{
		ID:        TabID("tab-chat-scrolled-history-only"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  term,
		Running:   true,
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()
	return m, tab
}

func testHistoryLine(width int, text string) []vterm.Cell {
	line := vterm.MakeBlankLine(width)
	for i, r := range text {
		if i >= width {
			break
		}
		cell := line[i]
		cell.Rune = r
		if cell.Width == 0 {
			cell.Width = 1
		}
		line[i] = cell
	}
	return line
}

func snapshotLines(screen [][]vterm.Cell) []string {
	lines := make([]string, 0, len(screen))
	for _, row := range screen {
		var b strings.Builder
		for _, cell := range row {
			if cell.Width == 0 {
				continue
			}
			if cell.Rune == 0 {
				b.WriteRune(' ')
			} else {
				b.WriteRune(cell.Rune)
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return lines
}

func layerLines(t *testing.T, m *Model) []string {
	t.Helper()
	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	return snapshotLines(layer.Snap.Screen)
}
