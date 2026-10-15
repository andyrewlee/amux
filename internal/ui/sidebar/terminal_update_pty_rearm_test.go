package sidebar

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// newRearmFixture builds a terminal model with one live tab whose flush
// latch is already set — the state a panicked handlePTYFlush leaves behind.
func newRearmFixture(t *testing.T) (*TerminalModel, *TerminalState, string, TerminalTabID) {
	t.Helper()
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	ts := &TerminalState{
		Running: true,
	}
	ts.State.LastOutputAt = time.Now().Add(-time.Second)
	ts.State.FlushPendingSince = time.Now().Add(-time.Second)
	ts.State.PendingOutput = []byte("abc")
	ts.State.FlushScheduled = true
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: ts}}
	m.tabs.ActiveByWorkspace[wsID] = 0
	return m, ts, wsID, tabID
}

// TestRearmForPanic_SidebarFlushRearmsAndClearsLatch covers the head
// finding: a panic inside handlePTYFlush dropped the tail arm AND left
// FlushScheduled latched, which froze the sidebar pane permanently. The
// rearm must emit one flush tick; delivering it runs the gate and clears
// the latch.
func TestRearmForPanic_SidebarFlushRearmsAndClearsLatch(t *testing.T) {
	m, ts, wsID, tabID := newRearmFixture(t)

	cmd := m.RearmForPanic(messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: string(tabID)})
	if cmd == nil {
		t.Fatal("expected a rearm tick for a live tab's flush chain")
	}
	flushMsg, ok := cmd().(messages.SidebarPTYFlush)
	if !ok {
		t.Fatalf("rearm cmd produced %T, want messages.SidebarPTYFlush", cmd())
	}
	if flushMsg.WorkspaceID != wsID || flushMsg.TabID != string(tabID) {
		t.Fatalf("rearmed flush = %+v, want ws=%s tab=%s", flushMsg, wsID, tabID)
	}

	_ = m.handlePTYFlush(flushMsg)
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.FlushScheduled {
		t.Fatal("FlushScheduled must clear once the rearmed flush runs")
	}
}

// TestRearmForPanic_SidebarFlushDeadTabStaysDead proves a rearm for a tab
// that no longer exists emits nothing — a dead chain correctly stays dead.
func TestRearmForPanic_SidebarFlushDeadTabStaysDead(t *testing.T) {
	m, _, wsID, _ := newRearmFixture(t)
	if cmd := m.RearmForPanic(messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: "gone"}); cmd != nil {
		t.Fatal("expected nil rearm for a missing tab")
	}
}

// TestRearmForPanic_SidebarStoppedArmsRestartTick proves the stopped
// handler's dropped tail arm is recovered as the chain's own restart tick,
// carrying the original gen fencing.
func TestRearmForPanic_SidebarStoppedArmsRestartTick(t *testing.T) {
	m, _, wsID, tabID := newRearmFixture(t)

	for _, msg := range []tea.Msg{
		messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID), Gen: 7},
		messages.SidebarPTYRestart{WorkspaceID: wsID, TabID: string(tabID), Gen: 7},
	} {
		cmd := m.RearmForPanic(msg)
		if cmd == nil {
			t.Fatalf("expected a rearm tick for %T", msg)
		}
		restartMsg, ok := cmd().(messages.SidebarPTYRestart)
		if !ok {
			t.Fatalf("rearm cmd produced %T, want messages.SidebarPTYRestart", cmd())
		}
		if restartMsg.WorkspaceID != wsID || restartMsg.TabID != string(tabID) || restartMsg.Gen != 7 {
			t.Fatalf("rearmed restart = %+v, want ws=%s tab=%s gen=7", restartMsg, wsID, tabID)
		}
	}
}

// TestRearmForPanic_SidebarStaleGenRestartSelfDrops proves the armed restart
// tick self-drops when a newer reader generation exists — no restart side
// effects for a superseded arm.
func TestRearmForPanic_SidebarStaleGenRestartSelfDrops(t *testing.T) {
	m, ts, wsID, tabID := newRearmFixture(t)

	restartMsg := messages.SidebarPTYRestart{WorkspaceID: wsID, TabID: string(tabID), Gen: 7}
	ts.mu.Lock()
	ts.State.ReaderGen = 8 // a newer reader already runs
	ts.mu.Unlock()

	if cmd := m.handlePTYRestart(restartMsg); cmd != nil {
		t.Fatal("superseded-gen restart must self-drop with no side effects")
	}
}

// TestRearmForPanic_SidebarSelectionScrollTickRearms: the edge-hold
// auto-scroll chain is self-rearming — a panic-dropped tick ends the
// scroll for the rest of the drag, so a live chain must rearm with the
// tab's current gen/seq.
func TestRearmForPanic_SidebarSelectionScrollTickRearms(t *testing.T) {
	m, ts, wsID, tabID := newRearmFixture(t)
	ts.mu.Lock()
	ts.selectionScroll.Active = true
	ts.selectionScroll.ScrollDir = 1
	ts.selectionScroll.Gen = 4
	ts.selectionScroll.TickSeq = 7
	ts.mu.Unlock()

	cmd := m.RearmForPanic(messages.SidebarSelectionScrollTick{WorkspaceID: wsID, TabID: string(tabID)})
	if cmd == nil {
		t.Fatal("expected a rearm tick for a live selection-scroll chain")
	}
	tick, ok := cmd().(messages.SidebarSelectionScrollTick)
	if !ok {
		t.Fatalf("rearm cmd produced %T, want messages.SidebarSelectionScrollTick", cmd())
	}
	if tick.Gen != 4 || tick.Seq != 7 {
		t.Fatalf("rearmed tick = gen %d seq %d, want 4/7", tick.Gen, tick.Seq)
	}
}

// TestRearmForPanic_SidebarSelectionScrollTickInactiveStaysNil: a dropped
// tick on an already-stopped chain must not resurrect scrolling.
func TestRearmForPanic_SidebarSelectionScrollTickInactiveStaysNil(t *testing.T) {
	m, _, wsID, tabID := newRearmFixture(t)
	if cmd := m.RearmForPanic(messages.SidebarSelectionScrollTick{WorkspaceID: wsID, TabID: string(tabID)}); cmd != nil {
		t.Fatal("inactive selection-scroll chain must not rearm")
	}
}

// TestRearmForPanic_SidebarNonChainMessagesReturnNil pins the boundary:
// only the periodic chain types rearm — everything else maps to nil.
func TestRearmForPanic_SidebarNonChainMessagesReturnNil(t *testing.T) {
	m, _, wsID, tabID := newRearmFixture(t)
	for _, msg := range []tea.Msg{
		messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: string(tabID)},
		messages.SidebarSelectionScrollTick{},
		nil,
	} {
		if cmd := m.RearmForPanic(msg); cmd != nil {
			t.Errorf("%T: expected nil rearm, got non-nil", msg)
		}
	}
}
