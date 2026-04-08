package center

import (
	"strings"
	"testing"

	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestUpdatePtyTabReattachResult_UsesSnapshotSizeBeforeResizingToCurrentSize(t *testing.T) {
	m := newTestModel()
	m.width = 20
	m.height = 9
	current := m.terminalMetrics()
	snapshotWidth := current.Width + 8
	snapshotHeight := current.Height

	rows := make([]string, snapshotHeight)
	for i := range rows {
		rows[i] = captureFillLine(rune('A'+i), snapshotWidth)
	}
	capture := []byte("history\n" + strings.Join(rows, "\n") + "\n")

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-reattach-snapshot-size"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(snapshotWidth, snapshotHeight),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID:       wsID,
		TabID:             tab.ID,
		Agent:             &appPty.Agent{Session: "sess-reattach-snapshot-size"},
		Rows:              snapshotHeight,
		Cols:              snapshotWidth,
		ScrollbackCapture: capture,
		CaptureFullPane:   true,
		SnapshotCols:      snapshotWidth,
		SnapshotRows:      snapshotHeight,
	})

	if got := tab.Terminal.Width; got != current.Width {
		t.Fatalf("expected terminal width %d after live resize, got %d", current.Width, got)
	}
	if got := captureRowText(tab.Terminal.Screen[0], snapshotWidth); got != rows[0] {
		t.Fatalf("expected restore to parse using snapshot width before shrinking, got %q", got)
	}
	if got := captureRowText(tab.Terminal.Screen[snapshotHeight-1], snapshotWidth); got != rows[snapshotHeight-1] {
		t.Fatalf("expected last visible row to preserve snapshot-width content, got %q", got)
	}
}

func TestHandlePtyTabCreated_UsesSnapshotSizeBeforeResizingToCurrentSize(t *testing.T) {
	m := newTestModel()
	m.width = 20
	m.height = 9
	current := m.terminalMetrics()
	snapshotWidth := current.Width + 8
	snapshotHeight := current.Height

	rows := make([]string, snapshotHeight)
	for i := range rows {
		rows[i] = captureFillLine(rune('K'+i), snapshotWidth)
	}
	capture := []byte("history\n" + strings.Join(rows, "\n") + "\n")

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace:         ws,
		Assistant:         "codex",
		Agent:             &appPty.Agent{Session: "sess-created-snapshot-size"},
		TabID:             TabID("tab-created-snapshot-size"),
		Rows:              snapshotHeight,
		Cols:              snapshotWidth,
		Activate:          true,
		ScrollbackCapture: capture,
		CaptureFullPane:   true,
		SnapshotCols:      snapshotWidth,
		SnapshotRows:      snapshotHeight,
	})

	tab := m.tabsByWorkspace[wsID][0]
	if got := tab.Terminal.Width; got != current.Width {
		t.Fatalf("expected terminal width %d after live resize, got %d", current.Width, got)
	}
	if got := captureRowText(tab.Terminal.Screen[0], snapshotWidth); got != rows[0] {
		t.Fatalf("expected restore to parse using snapshot width before shrinking, got %q", got)
	}
	if got := captureRowText(tab.Terminal.Screen[snapshotHeight-1], snapshotWidth); got != rows[snapshotHeight-1] {
		t.Fatalf("expected last visible row to preserve snapshot-width content, got %q", got)
	}
}

func TestUpdatePtyTabReattachResult_UsesCaptureSizeBeforeResizingForHistoryOnly(t *testing.T) {
	m := newTestModel()
	m.width = 20
	m.height = 9
	current := m.terminalMetrics()
	captureWidth := current.Width + 8
	captureHeight := 2
	capture := []byte(captureFillLine('A', captureWidth) + "\n" + captureFillLine('B', captureWidth) + "\n")

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-reattach-history-size"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(current.Width, current.Height),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID:       wsID,
		TabID:             tab.ID,
		Agent:             &appPty.Agent{Session: "sess-reattach-history-size"},
		Rows:              captureHeight,
		Cols:              captureWidth,
		ScrollbackCapture: capture,
		CaptureFullPane:   false,
	})

	if got := tab.Terminal.Width; got != current.Width {
		t.Fatalf("expected terminal width %d after live resize, got %d", current.Width, got)
	}
	if len(tab.Terminal.Scrollback) != 2 {
		t.Fatalf("expected 2 scrollback lines from history-only restore, got %d", len(tab.Terminal.Scrollback))
	}
	if got := captureRowText(tab.Terminal.Scrollback[0], captureWidth); got != captureFillLine('A', captureWidth) {
		t.Fatalf("expected first history row to preserve capture width before resize, got %q", got)
	}
	if got := captureRowText(tab.Terminal.Scrollback[1], captureWidth); got != captureFillLine('B', captureWidth) {
		t.Fatalf("expected second history row to preserve capture width before resize, got %q", got)
	}
}

func TestHandlePtyTabCreated_UsesCaptureSizeBeforeResizingForHistoryOnly(t *testing.T) {
	m := newTestModel()
	m.width = 20
	m.height = 9
	current := m.terminalMetrics()
	captureWidth := current.Width + 8
	captureHeight := 2
	capture := []byte(captureFillLine('M', captureWidth) + "\n" + captureFillLine('N', captureWidth) + "\n")

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace:         ws,
		Assistant:         "codex",
		Agent:             &appPty.Agent{Session: "sess-created-history-size"},
		TabID:             TabID("tab-created-history-size"),
		Rows:              captureHeight,
		Cols:              captureWidth,
		Activate:          true,
		ScrollbackCapture: capture,
		CaptureFullPane:   false,
	})

	tab := m.tabsByWorkspace[wsID][0]
	if got := tab.Terminal.Width; got != current.Width {
		t.Fatalf("expected terminal width %d after live resize, got %d", current.Width, got)
	}
	if len(tab.Terminal.Scrollback) != 2 {
		t.Fatalf("expected 2 scrollback lines from history-only create, got %d", len(tab.Terminal.Scrollback))
	}
	if got := captureRowText(tab.Terminal.Scrollback[0], captureWidth); got != captureFillLine('M', captureWidth) {
		t.Fatalf("expected first history row to preserve capture width before resize, got %q", got)
	}
	if got := captureRowText(tab.Terminal.Scrollback[1], captureWidth); got != captureFillLine('N', captureWidth) {
		t.Fatalf("expected second history row to preserve capture width before resize, got %q", got)
	}
}

func TestUpdatePtyTabReattachResult_UsesLiveDefaultPTYSizeBeforeFirstLayout(t *testing.T) {
	m := newTestModel()
	current := m.terminalMetrics()
	captureWidth := current.Width + 9
	captureHeight := current.Height + 5
	capture := []byte(captureFillLine('Q', captureWidth) + "\n")

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-reattach-startup-live-size"),
		Assistant: "codex",
		Workspace: ws,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID:       wsID,
		TabID:             tab.ID,
		Agent:             &appPty.Agent{Session: "sess-reattach-startup-live-size", Terminal: &appPty.Terminal{}},
		Rows:              captureHeight,
		Cols:              captureWidth,
		ScrollbackCapture: capture,
		CaptureFullPane:   false,
	})

	if tab.ptyRows != current.Height || tab.ptyCols != current.Width {
		t.Fatalf("expected live PTY size %dx%d before first layout, got %dx%d", current.Width, current.Height, tab.ptyCols, tab.ptyRows)
	}
	if tab.ptyRows == captureHeight || tab.ptyCols == captureWidth {
		t.Fatalf("expected PTY resize to avoid stale capture size %dx%d, got %dx%d", captureWidth, captureHeight, tab.ptyCols, tab.ptyRows)
	}
}

func TestHandlePtyTabCreated_UsesLiveDefaultPTYSizeBeforeFirstLayout(t *testing.T) {
	m := newTestModel()
	current := m.terminalMetrics()
	captureWidth := current.Width + 9
	captureHeight := current.Height + 5
	capture := []byte(captureFillLine('R', captureWidth) + "\n")

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace:         ws,
		Assistant:         "codex",
		Agent:             &appPty.Agent{Session: "sess-created-startup-live-size", Terminal: &appPty.Terminal{}},
		TabID:             TabID("tab-created-startup-live-size"),
		Rows:              captureHeight,
		Cols:              captureWidth,
		Activate:          true,
		ScrollbackCapture: capture,
		CaptureFullPane:   false,
	})

	tab := m.tabsByWorkspace[wsID][0]
	if tab.ptyRows != current.Height || tab.ptyCols != current.Width {
		t.Fatalf("expected live PTY size %dx%d before first layout, got %dx%d", current.Width, current.Height, tab.ptyCols, tab.ptyRows)
	}
	if tab.ptyRows == captureHeight || tab.ptyCols == captureWidth {
		t.Fatalf("expected PTY resize to avoid stale capture size %dx%d, got %dx%d", captureWidth, captureHeight, tab.ptyCols, tab.ptyRows)
	}
}

func TestRestoreTerminalScrollbackCapture_ScrollsToBottomBeforePrependingHistory(t *testing.T) {
	term := vterm.New(20, 2)
	term.LoadPaneCapture([]byte("oldest\nolder\nscreen one\nscreen two\n"))
	term.ScrollViewToTop()
	if term.ViewOffset == 0 {
		t.Fatal("expected seeded terminal to start scrolled into history")
	}

	common.RestoreScrollbackCapture(term, []byte("ancient\n"), 20, 1, 20, 2)

	if term.ViewOffset != 0 {
		t.Fatalf("expected history-only restore to land at live bottom, got view offset %d", term.ViewOffset)
	}
	if got := captureRowText(term.Scrollback[0], 20); got[:7] != "ancient" {
		t.Fatalf("expected prepended history to remain intact, got %q", got)
	}
}
