package center

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/vterm"
)

// newPanicWriteEvent runs the real enqueue accounting for chunk and returns
// the actor event dispatchFlushChunkViaActor would have sent.
func newPanicWriteEvent(tab *Tab, wsID string, chunk []byte) tabEvent {
	prevEpoch, _, _, filtered, noiseAfter := enqueueActorWrite(tab, chunk)
	return tabEvent{
		tab:            tab,
		workspaceID:    wsID,
		tabID:          tab.ID,
		kind:           tabEventWriteOutput,
		output:         chunk,
		filteredOutput: filtered,
		noiseAfter:     noiseAfter,
		writeEpoch:     prevEpoch,
	}
}

// TestTabActor_PanicReleasesTabMu proves a panic mid-apply cannot wedge the
// pane: withMu's deferred unlock frees tab.mu before the per-event recover
// runs, the write accounting settles instead of leaking a phantom pending
// count, and the actor keeps processing later events.
func TestTabActor_PanicReleasesTabMu(t *testing.T) {
	m := newTestModel()
	m.tabEvents = make(chan tabEvent, 8)
	m.msgSink = func(tea.Msg) {}

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{ID: "tab-panic", Workspace: ws, Terminal: vterm.New(80, 24), Running: true}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.workspace = ws

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- m.RunTabActor(ctx)
	}()
	testutil.Eventually(t, time.Second, 10*time.Millisecond, m.isTabActorReady, "actor never became ready")

	var panicked atomic.Bool
	tab.testApplyHook = func() {
		if panicked.CompareAndSwap(false, true) {
			panic("injected apply panic")
		}
	}

	if !m.sendTabEvent(newPanicWriteEvent(tab, wsID, []byte("boom"))) {
		t.Fatal("panicking write event was not accepted by the actor")
	}
	testutil.Eventually(t, 2*time.Second, 10*time.Millisecond, panicked.Load, "apply hook never ran")
	// Recovery is complete only once the mutex is acquirable AND the write
	// accounting settled (pending drained, the in-flight breadcrumb cleared).
	testutil.Eventually(t, 2*time.Second, 10*time.Millisecond, func() bool {
		if !tab.mu.TryLock() {
			return false
		}
		defer tab.mu.Unlock()
		return tab.actorWritesPending == 0 && tab.actorQueuedBytes == 0 && !tab.actorApplyInFlight
	}, "tab.mu stayed locked or write accounting never settled after the panic")

	// The actor survived its own panic: a later write still applies.
	if !m.sendTabEvent(newPanicWriteEvent(tab, wsID, []byte("after-panic"))) {
		t.Fatal("post-panic write event was not accepted")
	}
	testutil.Eventually(t, 2*time.Second, 10*time.Millisecond, func() bool {
		tab.mu.Lock()
		defer tab.mu.Unlock()
		return strings.Contains(tab.Terminal.Render(), "after-panic")
	}, "post-panic write never reached the terminal")

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunTabActor() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for actor to stop")
	}
}

// TestActorWritePanic_ReconcilesPending pins the two reconcile arms: a write
// event that dies before its apply owes its enqueue accounting back, while a
// write whose apply began already consumed it and must not be double-counted.
func TestActorWritePanic_ReconcilesPending(t *testing.T) {
	m := newTestModel()

	t.Run("apply never began decrements the queued accounting", func(t *testing.T) {
		tab := &Tab{ID: "t", Terminal: vterm.New(80, 24)}
		chunk := []byte("lost-chunk")
		ev := newPanicWriteEvent(tab, "ws", chunk)
		if tab.actorWritesPending != 1 || tab.actorQueuedBytes != len(chunk) {
			t.Fatalf("enqueue accounting = (%d, %d), want (1, %d)", tab.actorWritesPending, tab.actorQueuedBytes, len(chunk))
		}

		m.reconcileActorEventPanic(ev)

		if tab.actorWritesPending != 0 || tab.actorQueuedBytes != 0 {
			t.Fatalf("post-reconcile accounting = (%d, %d), want (0, 0)", tab.actorWritesPending, tab.actorQueuedBytes)
		}
	})

	t.Run("apply that began is not double-decremented", func(t *testing.T) {
		tab := &Tab{ID: "t", Terminal: vterm.New(80, 24)}
		chunk := []byte("partial")
		ev := newPanicWriteEvent(tab, "ws", chunk)
		// Apply consumed the accounting at its top, then died mid-write:
		// the breadcrumb is what tells reconcile the decrement already ran.
		tab.mu.Lock()
		tab.actorWritesPending--
		tab.actorQueuedBytes -= len(chunk)
		tab.actorApplyInFlight = true
		tab.mu.Unlock()

		m.reconcileActorEventPanic(ev)

		tab.mu.Lock()
		defer tab.mu.Unlock()
		if tab.actorApplyInFlight {
			t.Fatal("reconcile left the apply breadcrumb set")
		}
		if tab.actorWritesPending != 0 || tab.actorQueuedBytes != 0 {
			t.Fatalf("double-decrement: accounting = (%d, %d), want (0, 0)", tab.actorWritesPending, tab.actorQueuedBytes)
		}
	})

	t.Run("stale epoch leaves settlement to the bump", func(t *testing.T) {
		tab := &Tab{ID: "t", Terminal: vterm.New(80, 24)}
		ev := newPanicWriteEvent(tab, "ws", []byte("stale"))
		// An epoch bump (attach/detach/reset) settles queued accounting
		// itself; reconcile must not decrement again for a superseded write.
		tab.mu.Lock()
		tab.actorWriteEpoch++
		tab.mu.Unlock()

		m.reconcileActorEventPanic(ev)

		tab.mu.Lock()
		defer tab.mu.Unlock()
		if tab.actorWritesPending != 1 {
			t.Fatalf("stale write decremented pending to %d, want 1 (the bump owns settlement)", tab.actorWritesPending)
		}
	})

	t.Run("non-write events reconcile to nothing", func(t *testing.T) {
		tab := &Tab{ID: "t", Terminal: vterm.New(80, 24)}
		m.reconcileActorEventPanic(tabEvent{kind: tabEventScrollBy, tab: tab})
		m.reconcileActorEventPanic(tabEvent{kind: tabEventWriteOutput})
	})
}

// TestSelectionScrollTick_Rearm proves the edge-hold auto-scroll chain
// rearms after a panic: a live scroll state yields a tick carrying the tab's
// CURRENT gen/seq — accepted by HandleTick as the next in-sequence tick —
// while an inactive state correctly stays dead.
func TestSelectionScrollTick_Rearm(t *testing.T) {
	m, tab, wsID := newRearmFixture(t)

	if cmd := m.RearmForPanic(selectionScrollTick{WorkspaceID: wsID, TabID: tab.ID, Gen: 1, Seq: 1}); cmd != nil {
		t.Fatal("inactive selection scroll must not rearm")
	}

	tab.mu.Lock()
	tab.selectionScroll.ScrollDir = 1
	_, gen, seq := tab.selectionScroll.NeedsTick()
	// The dropped tick's seq was already consumed by the handler before the
	// panic — the rearm must emit the chain's next seq, not the spent one.
	if !tab.selectionScroll.HandleTick(gen, seq) {
		tab.mu.Unlock()
		t.Fatal("first tick rejected by the live chain")
	}
	wantSeq := tab.selectionScroll.TickSeq
	tab.mu.Unlock()

	cmd := m.RearmForPanic(selectionScrollTick{WorkspaceID: wsID, TabID: tab.ID, Gen: gen, Seq: seq})
	if cmd == nil {
		t.Fatal("expected a rearm tick for a live selection-scroll chain")
	}
	tick, ok := cmd().(selectionScrollTick)
	if !ok {
		t.Fatalf("rearm cmd produced %T, want selectionScrollTick", cmd())
	}
	if tick.WorkspaceID != wsID || tick.TabID != tab.ID {
		t.Fatalf("rearmed tick = %+v, want ws=%s tab=%s", tick, wsID, tab.ID)
	}
	if tick.Gen != gen || tick.Seq != wantSeq {
		t.Fatalf("rearmed tick gen/seq = (%d, %d), want (%d, %d)", tick.Gen, tick.Seq, gen, wantSeq)
	}

	tab.mu.Lock()
	defer tab.mu.Unlock()
	if !tab.selectionScroll.HandleTick(tick.Gen, tick.Seq) {
		t.Fatal("rearmed tick rejected by the live chain")
	}
}
