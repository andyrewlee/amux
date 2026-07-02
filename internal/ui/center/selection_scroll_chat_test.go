package center

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/vterm"
)

// Chat tabs (coding agents) render scrolled history as a scrollback-only view
// (model_scrolled_history.go) instead of the vterm's native window, and their
// agents constantly repaint via 2J clears that churn scrollback through
// capture/dedup while the viewport anchor holds position. These tests pin the
// drag-selection auto-scroll behavior on that display mode; plain-tab
// equivalents live in selection_scroll_test.go.

// setupChatScrollModel builds a center model with an active chat tab whose
// terminal has real scrollback and chat capture behavior enabled.
func setupChatScrollModel(t *testing.T, lines int) (*Model, *Tab, string) {
	t.Helper()
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m := New(cfg)
	wt := &data.Workspace{Name: "wt", Repo: "/tmp/repo", Root: "/tmp/repo"}
	m.SetWorkspace(wt)
	wtID := string(wt.ID())

	vt := vterm.New(80, 24)
	vt.AllowAltScreenScrollback = true
	vt.CaptureNormalScreenOnClear = true
	for i := 0; i < lines; i++ {
		vt.Write([]byte(fmt.Sprintf("line %d\r\n", i)))
	}

	tab := &Tab{
		ID:        TabID("tab-chat-1"),
		Assistant: "claude",
		Workspace: wt,
		Terminal:  vt,
	}
	m.tabs.ByWorkspace[wtID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wtID] = 0
	m.SetSize(100, 40)
	m.SetOffset(0)
	m.Focus()
	return m, tab, wtID
}

// chatViewStartLine returns the first scrollback line of the chat history
// view, or -1 when the tab is at the live view.
func chatViewStartLine(tab *Tab) int {
	tab.mu.Lock()
	defer tab.mu.Unlock()
	start, _, ok := scrolledChatHistoryVisibleRange(tab.Terminal, tab.Terminal.Height)
	if !ok {
		return -1
	}
	return start
}

func TestChatTab_DragUpAutoScroll_TicksKeepScrolling(t *testing.T) {
	m, tab, wtID := setupChatScrollModel(t, 100)

	var sinkMsgs []tea.Msg
	m.msgSink = func(msg tea.Msg) { sinkMsgs = append(sinkMsgs, msg) }

	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionStart, termX: 10, termY: 10, inBounds: true,
	})
	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionUpdate, termX: 10, termY: -1,
	})

	tab.mu.Lock()
	off := tab.Terminal.ViewOffset
	gen := tab.selectionScroll.Gen
	seq := tab.selectionScroll.TickSeq
	tab.mu.Unlock()
	if off <= 0 {
		t.Fatalf("drag above viewport did not scroll a chat tab up (ViewOffset=%d)", off)
	}
	if len(sinkMsgs) == 0 {
		t.Fatal("no tick request emitted for chat tab")
	}

	for i := 0; i < 5; i++ {
		m.handleTabEvent(tabEvent{
			tab: tab, workspaceID: wtID, tabID: tab.ID,
			kind: tabEventSelectionScrollTick, gen: gen, seq: seq,
		})
		seq++
		tab.mu.Lock()
		next := tab.Terminal.ViewOffset
		active := tab.selectionScroll.Active
		tab.mu.Unlock()
		if !active {
			t.Fatalf("tick %d: chain died", i)
		}
		if next <= off {
			t.Fatalf("tick %d: offset did not advance (%d -> %d)", i, off, next)
		}
		off = next
	}
}

// TestChatTab_DragUpAutoScroll_AnchorStableWhileStreaming is the streaming
// invariant: while the agent repaints via 2J (scrollback grows through capture
// and shrinks through dedup, with the viewport anchor adjusting both ways),
// each auto-scroll tick must still move the visible history window up by
// exactly one line — no jumps, no stalls.
func TestChatTab_DragUpAutoScroll_AnchorStableWhileStreaming(t *testing.T) {
	m, tab, wtID := setupChatScrollModel(t, 100)
	m.msgSink = func(tea.Msg) {}

	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionStart, termX: 10, termY: 10, inBounds: true,
	})
	m.handleTabEvent(tabEvent{
		tab: tab, workspaceID: wtID, tabID: tab.ID,
		kind: tabEventSelectionUpdate, termX: 10, termY: -1,
	})

	tab.mu.Lock()
	gen := tab.selectionScroll.Gen
	seq := tab.selectionScroll.TickSeq
	tab.mu.Unlock()

	prevStart := chatViewStartLine(tab)
	if prevStart < 0 {
		t.Fatal("expected chat history view after drag up")
	}

	for i := 0; i < 5; i++ {
		// Agent repaint between ticks: home + clear + fresh frame. The 2J
		// capture appends the visible frame to scrollback; the next repaint's
		// dedup collapses it again.
		tab.mu.Lock()
		tab.Terminal.Write([]byte(fmt.Sprintf("\x1b[H\x1b[2Jframe %d\r\noutput %d\r\n", i, i)))
		tab.mu.Unlock()

		m.handleTabEvent(tabEvent{
			tab: tab, workspaceID: wtID, tabID: tab.ID,
			kind: tabEventSelectionScrollTick, gen: gen, seq: seq,
		})
		seq++

		tab.mu.Lock()
		active := tab.selectionScroll.Active
		tab.mu.Unlock()
		if !active {
			t.Fatalf("tick %d: chain died during streaming", i)
		}

		start := chatViewStartLine(tab)
		if start != prevStart-1 {
			t.Fatalf("tick %d: view window moved %d -> %d, want exactly one line up (%d)",
				i, prevStart, start, prevStart-1)
		}
		prevStart = start
	}
}

// TestChatTab_DragUp_FullUpdateChain drives the real Update entry points
// (click, motion) with the actor not ready, verifying the synchronous fallback
// runs the same handlers and requests the tick via msgSink.
func TestChatTab_DragUp_FullUpdateChain(t *testing.T) {
	m, tab, _ := setupChatScrollModel(t, 100)
	var sinkMsgs []tea.Msg
	m.msgSink = func(msg tea.Msg) { sinkMsgs = append(sinkMsgs, msg) }

	tm := m.terminalMetrics()
	clickX := tm.ContentStartX + 5
	clickY := tm.ContentStartY + 5
	m, _ = m.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})

	tab.mu.Lock()
	active := tab.Selection.Active
	tab.mu.Unlock()
	if !active {
		t.Fatalf("selection not active after click at (%d,%d)", clickX, clickY)
	}

	// Drag above the content area (screen Y = 0 → negative termY).
	m, _ = m.Update(tea.MouseMotionMsg{X: clickX, Y: 0, Button: tea.MouseLeft})

	tab.mu.Lock()
	off := tab.Terminal.ViewOffset
	dir := tab.selectionScroll.ScrollDir
	tab.mu.Unlock()
	if dir != 1 {
		t.Fatalf("scroll direction = %d, want 1 (up)", dir)
	}
	if off <= 0 {
		t.Fatalf("no scroll on drag above viewport (ViewOffset=%d)", off)
	}
	tickRequested := false
	for _, sm := range sinkMsgs {
		if _, ok := sm.(selectionTickRequest); ok {
			tickRequested = true
		}
	}
	if !tickRequested {
		t.Fatal("no tick request posted via msgSink")
	}

	// Release finishes the selection and stops the chain.
	_, _ = m.Update(tea.MouseReleaseMsg{X: clickX, Y: 0, Button: tea.MouseLeft})
	tab.mu.Lock()
	stillActive := tab.Selection.Active
	tab.mu.Unlock()
	if stillActive {
		t.Fatal("selection still active after release")
	}
}
