package sidebar

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/vterm"
)

// TestSidebarWriter_PanicReleasesTSMu proves a panic mid-apply cannot wedge
// the pane: the step's deferred unlock frees ts.mu before the per-request
// recover runs, the panicking (already-dequeued) request is dropped, and the
// writer keeps processing later requests in order.
func TestSidebarWriter_PanicReleasesTSMu(t *testing.T) {
	m := NewTerminalModel()
	results := make(chan SidebarTabWritten, 8)
	m.SetMsgSink(collectWritten(results))
	ts := &TerminalState{VTerm: vterm.New(60, 20), Running: true}
	m.tabs.ByWorkspace["ws"] = []*TerminalTab{{ID: "t", State: ts}}
	w := installTestWriter(t, m, ts, nil)

	var panicked atomic.Bool
	ts.mu.Lock()
	w.testApplyHook = func() {
		if panicked.CompareAndSwap(false, true) {
			panic("injected apply panic")
		}
	}
	ts.mu.Unlock()

	if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("boom"), workspaceID: "ws", tabID: "t"}) {
		t.Fatal("panicking request fell back")
	}
	testutil.Eventually(t, 2*time.Second, 10*time.Millisecond, panicked.Load, "apply hook never ran")
	// The recover leaves the worker looping: the mutex is acquirable again
	// (never wedged by the panic-held lock) and the queue drains.
	testutil.Eventually(t, 2*time.Second, 10*time.Millisecond, func() bool {
		if !ts.mu.TryLock() {
			return false
		}
		defer ts.mu.Unlock()
		return len(w.reqs) == 0
	}, "ts.mu stayed locked or the queue never drained after the panic")

	// The writer survived its own panic: a later request still applies.
	if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("after-panic"), workspaceID: "ws", tabID: "t"}) {
		t.Fatal("post-panic request fell back")
	}
	testutil.Eventually(t, 2*time.Second, 10*time.Millisecond, func() bool {
		return strings.Contains(renderSidebarScreen(ts), "after-panic")
	}, "post-panic request never reached the terminal")
	if strings.Contains(renderSidebarScreen(ts), "boom") {
		t.Fatal("the panicking request leaked into the terminal")
	}
}

// TestSidebarSelectionScrollTick_Rearm proves the edge-hold auto-scroll
// chain rearms after a panic: a live scroll state yields a tick carrying the
// tab's CURRENT gen/seq — accepted by HandleTick as the next in-sequence
// tick — while an inactive state correctly stays dead.
func TestSidebarSelectionScrollTick_Rearm(t *testing.T) {
	m, ts, wsID, tabID := newRearmFixture(t)

	if cmd := m.RearmForPanic(messages.SidebarSelectionScrollTick{WorkspaceID: wsID, TabID: string(tabID), Gen: 1, Seq: 1}); cmd != nil {
		t.Fatal("inactive selection scroll must not rearm")
	}

	ts.mu.Lock()
	ts.selectionScroll.ScrollDir = 1
	_, gen, seq := ts.selectionScroll.NeedsTick()
	// The dropped tick's seq was already consumed by the handler before the
	// panic — the rearm must emit the chain's next seq, not the spent one.
	if !ts.selectionScroll.HandleTick(gen, seq) {
		ts.mu.Unlock()
		t.Fatal("first tick rejected by the live chain")
	}
	wantSeq := ts.selectionScroll.TickSeq
	ts.mu.Unlock()

	cmd := m.RearmForPanic(messages.SidebarSelectionScrollTick{WorkspaceID: wsID, TabID: string(tabID), Gen: gen, Seq: seq})
	if cmd == nil {
		t.Fatal("expected a rearm tick for a live selection-scroll chain")
	}
	tick, ok := cmd().(messages.SidebarSelectionScrollTick)
	if !ok {
		t.Fatalf("rearm cmd produced %T, want SidebarSelectionScrollTick", cmd())
	}
	if tick.WorkspaceID != wsID || tick.TabID != string(tabID) {
		t.Fatalf("rearmed tick = %+v, want ws=%s tab=%s", tick, wsID, tabID)
	}
	if tick.Gen != gen || tick.Seq != wantSeq {
		t.Fatalf("rearmed tick gen/seq = (%d, %d), want (%d, %d)", tick.Gen, tick.Seq, gen, wantSeq)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !ts.selectionScroll.HandleTick(tick.Gen, tick.Seq) {
		t.Fatal("rearmed tick rejected by the live chain")
	}
}
