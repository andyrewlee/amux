package center

import (
	"strings"
	"testing"

	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/vterm"
)

func captureRowText(line []vterm.Cell, width int) string {
	if width > len(line) {
		width = len(line)
	}
	var b strings.Builder
	b.Grow(width)
	for i := 0; i < width; i++ {
		r := line[i].Rune
		if r == 0 {
			r = ' '
		}
		b.WriteRune(r)
	}
	return b.String()
}

func captureFillLine(fill rune, width int) string {
	if width <= 0 {
		return ""
	}
	return strings.Repeat(string(fill), width)
}

func TestUpdatePtyTabReattachResult_NormalizesCapturedPaneLFForChatTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-reattach-lf"),
		Assistant: "codex",
		Workspace: ws,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID:       wsID,
		TabID:             tab.ID,
		Agent:             &appPty.Agent{Session: "sess-reattach-lf"},
		Rows:              24,
		Cols:              80,
		ScrollbackCapture: []byte("abc\nx"),
		CaptureFullPane:   true,
	})

	if tab.Terminal == nil {
		t.Fatal("expected terminal to be created")
	}
	if got := tab.Terminal.Screen[1][0].Rune; got != 'x' {
		t.Fatalf("expected captured pane LF to reset to col 0, got %q", got)
	}
}

func TestUpdatePtyTabReattachResult_LoadsPaneCaptureIntoVisibleScreen(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 2)
	term.LoadPaneCapture([]byte("old history\nstale one\nstale two\n"))
	tab := &Tab{
		ID:            TabID("tab-reattach-pane-capture"),
		Assistant:     "codex",
		Workspace:     ws,
		Terminal:      term,
		pendingOutput: []byte("buffered"),
	}
	tab.pendingOutputBytes = len(tab.pendingOutput)
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID:       wsID,
		TabID:             tab.ID,
		Agent:             &appPty.Agent{Session: "sess-reattach-pane-capture"},
		Rows:              2,
		Cols:              20,
		ScrollbackCapture: []byte("history\nscreen one\nscreen two\n"),
		CaptureFullPane:   true,
	})

	if tab.Terminal == nil {
		t.Fatal("expected terminal to be created")
	}
	if len(tab.Terminal.Scrollback) != 1 {
		t.Fatalf("expected 1 scrollback line, got %d", len(tab.Terminal.Scrollback))
	}
	if got := tab.Terminal.Scrollback[0][0].Rune; got != 'h' {
		t.Fatalf("expected stale scrollback to be replaced by pane capture, got %q", got)
	}
	if got := tab.Terminal.Screen[0][0].Rune; got != 's' {
		t.Fatalf("expected first visible row to start with screen data, got %q", got)
	}
	if got := tab.Terminal.Screen[1][0].Rune; got != 's' {
		t.Fatalf("expected second visible row to start with screen data, got %q", got)
	}
	if len(tab.pendingOutput) != 0 || tab.pendingOutputBytes != 0 {
		t.Fatalf("expected full-pane restore to clear preserved PTY backlog, got %q (%d bytes)", tab.pendingOutput, tab.pendingOutputBytes)
	}
}

func TestUpdatePtyTabReattachResult_ReconcilesPostAttachHistoryAfterFullPaneRestore(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-reattach-history-reconcile"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(20, 2),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID:                 wsID,
		TabID:                       tab.ID,
		Agent:                       &appPty.Agent{Session: "sess-reattach-history-reconcile"},
		Rows:                        2,
		Cols:                        20,
		ScrollbackCapture:           []byte("history\nscreen one\nscreen two\n"),
		PostAttachScrollbackCapture: []byte("history\nscreen one\n"),
		CaptureFullPane:             true,
	})

	if len(tab.Terminal.Scrollback) != 2 {
		t.Fatalf("expected post-attach history suffix to be appended, got %d rows", len(tab.Terminal.Scrollback))
	}
	if got := tab.Terminal.Scrollback[0][0].Rune; got != 'h' {
		t.Fatalf("expected original history row to remain first, got %q", got)
	}
	if got := tab.Terminal.Scrollback[1][0].Rune; got != 's' {
		t.Fatalf("expected newly scrolled row to be reconciled into history, got %q", got)
	}
	if got := tab.Terminal.Screen[0][0].Rune; got != 's' {
		t.Fatalf("expected authoritative visible frame to remain intact, got %q", got)
	}
}

func TestUpdatePtyTabReattachResult_ResizesExistingTerminalBeforeFullPaneRestore(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(4, 2)
	tab := &Tab{
		ID:        TabID("tab-reattach-pane-resize"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  term,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID:       wsID,
		TabID:             tab.ID,
		Agent:             &appPty.Agent{Session: "sess-reattach-pane-resize"},
		Rows:              2,
		Cols:              8,
		ScrollbackCapture: []byte("history\n12345678\nabcdefgh\n"),
		CaptureFullPane:   true,
	})

	if tab.Terminal == nil {
		t.Fatal("expected terminal to be preserved")
	}
	if got := tab.Terminal.Width; got != 8 {
		t.Fatalf("expected reattached terminal width 8, got %d", got)
	}
	if got := captureRowText(tab.Terminal.Screen[0], 8); got != "12345678" {
		t.Fatalf("expected first visible row to use restored width, got %q", got)
	}
	if got := captureRowText(tab.Terminal.Screen[1], 8); got != "abcdefgh" {
		t.Fatalf("expected second visible row to use restored width, got %q", got)
	}
}

func TestUpdatePtyTabReattachResult_NonAuthoritativeEmptyCapturePreservesExistingFrame(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 2)
	term.LoadPaneCapture([]byte("old history\nstale one\nstale two\n"))
	tab := &Tab{
		ID:            TabID("tab-reattach-snapshot-fallback"),
		Assistant:     "codex",
		Workspace:     ws,
		Terminal:      term,
		pendingOutput: []byte("buffered"),
	}
	tab.pendingOutputBytes = len(tab.pendingOutput)
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Agent:       &appPty.Agent{Session: "sess-reattach-snapshot-fallback"},
		Rows:        2,
		Cols:        20,
	})

	if tab.Terminal == nil {
		t.Fatal("expected terminal to be preserved")
	}
	if len(tab.Terminal.Scrollback) != 1 {
		t.Fatalf("expected existing scrollback to remain when full snapshot capture fails, got %d lines", len(tab.Terminal.Scrollback))
	}
	if got := tab.Terminal.Scrollback[0][0].Rune; got != 'o' {
		t.Fatalf("expected existing scrollback to remain untouched, got %q", got)
	}
	if got := tab.Terminal.Screen[0][0].Rune; got != 's' {
		t.Fatalf("expected existing visible frame to remain untouched, got %q", got)
	}
	if len(tab.pendingOutput) != len([]byte("buffered")) || tab.pendingOutputBytes != len([]byte("buffered")) {
		t.Fatalf("expected non-authoritative fallback to preserve PTY backlog, got %q (%d bytes)", tab.pendingOutput, tab.pendingOutputBytes)
	}
}

func TestUpdatePtyTabReattachResult_PreservesExistingAltScreenStateWithoutSnapshotModes(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(6, 3)
	term.Write([]byte("shell"))
	term.Write([]byte("\x1b[?1049h"))
	term.Write([]byte("tui"))

	tab := &Tab{
		ID:        TabID("tab-reattach-missing-modes"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  term,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID:       wsID,
		TabID:             tab.ID,
		Agent:             &appPty.Agent{Session: "sess-reattach-missing-modes"},
		Rows:              3,
		Cols:              6,
		ScrollbackCapture: []byte("one\ntwo\nthree\n"),
		CaptureFullPane:   true,
		SnapshotCursorX:   0,
		SnapshotCursorY:   2,
		SnapshotHasCursor: true,
	})

	if !tab.Terminal.AltScreen {
		t.Fatal("expected missing snapshot modes to preserve existing alt-screen state")
	}

	tab.Terminal.Write([]byte("\x1b[?1049l"))

	if tab.Terminal.AltScreen {
		t.Fatal("expected later 1049l to exit preserved alt-screen state")
	}
	if got := tab.Terminal.Screen[0][0].Rune; got != 's' {
		t.Fatalf("expected preserved hidden main screen after unknown-mode restore, got %q", got)
	}
}

func TestHandlePtyTabCreated_NewTabNormalizesCapturedPaneLFForChatTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace:         ws,
		Assistant:         "codex",
		Agent:             &appPty.Agent{Session: "sess-created-lf"},
		TabID:             TabID("tab-created-lf"),
		Rows:              24,
		Cols:              80,
		Activate:          true,
		ScrollbackCapture: []byte("abc\nx"),
		CaptureFullPane:   true,
	})

	tabs := m.tabsByWorkspace[wsID]
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	tab := tabs[0]
	if tab.Terminal == nil {
		t.Fatal("expected terminal to be created")
	}
	if got := tab.Terminal.Screen[1][0].Rune; got != 'x' {
		t.Fatalf("expected captured pane LF to reset to col 0, got %q", got)
	}
}

func TestHandlePtyTabCreated_LoadsPaneCaptureIntoVisibleScreen(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace:         ws,
		Assistant:         "codex",
		Agent:             &appPty.Agent{Session: "sess-created-pane-capture"},
		TabID:             TabID("tab-created-pane-capture"),
		Rows:              2,
		Cols:              20,
		Activate:          true,
		ScrollbackCapture: []byte("history\nscreen one\nscreen two\n"),
		CaptureFullPane:   true,
	})

	tabs := m.tabsByWorkspace[wsID]
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	tab := tabs[0]
	if tab.Terminal == nil {
		t.Fatal("expected terminal to be created")
	}
	if len(tab.Terminal.Scrollback) != 1 {
		t.Fatalf("expected 1 scrollback line, got %d", len(tab.Terminal.Scrollback))
	}
	if got := tab.Terminal.Screen[0][0].Rune; got != 's' {
		t.Fatalf("expected first visible row to start with screen data, got %q", got)
	}
	if got := tab.Terminal.Screen[1][0].Rune; got != 's' {
		t.Fatalf("expected second visible row to start with screen data, got %q", got)
	}
}

func TestHandlePtyTabCreated_ReplacesExistingTerminalWithPaneCapture(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 2)
	term.LoadPaneCapture([]byte("old history\nstale one\nstale two\n"))
	tabID := TabID("tab-existing-pane-capture")
	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:            tabID,
			Name:          "codex",
			Assistant:     "codex",
			Workspace:     ws,
			Terminal:      term,
			pendingOutput: []byte("buffered"),
		},
	}
	m.tabsByWorkspace[wsID][0].pendingOutputBytes = len(m.tabsByWorkspace[wsID][0].pendingOutput)

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace:         ws,
		Assistant:         "codex",
		Agent:             &appPty.Agent{Session: "sess-existing-pane-capture"},
		TabID:             tabID,
		Rows:              2,
		Cols:              20,
		Activate:          true,
		ScrollbackCapture: []byte("history\nscreen one\nscreen two\n"),
		CaptureFullPane:   true,
	})

	tab := m.tabsByWorkspace[wsID][0]
	if tab.Terminal == nil {
		t.Fatal("expected terminal to be preserved")
	}
	if len(tab.Terminal.Scrollback) != 1 {
		t.Fatalf("expected 1 scrollback line, got %d", len(tab.Terminal.Scrollback))
	}
	if got := tab.Terminal.Scrollback[0][0].Rune; got != 'h' {
		t.Fatalf("expected stale scrollback to be replaced by pane capture, got %q", got)
	}
	if got := tab.Terminal.Screen[0][0].Rune; got != 's' {
		t.Fatalf("expected first visible row to start with screen data, got %q", got)
	}
	if got := tab.Terminal.Screen[1][0].Rune; got != 's' {
		t.Fatalf("expected second visible row to start with screen data, got %q", got)
	}
	if len(tab.pendingOutput) != 0 || tab.pendingOutputBytes != 0 {
		t.Fatalf("expected full-pane restore to clear preserved PTY backlog, got %q (%d bytes)", tab.pendingOutput, tab.pendingOutputBytes)
	}
}

func TestHandlePtyTabCreated_ResizesExistingTerminalBeforeFullPaneRestore(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(4, 2)
	tabID := TabID("tab-existing-pane-resize")
	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        tabID,
			Name:      "codex",
			Assistant: "codex",
			Workspace: ws,
			Terminal:  term,
		},
	}

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace:         ws,
		Assistant:         "codex",
		Agent:             &appPty.Agent{Session: "sess-existing-pane-resize"},
		TabID:             tabID,
		Rows:              2,
		Cols:              8,
		Activate:          true,
		ScrollbackCapture: []byte("history\n12345678\nabcdefgh\n"),
		CaptureFullPane:   true,
	})

	tab := m.tabsByWorkspace[wsID][0]
	if tab.Terminal == nil {
		t.Fatal("expected terminal to be preserved")
	}
	if got := tab.Terminal.Width; got != 8 {
		t.Fatalf("expected recreated terminal width 8, got %d", got)
	}
	if got := captureRowText(tab.Terminal.Screen[0], 8); got != "12345678" {
		t.Fatalf("expected first visible row to use restored width, got %q", got)
	}
	if got := captureRowText(tab.Terminal.Screen[1], 8); got != "abcdefgh" {
		t.Fatalf("expected second visible row to use restored width, got %q", got)
	}
}
