package center

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

// newRearmFixture builds a model with one live tab whose flush latch is
// already set — the state a panicked updatePTYFlush leaves behind.
func newRearmFixture(t *testing.T) (*Model, *Tab, string) {
	t.Helper()
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-rearm"),
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
		State: ptyio.State{
			LastOutputAt:      time.Now().Add(-time.Second),
			FlushPendingSince: time.Now().Add(-time.Second),
			PendingOutput:     []byte("abc"),
			FlushScheduled:    true,
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.workspace = ws
	return m, tab, wsID
}

// TestRearmForPanic_PTYFlushRearmsAndClearsLatch covers the head finding: a
// panic inside updatePTYFlush dropped the tail arm AND left FlushScheduled
// latched, which froze the pane permanently. The rearm must emit one flush
// tick; delivering it runs the gate and clears the latch.
func TestRearmForPanic_PTYFlushRearmsAndClearsLatch(t *testing.T) {
	m, tab, wsID := newRearmFixture(t)

	cmd := m.RearmForPanic(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})
	if cmd == nil {
		t.Fatal("expected a rearm tick for a live tab's flush chain")
	}
	flushMsg, ok := cmd().(PTYFlush)
	if !ok {
		t.Fatalf("rearm cmd produced %T, want PTYFlush", cmd())
	}
	if flushMsg.WorkspaceID != wsID || flushMsg.TabID != tab.ID {
		t.Fatalf("rearmed flush = %+v, want ws=%s tab=%s", flushMsg, wsID, tab.ID)
	}

	_ = m.updatePTYFlush(flushMsg)
	if tab.FlushScheduled {
		t.Fatal("FlushScheduled must clear once the rearmed flush runs")
	}
	if len(tab.PendingOutput) != 0 {
		t.Fatalf("pending output = %d, want 0 after rearmed flush", len(tab.PendingOutput))
	}
}

// TestRearmForPanic_PTYFlushDeadTabStaysDead proves a rearm for a tab that no
// longer exists emits nothing — a dead chain correctly stays dead.
func TestRearmForPanic_PTYFlushDeadTabStaysDead(t *testing.T) {
	m, _, wsID := newRearmFixture(t)
	if cmd := m.RearmForPanic(PTYFlush{WorkspaceID: wsID, TabID: TabID("gone")}); cmd != nil {
		t.Fatal("expected nil rearm for a missing tab")
	}
}

// TestRearmForPanic_PTYFlushStaleWorkspaceStampsResolvedID mirrors the
// handler's own rebind semantics: a tick stamped with the pre-rebind
// workspace ID resolves by tab scan and re-arms under the tab's actual ID.
func TestRearmForPanic_PTYFlushStaleWorkspaceStampsResolvedID(t *testing.T) {
	m, tab, wsID := newRearmFixture(t)
	cmd := m.RearmForPanic(PTYFlush{WorkspaceID: "pre-rebind-ws", TabID: tab.ID})
	if cmd == nil {
		t.Fatal("expected a rearm tick for a resolvable tab stamped with a stale wsID")
	}
	flushMsg, ok := cmd().(PTYFlush)
	if !ok {
		t.Fatalf("rearm cmd produced %T, want PTYFlush", cmd())
	}
	if flushMsg.WorkspaceID != wsID {
		t.Fatalf("rearmed flush ws = %q, want resolved %q", flushMsg.WorkspaceID, wsID)
	}
}

// TestRearmForPanic_PTYStoppedArmsRestartTick proves the stopped handler's
// dropped tail arm is recovered as the chain's own restart tick, carrying the
// original gen fencing.
func TestRearmForPanic_PTYStoppedArmsRestartTick(t *testing.T) {
	m, tab, wsID := newRearmFixture(t)

	for _, msg := range []tea.Msg{PTYStopped{WorkspaceID: wsID, TabID: tab.ID, Gen: 7}, PTYRestart{WorkspaceID: wsID, TabID: tab.ID, Gen: 7}} {
		cmd := m.RearmForPanic(msg)
		if cmd == nil {
			t.Fatalf("expected a rearm tick for %T", msg)
		}
		restartMsg, ok := cmd().(PTYRestart)
		if !ok {
			t.Fatalf("rearm cmd produced %T, want PTYRestart", cmd())
		}
		if restartMsg.WorkspaceID != wsID || restartMsg.TabID != tab.ID || restartMsg.Gen != 7 {
			t.Fatalf("rearmed restart = %+v, want ws=%s tab=%s gen=7", restartMsg, wsID, tab.ID)
		}
	}
}

// TestRearmForPanic_StaleGenRestartSelfDrops proves the armed restart tick
// self-drops when a newer reader generation exists — no restart side effects
// for a superseded arm.
func TestRearmForPanic_StaleGenRestartSelfDrops(t *testing.T) {
	m, tab, wsID := newRearmFixture(t)

	restartMsg := PTYRestart{WorkspaceID: wsID, TabID: tab.ID, Gen: 7}
	tab.mu.Lock()
	tab.State.ReaderGen = 8 // a newer reader already runs
	tab.mu.Unlock()

	if cmd := m.updatePTYRestart(restartMsg); cmd != nil {
		t.Fatal("superseded-gen restart must self-drop with no side effects")
	}
}

// TestRearmForPanic_NonChainMessagesReturnNil pins the boundary: only the
// three periodic chain types rearm — everything else maps to nil so
// non-periodic panic behavior is unchanged.
func TestRearmForPanic_NonChainMessagesReturnNil(t *testing.T) {
	m, tab, wsID := newRearmFixture(t)
	for _, msg := range []tea.Msg{
		PTYOutput{WorkspaceID: wsID, TabID: tab.ID},
		PTYCursorRefresh{WorkspaceID: wsID, TabID: tab.ID},
		TabInputFailed{WorkspaceID: wsID, TabID: tab.ID},
		nil,
	} {
		if cmd := m.RearmForPanic(msg); cmd != nil {
			t.Errorf("%T: expected nil rearm, got non-nil", msg)
		}
	}
}

// TestRearmForPanic_SelectionScrollTickRearms covers the head finding: the
// edge-hold auto-scroll chain is self-rearming — a panic-dropped tick ends
// the scroll for the rest of the drag. A live chain (Active + ScrollDir)
// must rearm with the tab's CURRENT gen/seq.
func TestRearmForPanic_SelectionScrollTickRearms(t *testing.T) {
	m, tab, wsID := newRearmFixture(t)
	tab.selectionScroll.Active = true
	tab.selectionScroll.ScrollDir = 1
	tab.selectionScroll.Gen = 4
	tab.selectionScroll.TickSeq = 7

	cmd := m.RearmForPanic(selectionScrollTick{WorkspaceID: wsID, TabID: tab.ID})
	if cmd == nil {
		t.Fatal("expected a rearm tick for a live selection-scroll chain")
	}
	tick, ok := cmd().(selectionScrollTick)
	if !ok {
		t.Fatalf("rearm cmd produced %T, want selectionScrollTick", cmd())
	}
	if tick.Gen != 4 || tick.Seq != 7 {
		t.Fatalf("rearmed tick = gen %d seq %d, want 4/7", tick.Gen, tick.Seq)
	}
}

// TestRearmForPanic_SelectionScrollTickInactiveStaysNil: a dropped tick on
// an already-stopped chain must not resurrect scrolling.
func TestRearmForPanic_SelectionScrollTickInactiveStaysNil(t *testing.T) {
	m, tab, wsID := newRearmFixture(t)
	tab.selectionScroll.Active = false
	tab.selectionScroll.ScrollDir = 0

	if cmd := m.RearmForPanic(selectionScrollTick{WorkspaceID: wsID, TabID: tab.ID}); cmd != nil {
		t.Fatal("inactive selection-scroll chain must not rearm")
	}
}

// TestRearmActorChainAfterPanic_SelectionTickReemits pins the actor-side
// gap: handleTabEvent runs under the actor's per-event recover, and a
// panicked tick there never reached rearmCmdForPanic — the recover must
// re-emit a selectionTickRequest carrying the tab's current gen/seq.
func TestRearmActorChainAfterPanic_SelectionTickReemits(t *testing.T) {
	m, tab, wsID := newRearmFixture(t)
	tab.selectionScroll.Active = true
	tab.selectionScroll.ScrollDir = -1
	tab.selectionScroll.Gen = 9
	tab.selectionScroll.TickSeq = 3
	var got []tea.Msg
	m.msgSink = func(msg tea.Msg) { got = append(got, msg) }

	m.rearmActorChainAfterPanic(tabEvent{
		kind:        tabEventSelectionScrollTick,
		tab:         tab,
		workspaceID: wsID,
		tabID:       tab.ID,
	})
	if len(got) != 1 {
		t.Fatalf("expected one re-emitted tick request, got %d", len(got))
	}
	req, ok := got[0].(selectionTickRequest)
	if !ok {
		t.Fatalf("re-emitted %T, want selectionTickRequest", got[0])
	}
	if req.gen != 9 || req.seq != 3 || req.workspaceID != wsID || req.tabID != tab.ID {
		t.Fatalf("re-emitted request = %+v, want gen9 seq3 ws=%s tab=%s", req, wsID, tab.ID)
	}
}

// TestRearmActorChainAfterPanic_InactiveAndOtherKindsNoOp: dead chains and
// non-tick events (write output, scrolls) must not emit anything — the
// write path's accounting stays reconcileActorEventPanic's alone.
func TestRearmActorChainAfterPanic_InactiveAndOtherKindsNoOp(t *testing.T) {
	m, tab, wsID := newRearmFixture(t)
	var got []tea.Msg
	m.msgSink = func(msg tea.Msg) { got = append(got, msg) }

	// Inactive chain — nothing to rearm.
	m.rearmActorChainAfterPanic(tabEvent{kind: tabEventSelectionScrollTick, tab: tab, workspaceID: wsID, tabID: tab.ID})
	// A different kind on a live chain — not this function's job.
	tab.selectionScroll.Active = true
	tab.selectionScroll.ScrollDir = 1
	m.rearmActorChainAfterPanic(tabEvent{kind: tabEventWriteOutput, tab: tab, workspaceID: wsID, tabID: tab.ID})
	if len(got) != 0 {
		t.Fatalf("expected no re-emission, got %v", got)
	}
}
