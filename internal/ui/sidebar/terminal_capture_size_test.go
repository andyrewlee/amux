package sidebar

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestReattach_UsesCaptureSizeBeforeResizingForHistoryOnly(t *testing.T) {
	m := NewTerminalModel()
	m.width = 12
	m.height = 5
	currentWidth, currentHeight := m.terminalContentSize()
	captureWidth := currentWidth + 8
	captureHeight := 2
	capture := []byte(sidebarCaptureFillLine('A', captureWidth) + "\n" + sidebarCaptureFillLine('B', captureWidth) + "\n")

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
				VTerm:       vterm.New(currentWidth, currentHeight),
			},
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_, _ = m.Update(SidebarTerminalReattachResult{
		WorkspaceID: wsID,
		TabID:       tabID,
		SessionName: "session-1",
		Scrollback:  capture,
		CaptureCols: captureWidth,
		CaptureRows: captureHeight,
	})

	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil || tab.State.VTerm == nil {
		t.Fatal("expected vterm to be preserved on reattach")
	}
	if got := tab.State.VTerm.Width; got != currentWidth {
		t.Fatalf("expected terminal width %d after live resize, got %d", currentWidth, got)
	}
	if len(tab.State.VTerm.Scrollback) != 2 {
		t.Fatalf("expected 2 scrollback lines from history-only reattach, got %d", len(tab.State.VTerm.Scrollback))
	}
	if got := sidebarCaptureRowText(tab.State.VTerm.Scrollback[0], captureWidth); got != sidebarCaptureFillLine('A', captureWidth) {
		t.Fatalf("expected first history row to preserve capture width before resize, got %q", got)
	}
	if got := sidebarCaptureRowText(tab.State.VTerm.Scrollback[1], captureWidth); got != sidebarCaptureFillLine('B', captureWidth) {
		t.Fatalf("expected second history row to preserve capture width before resize, got %q", got)
	}
}

func TestHandleTerminalCreated_UsesCaptureSizeBeforeResizingForHistoryOnly(t *testing.T) {
	m := NewTerminalModel()
	m.width = 12
	m.height = 5
	currentWidth, _ := m.terminalContentSize()
	captureWidth := currentWidth + 8
	captureHeight := 2
	capture := []byte(sidebarCaptureFillLine('M', captureWidth) + "\n" + sidebarCaptureFillLine('N', captureWidth) + "\n")

	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	m.workspace = ws

	_, _ = m.Update(SidebarTerminalCreated{
		WorkspaceID: wsID,
		TabID:       tabID,
		SessionName: "session-created",
		Scrollback:  capture,
		CaptureCols: captureWidth,
		CaptureRows: captureHeight,
	})

	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil || tab.State.VTerm == nil {
		t.Fatal("expected vterm to be created on terminal creation")
	}
	if got := tab.State.VTerm.Width; got != currentWidth {
		t.Fatalf("expected terminal width %d after live resize, got %d", currentWidth, got)
	}
	if len(tab.State.VTerm.Scrollback) != 2 {
		t.Fatalf("expected 2 scrollback lines from history-only create, got %d", len(tab.State.VTerm.Scrollback))
	}
	if got := sidebarCaptureRowText(tab.State.VTerm.Scrollback[0], captureWidth); got != sidebarCaptureFillLine('M', captureWidth) {
		t.Fatalf("expected first history row to preserve capture width before resize, got %q", got)
	}
	if got := sidebarCaptureRowText(tab.State.VTerm.Scrollback[1], captureWidth); got != sidebarCaptureFillLine('N', captureWidth) {
		t.Fatalf("expected second history row to preserve capture width before resize, got %q", got)
	}
}

func TestHandleTerminalCreated_RefreshesSizeAfterInsertWhenHintsShrinkViewport(t *testing.T) {
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())

	for width := 10; width <= 40; width++ {
		m := NewTerminalModel()
		m.workspace = ws
		m.width = width
		m.height = 8
		m.showKeymapHints = true

		preWidth, preHeight := m.terminalContentSize()
		tabID := generateTerminalTabID()
		_, _ = m.Update(SidebarTerminalCreated{
			WorkspaceID: wsID,
			TabID:       tabID,
			SessionName: "session-created",
		})

		postWidth, postHeight := m.terminalContentSize()
		if postHeight >= preHeight {
			continue
		}

		tab := m.getTabByID(wsID, tabID)
		if tab == nil || tab.State == nil || tab.State.VTerm == nil {
			t.Fatal("expected terminal state to be created")
		}
		if tab.State.lastWidth != postWidth || tab.State.lastHeight != postHeight {
			t.Fatalf("expected stored size to refresh to post-insert %dx%d, got %dx%d (pre-insert was %dx%d)", postWidth, postHeight, tab.State.lastWidth, tab.State.lastHeight, preWidth, preHeight)
		}
		if tab.State.VTerm.Width != postWidth || tab.State.VTerm.Height != postHeight {
			t.Fatalf("expected vterm size to refresh to post-insert %dx%d, got %dx%d (pre-insert was %dx%d)", postWidth, postHeight, tab.State.VTerm.Width, tab.State.VTerm.Height, preWidth, preHeight)
		}
		return
	}

	t.Fatal("expected a narrow hint-visible setup where first terminal creation shrinks the sidebar viewport")
}

func TestHandleTerminalCreated_DelaysPTYResizeUntilAfterFullPaneRestore(t *testing.T) {
	oldSetTerminalSizeFn := setTerminalSizeFn
	defer func() {
		setTerminalSizeFn = oldSetTerminalSizeFn
	}()

	m := NewTerminalModel()
	m.width = 12
	m.height = 5
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	m.workspace = ws

	currentWidth, _ := m.terminalContentSize()
	snapshotWidth := currentWidth + 8
	resizeCalls := 0
	setTerminalSizeFn = func(term *pty.Terminal, rows, cols uint16) error {
		resizeCalls++
		tab := m.getTabByID(wsID, tabID)
		if tab == nil || tab.State == nil || tab.State.VTerm == nil {
			t.Fatal("expected PTY resize only after sidebar tab state exists")
		}
		if got := tab.State.VTerm.Screen[0][0].Rune; got == 0 {
			t.Fatal("expected full-pane capture content to be present before PTY resize")
		}
		return nil
	}

	_, _ = m.Update(SidebarTerminalCreated{
		WorkspaceID:     wsID,
		TabID:           tabID,
		SessionName:     "session-full-pane",
		Terminal:        &pty.Terminal{},
		Scrollback:      []byte("history\nscreen one\nscreen two\n"),
		CaptureFullPane: true,
		SnapshotCols:    snapshotWidth,
		SnapshotRows:    2,
	})

	if resizeCalls == 0 {
		t.Fatal("expected live PTY resize after full-pane restore when snapshot size differs from the viewport")
	}
}

func TestHandleTerminalCreated_UsesSnapshotSizeBeforeSidebarLayout(t *testing.T) {
	oldSetTerminalSizeFn := setTerminalSizeFn
	defer func() {
		setTerminalSizeFn = oldSetTerminalSizeFn
	}()

	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	m.workspace = ws

	resizeCalls := 0
	setTerminalSizeFn = func(term *pty.Terminal, rows, cols uint16) error {
		resizeCalls++
		return nil
	}

	_, _ = m.Update(SidebarTerminalCreated{
		WorkspaceID:     wsID,
		TabID:           tabID,
		SessionName:     "session-full-pane",
		Terminal:        &pty.Terminal{},
		Scrollback:      []byte("history\nscreen one\nscreen two\n"),
		CaptureFullPane: true,
		SnapshotCols:    18,
		SnapshotRows:    4,
	})

	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil || tab.State.VTerm == nil {
		t.Fatal("expected terminal state to be created")
	}
	if tab.State.VTerm.Width != 18 || tab.State.VTerm.Height != 4 {
		t.Fatalf("expected full-pane startup restore to keep snapshot size before layout, got %dx%d", tab.State.VTerm.Width, tab.State.VTerm.Height)
	}
	if tab.State.lastWidth != 18 || tab.State.lastHeight != 4 {
		t.Fatalf("expected stored size to keep snapshot size before layout, got %dx%d", tab.State.lastWidth, tab.State.lastHeight)
	}
	if resizeCalls != 0 {
		t.Fatalf("expected no PTY resize before the real sidebar layout is known, got %d calls", resizeCalls)
	}
}

func TestHandleReattachResult_UsesSnapshotSizeBeforeSidebarLayout(t *testing.T) {
	oldSetTerminalSizeFn := setTerminalSizeFn
	defer func() {
		setTerminalSizeFn = oldSetTerminalSizeFn
	}()

	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	m.workspace = ws
	m.tabsByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: &TerminalState{SessionName: "session-1"}}}
	m.activeTabByWorkspace[wsID] = 0

	var gotRows, gotCols uint16
	setTerminalSizeFn = func(term *pty.Terminal, rows, cols uint16) error {
		gotRows, gotCols = rows, cols
		return nil
	}

	_, _ = m.Update(SidebarTerminalReattachResult{
		WorkspaceID:     wsID,
		TabID:           tabID,
		SessionName:     "session-1",
		Terminal:        &pty.Terminal{},
		Scrollback:      []byte("history\nscreen one\nscreen two\n"),
		CaptureFullPane: true,
		SnapshotCols:    18,
		SnapshotRows:    4,
	})

	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil || tab.State.VTerm == nil {
		t.Fatal("expected reattached terminal state to exist")
	}
	if tab.State.VTerm.Width != 18 || tab.State.VTerm.Height != 4 {
		t.Fatalf("expected full-pane reattach restore to keep snapshot size before layout, got %dx%d", tab.State.VTerm.Width, tab.State.VTerm.Height)
	}
	if tab.State.lastWidth != 18 || tab.State.lastHeight != 4 {
		t.Fatalf("expected stored size to keep snapshot size before layout, got %dx%d", tab.State.lastWidth, tab.State.lastHeight)
	}
	if gotCols != 18 || gotRows != 4 {
		t.Fatalf("expected PTY resize to use snapshot size before layout, got %dx%d", gotCols, gotRows)
	}
}

func TestRestoreSidebarScrollbackCapture_ScrollsToBottomBeforePrependingHistory(t *testing.T) {
	vt := vterm.New(20, 2)
	vt.LoadPaneCapture([]byte("oldest\nolder\nscreen one\nscreen two\n"))
	vt.ScrollViewToTop()
	if vt.ViewOffset == 0 {
		t.Fatal("expected seeded terminal to start scrolled into history")
	}

	common.RestoreScrollbackCapture(vt, []byte("ancient\n"), 20, 1, 20, 2)

	if vt.ViewOffset != 0 {
		t.Fatalf("expected history-only restore to land at live bottom, got view offset %d", vt.ViewOffset)
	}
	if got := sidebarCaptureRowText(vt.Scrollback[0], 20); !strings.HasPrefix(got, "ancient") {
		t.Fatalf("expected prepended history to remain intact, got %q", got)
	}
}

func sidebarCaptureFillLine(ch rune, width int) string {
	return strings.Repeat(string(ch), width)
}

func sidebarCaptureRowText(row []vterm.Cell, width int) string {
	var b strings.Builder
	for i := 0; i < width && i < len(row); i++ {
		r := row[i].Rune
		if r == 0 {
			r = ' '
		}
		b.WriteRune(r)
	}
	return b.String()
}
