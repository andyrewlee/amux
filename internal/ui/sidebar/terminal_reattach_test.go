package sidebar

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestReattachPrependsScrollbackWhenCaptureExcludesScreen(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()

	m.tabsByWorkspace[wsID] = []*TerminalTab{
		{
			ID: tabID,
			State: &TerminalState{
				SessionName: "session-1",
				Running:     false,
				Detached:    true,
			},
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	msg := SidebarTerminalReattachResult{
		WorkspaceID: wsID,
		TabID:       tabID,
		SessionName: "session-1",
		Scrollback:  []byte("line-1\nline-2\n"),
	}

	_, _ = m.Update(msg)

	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil || tab.State.VTerm == nil {
		t.Fatal("expected vterm to be created on reattach")
	}
	if len(tab.State.VTerm.Scrollback) == 0 {
		t.Fatal("expected scrollback to be prepended on reattach")
	}
}

func TestReattachHistoryOnlyResizesExistingVTermBeforeRecordingSize(t *testing.T) {
	m := NewTerminalModel()
	m.width = 12
	m.height = 5
	currentWidth, currentHeight := m.terminalContentSize()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	oldWidth, oldHeight := 6, 2

	m.tabsByWorkspace[wsID] = []*TerminalTab{
		{
			ID: tabID,
			State: &TerminalState{
				SessionName: "session-1",
				Running:     false,
				Detached:    true,
				VTerm:       vterm.New(oldWidth, oldHeight),
				lastWidth:   oldWidth,
				lastHeight:  oldHeight,
			},
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	msg := SidebarTerminalReattachResult{
		WorkspaceID: wsID,
		TabID:       tabID,
		SessionName: "session-1",
		Scrollback:  []byte("line-1\nline-2\n"),
	}

	_, _ = m.Update(msg)

	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil || tab.State.VTerm == nil {
		t.Fatal("expected vterm to be preserved on reattach")
	}
	if tab.State.VTerm.Width != currentWidth || tab.State.VTerm.Height != currentHeight {
		t.Fatalf("expected history-only reattach to resize reused vterm to %dx%d, got %dx%d", currentWidth, currentHeight, tab.State.VTerm.Width, tab.State.VTerm.Height)
	}
	if tab.State.lastWidth != currentWidth || tab.State.lastHeight != currentHeight {
		t.Fatalf("expected stored terminal size to match resized vterm, got %dx%d", tab.State.lastWidth, tab.State.lastHeight)
	}
	if len(tab.State.VTerm.Scrollback) == 0 {
		t.Fatal("expected scrollback to be prepended after resize")
	}
}

func TestReattachLoadsPaneCapture(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	term := vterm.New(20, 2)
	term.LoadPaneCapture([]byte("old history\nstale one\nstale two\n"))

	m.tabsByWorkspace[wsID] = []*TerminalTab{
		{
			ID: tabID,
			State: &TerminalState{
				SessionName: "session-1",
				Running:     false,
				Detached:    true,
				VTerm:       term,
			},
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	msg := SidebarTerminalReattachResult{
		WorkspaceID:     wsID,
		TabID:           tabID,
		SessionName:     "session-1",
		Scrollback:      []byte("line-1\nline-2\n"),
		CaptureFullPane: true,
	}

	_, _ = m.Update(msg)

	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil || tab.State.VTerm == nil {
		t.Fatal("expected vterm to be created on reattach")
	}
	if len(tab.State.VTerm.Scrollback) != 0 {
		t.Fatalf("expected stale scrollback to be replaced by a 2-line pane capture, got %d lines", len(tab.State.VTerm.Scrollback))
	}
	if got := tab.State.VTerm.Screen[0][0].Rune; got != 'l' {
		t.Fatalf("expected first visible row to start with pane capture, got %q", got)
	}
	if got := tab.State.VTerm.Screen[1][0].Rune; got != 'l' {
		t.Fatalf("expected second visible row to start with pane capture, got %q", got)
	}

	firstLen := len(tab.State.VTerm.Scrollback)
	_, _ = m.Update(msg)
	if len(tab.State.VTerm.Scrollback) != firstLen {
		t.Fatal("expected scrollback not to duplicate on reattach")
	}
}

func TestReattachReconcilesPostAttachHistoryAfterFullPaneRestore(t *testing.T) {
	m := NewTerminalModel()
	m.width = 20
	m.height = 5
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()

	m.tabsByWorkspace[wsID] = []*TerminalTab{
		{
			ID: tabID,
			State: &TerminalState{
				SessionName: "session-1",
				Running:     false,
				Detached:    true,
				VTerm:       vterm.New(20, 2),
			},
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	msg := SidebarTerminalReattachResult{
		WorkspaceID:          wsID,
		TabID:                tabID,
		SessionName:          "session-1",
		Scrollback:           []byte("history\nscreen zero\nscreen one\nscreen two\n"),
		PostAttachScrollback: []byte("history\nscreen zero\n"),
		CaptureFullPane:      true,
		SnapshotCols:         20,
		SnapshotRows:         3,
	}

	_, _ = m.Update(msg)

	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil || tab.State.VTerm == nil {
		t.Fatal("expected vterm to be created on reattach")
	}
	if len(tab.State.VTerm.Scrollback) != 2 {
		t.Fatalf("expected post-attach history suffix to be appended, got %d lines", len(tab.State.VTerm.Scrollback))
	}
	if got := tab.State.VTerm.Scrollback[0][0].Rune; got != 'h' {
		t.Fatalf("expected original history row to remain first, got %q", got)
	}
	if got := tab.State.VTerm.Scrollback[1][0].Rune; got != 's' {
		t.Fatalf("expected newly scrolled row to be reconciled into history, got %q", got)
	}
	if got := tab.State.VTerm.Screen[0][0].Rune; got != 's' {
		t.Fatalf("expected authoritative visible frame to remain intact, got %q", got)
	}
}

func TestHandleTerminalCreated_LoadsPaneCaptureBeforeStartingReader(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	m.workspace = ws
	m.width = 20
	m.height = 4

	_, _ = m.Update(SidebarTerminalCreated{
		WorkspaceID:       wsID,
		TabID:             tabID,
		SessionName:       "session-1",
		Scrollback:        []byte("history\nscreen zero\nscreen one\nscreen two\n"),
		CaptureFullPane:   true,
		SnapshotCursorX:   5,
		SnapshotCursorY:   2,
		SnapshotHasCursor: true,
	})

	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil || tab.State.VTerm == nil {
		t.Fatal("expected vterm to be created on terminal creation")
	}
	if len(tab.State.VTerm.Scrollback) != 1 {
		t.Fatalf("expected one scrollback line from pane capture, got %d", len(tab.State.VTerm.Scrollback))
	}
	if got := tab.State.VTerm.Scrollback[0][0].Rune; got != 'h' {
		t.Fatalf("expected historical line to remain in scrollback, got %q", got)
	}
	if got := tab.State.VTerm.Screen[0][0].Rune; got != 's' {
		t.Fatalf("expected first visible row to be restored from pane capture, got %q", got)
	}
	if tab.State.VTerm.CursorX != 5 || tab.State.VTerm.CursorY != 2 {
		t.Fatalf("expected explicit pane cursor to be restored, got (%d,%d)", tab.State.VTerm.CursorX, tab.State.VTerm.CursorY)
	}
	if tab.State.readerActive {
		t.Fatal("expected reader not to start without a PTY terminal")
	}
}

func TestHandleTerminalCreated_BlankPaneCaptureClearsStaleFrame(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	m.workspace = ws
	m.width = 20
	m.height = 4

	_, _ = m.Update(SidebarTerminalCreated{
		WorkspaceID:       wsID,
		TabID:             tabID,
		SessionName:       "session-blank",
		CaptureFullPane:   true,
		SnapshotCursorX:   1,
		SnapshotCursorY:   0,
		SnapshotHasCursor: true,
	})

	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil || tab.State.VTerm == nil {
		t.Fatal("expected vterm to be created on terminal creation")
	}
	if len(tab.State.VTerm.Scrollback) != 0 {
		t.Fatalf("expected blank authoritative pane capture to clear scrollback, got %d lines", len(tab.State.VTerm.Scrollback))
	}
	if got := tab.State.VTerm.Screen[0][0].Rune; got != ' ' {
		t.Fatalf("expected blank authoritative pane capture to blank the visible frame, got %q", got)
	}
	if tab.State.VTerm.CursorX != 1 || tab.State.VTerm.CursorY != 0 {
		t.Fatalf("expected explicit cursor restored on blank pane capture, got (%d,%d)", tab.State.VTerm.CursorX, tab.State.VTerm.CursorY)
	}
}

func TestReattachBlankPaneCaptureClearsStaleFrame(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	term := vterm.New(20, 2)
	term.LoadPaneCapture([]byte("old history\nstale one\nstale two\n"))

	m.tabsByWorkspace[wsID] = []*TerminalTab{
		{
			ID: tabID,
			State: &TerminalState{
				SessionName: "session-1",
				Running:     false,
				Detached:    true,
				VTerm:       term,
			},
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	msg := SidebarTerminalReattachResult{
		WorkspaceID:       wsID,
		TabID:             tabID,
		SessionName:       "session-1",
		CaptureFullPane:   true,
		SnapshotCursorX:   2,
		SnapshotCursorY:   0,
		SnapshotHasCursor: true,
	}

	_, _ = m.Update(msg)

	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil || tab.State.VTerm == nil {
		t.Fatal("expected vterm to be preserved on reattach")
	}
	if len(tab.State.VTerm.Scrollback) != 0 {
		t.Fatalf("expected blank authoritative snapshot to clear stale scrollback, got %d lines", len(tab.State.VTerm.Scrollback))
	}
	if got := tab.State.VTerm.Screen[0][0].Rune; got != ' ' {
		t.Fatalf("expected blank authoritative snapshot to clear stale screen, got %q", got)
	}
	if tab.State.VTerm.CursorX != 2 || tab.State.VTerm.CursorY != 0 {
		t.Fatalf("expected explicit cursor restored on blank reattach, got (%d,%d)", tab.State.VTerm.CursorX, tab.State.VTerm.CursorY)
	}
}

func TestReattachPreservesExistingAltScreenStateWithoutSnapshotModes(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	term := vterm.New(6, 3)
	term.Write([]byte("shell"))
	term.Write([]byte("\x1b[?1049h"))
	term.Write([]byte("tui"))
	term.SavedCursorX = 5
	term.SavedCursorY = 1
	term.SavedStyle = vterm.Style{Bold: true}

	m.tabsByWorkspace[wsID] = []*TerminalTab{
		{
			ID: tabID,
			State: &TerminalState{
				SessionName: "session-1",
				Running:     false,
				Detached:    true,
				VTerm:       term,
			},
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	msg := SidebarTerminalReattachResult{
		WorkspaceID:       wsID,
		TabID:             tabID,
		SessionName:       "session-1",
		Scrollback:        []byte("one\ntwo\nthree"),
		CaptureFullPane:   true,
		SnapshotCursorX:   0,
		SnapshotCursorY:   2,
		SnapshotHasCursor: true,
	}

	_, _ = m.Update(msg)

	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil || tab.State.VTerm == nil {
		t.Fatal("expected vterm to be preserved on reattach")
	}
	if !tab.State.VTerm.AltScreen {
		t.Fatal("expected missing snapshot modes to preserve existing alt-screen state")
	}
	if tab.State.VTerm.SavedCursorX == 5 && tab.State.VTerm.SavedCursorY == 1 {
		t.Fatalf("expected stale saved cursor state to be reset, got (%d,%d)", tab.State.VTerm.SavedCursorX, tab.State.VTerm.SavedCursorY)
	}

	tab.State.VTerm.Write([]byte("\x1b[?1049l"))

	if tab.State.VTerm.AltScreen {
		t.Fatal("expected later 1049l to exit alt screen")
	}
	if got := tab.State.VTerm.Screen[0][0].Rune; got != 's' {
		t.Fatalf("expected preserved main-screen buffer to survive missing snapshot modes, got %q", got)
	}
}

func TestHandleTerminalCreated_UsesSnapshotDimensionsForFullPaneRestore(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	m.workspace = ws
	m.width = 12
	m.height = 6

	currentWidth, currentHeight := m.terminalContentSize()
	snapshotWidth, snapshotHeight := 5, 2
	capture := []byte("1234567890\nABCDE\nFGHIJ\n")

	expected := vterm.New(snapshotWidth, snapshotHeight)
	expected.AllowAltScreenScrollback = true
	expected.LoadPaneCaptureWithCursorAndModes(capture, 0, 1, true, vterm.PaneModeState{})
	expected.Resize(currentWidth, currentHeight)

	_, _ = m.Update(SidebarTerminalCreated{
		WorkspaceID:       wsID,
		TabID:             tabID,
		SessionName:       "session-sized",
		Scrollback:        capture,
		CaptureFullPane:   true,
		SnapshotCols:      snapshotWidth,
		SnapshotRows:      snapshotHeight,
		SnapshotCursorX:   0,
		SnapshotCursorY:   1,
		SnapshotHasCursor: true,
	})

	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil || tab.State.VTerm == nil {
		t.Fatal("expected vterm to be created on terminal creation")
	}

	actualScreen := renderLines(tab.State.VTerm.Screen)
	expectedScreen := renderLines(expected.Screen)
	if actualScreen != expectedScreen {
		t.Fatalf("expected snapshot to be parsed at %dx%d before resizing to %dx%d\nwant:\n%s\n\ngot:\n%s", snapshotWidth, snapshotHeight, currentWidth, currentHeight, expectedScreen, actualScreen)
	}
	if len(tab.State.VTerm.Scrollback) != len(expected.Scrollback) {
		t.Fatalf("expected scrollback len %d after snapshot-sized restore, got %d", len(expected.Scrollback), len(tab.State.VTerm.Scrollback))
	}
}

func renderLines(lines [][]vterm.Cell) string {
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		var b strings.Builder
		for _, cell := range line {
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
		rendered = append(rendered, b.String())
	}
	return strings.Join(rendered, "\n")
}
