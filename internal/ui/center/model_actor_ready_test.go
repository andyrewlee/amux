package center

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestIsTabActorReady_FalseWhenHeartbeatStale(t *testing.T) {
	m := newTestModel()
	m.setTabActorReady()
	atomic.StoreInt64(&m.tabActorHeartbeat, time.Now().Add(-(tabActorStallTimeout + time.Second)).UnixNano())

	if m.isTabActorReady() {
		t.Fatal("expected stale actor heartbeat to clear readiness")
	}
	if atomic.LoadUint32(&m.tabActorReady) != 0 {
		t.Fatal("expected stale readiness flag to be cleared")
	}
}

func TestUpdatePTYFlush_StaleActorHeartbeatForcesParserResetFallback(t *testing.T) {
	m := newTestModel()
	m.setTabActorReady()
	atomic.StoreInt64(&m.tabActorHeartbeat, time.Now().Add(-(tabActorStallTimeout + time.Second)).UnixNano())

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                 TabID("tab-1"),
		Assistant:          "codex",
		Workspace:          ws,
		Terminal:           vterm.New(80, 24),
		Running:            true,
		pendingOutput:      []byte("visible"),
		lastOutputAt:       time.Now().Add(-time.Second),
		flushPendingSince:  time.Now().Add(-time.Second),
		parserResetPending: true,
		actorWritesPending: 1,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})

	if tab.parserResetPending {
		t.Fatal("expected stale actor flush to clear parserResetPending")
	}
	if tab.actorWritesPending != 0 {
		t.Fatalf("expected stale actor flush to clear actorWritesPending, got %d", tab.actorWritesPending)
	}
	if len(tab.pendingOutput) == 0 {
		t.Fatal("expected pending output to remain queued for the follow-up flush")
	}
	if !tab.flushScheduled {
		t.Fatal("expected follow-up flush to be scheduled after stale actor fallback")
	}
}

func TestRunTabActor_SetsReadyWithoutEmittingLivenessMsgs(t *testing.T) {
	m := newTestModel()
	m.tabEvents = make(chan tabEvent, 1)
	sinkMsgs := make(chan tea.Msg, 4)
	m.msgSink = func(msg tea.Msg) {
		sinkMsgs <- msg
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- m.RunTabActor(ctx)
	}()

	deadline := time.Now().Add(time.Second)
	for !m.isTabActorReady() {
		if time.Now().After(deadline) {
			t.Fatal("expected actor startup to set readiness directly")
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case msg := <-sinkMsgs:
		t.Fatalf("unexpected liveness message on startup: %T", msg)
	case <-time.After(100 * time.Millisecond):
	}

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

func TestNoteTabActorHeartbeat_RefreshesReadiness(t *testing.T) {
	m := newTestModel()
	before := time.Now()

	m.noteTabActorHeartbeat()

	if !m.isTabActorReady() {
		t.Fatal("expected direct heartbeat to refresh actor readiness")
	}
	if got := atomic.LoadUint32(&m.tabActorReady); got != 1 {
		t.Fatalf("expected ready flag to be set, got %d", got)
	}
	got := atomic.LoadInt64(&m.tabActorHeartbeat)
	if got < before.UnixNano() {
		t.Fatalf("expected heartbeat timestamp to be refreshed on receipt, got %d before %d", got, before.UnixNano())
	}
}

func TestSetTabActorReady_SeedsHeartbeatBeforeReadiness(t *testing.T) {
	m := newTestModel()

	m.setTabActorReady()

	if got := atomic.LoadUint32(&m.tabActorReady); got != 1 {
		t.Fatalf("expected ready flag to be set, got %d", got)
	}
	if got := atomic.LoadInt64(&m.tabActorHeartbeat); got == 0 {
		t.Fatal("expected setTabActorReady to seed heartbeat timestamp")
	}
	if !m.isTabActorReady() {
		t.Fatal("expected actor to be ready immediately after setTabActorReady")
	}
}

func TestRunTabActor_EmitsRedrawForActorHandledUIEvent(t *testing.T) {
	m := newTestModel()
	m.tabEvents = make(chan tabEvent, 1)
	sinkMsgs := make(chan tea.Msg, 4)
	m.msgSink = func(msg tea.Msg) {
		sinkMsgs <- msg
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- m.RunTabActor(ctx)
	}()

	deadline := time.Now().Add(time.Second)
	for !m.isTabActorReady() {
		if time.Now().After(deadline) {
			t.Fatal("expected actor startup to set readiness directly")
		}
		time.Sleep(10 * time.Millisecond)
	}

	tab := &Tab{Terminal: vterm.New(80, 24)}
	m.tabEvents <- tabEvent{kind: tabEventSelectionStart, tab: tab}
	select {
	case msg := <-sinkMsgs:
		if _, ok := msg.(tabActorRedraw); !ok {
			t.Fatalf("expected redraw message, got %T", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected actor-handled UI event to emit redraw message")
	}

	select {
	case msg := <-sinkMsgs:
		t.Fatalf("unexpected extra actor message %T", msg)
	case <-time.After(50 * time.Millisecond):
	}

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

func TestRequestTabActorRedraw_CoalescesOutstandingSignals(t *testing.T) {
	m := newTestModel()
	sinkMsgs := make(chan tea.Msg, 4)
	m.msgSinkTry = func(msg tea.Msg) bool {
		sinkMsgs <- msg
		return true
	}
	m.msgSink = func(msg tea.Msg) {
		_ = m.msgSinkTry(msg)
	}

	m.requestTabActorRedraw()
	m.requestTabActorRedraw()

	select {
	case msg := <-sinkMsgs:
		if _, ok := msg.(tabActorRedraw); !ok {
			t.Fatalf("expected redraw message, got %T", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected coalesced redraw message")
	}

	select {
	case msg := <-sinkMsgs:
		t.Fatalf("unexpected extra redraw message %T", msg)
	case <-time.After(50 * time.Millisecond):
	}

	_, _ = m.Update(tabActorRedraw{})
	m.requestTabActorRedraw()

	select {
	case msg := <-sinkMsgs:
		if _, ok := msg.(tabActorRedraw); !ok {
			t.Fatalf("expected redraw message after clear, got %T", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected redraw message after clearing pending flag")
	}
}

func TestRequestTabActorRedraw_MsgSinkFallbackDoesNotLatchPending(t *testing.T) {
	m := newTestModel()
	sinkMsgs := make(chan tea.Msg, 4)
	m.msgSink = func(msg tea.Msg) {
		sinkMsgs <- msg
	}

	m.requestTabActorRedraw()
	m.requestTabActorRedraw()

	if got := atomic.LoadUint32(&m.tabActorRedrawPending); got != 0 {
		t.Fatalf("expected msgSink-only redraw path not to latch pending flag, got %d", got)
	}

	for i := 0; i < 2; i++ {
		select {
		case msg := <-sinkMsgs:
			if _, ok := msg.(tabActorRedraw); !ok {
				t.Fatalf("expected redraw message, got %T", msg)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("expected redraw message %d from msgSink-only fallback", i+1)
		}
	}
}

func TestRequestTabActorRedraw_DropDoesNotLatchPending(t *testing.T) {
	m := newTestModel()
	attempts := 0
	sinkMsgs := make(chan tea.Msg, 2)
	m.msgSinkTry = func(msg tea.Msg) bool {
		attempts++
		if attempts == 1 {
			return false
		}
		sinkMsgs <- msg
		return true
	}
	m.msgSink = func(msg tea.Msg) {
		_ = m.msgSinkTry(msg)
	}

	m.requestTabActorRedraw()
	if got := atomic.LoadUint32(&m.tabActorRedrawPending); got != 0 {
		t.Fatalf("expected dropped redraw not to latch pending flag, got %d", got)
	}

	m.requestTabActorRedraw()
	select {
	case msg := <-sinkMsgs:
		if _, ok := msg.(tabActorRedraw); !ok {
			t.Fatalf("expected redraw message after retry, got %T", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected redraw retry after dropped enqueue")
	}
}

func TestRunTabActor_EventProcessingRefreshesHeartbeat(t *testing.T) {
	m := newTestModel()
	m.tabEvents = make(chan tabEvent, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- m.RunTabActor(ctx)
	}()

	deadline := time.Now().Add(time.Second)
	for !m.isTabActorReady() {
		if time.Now().After(deadline) {
			t.Fatal("expected actor startup to set readiness directly")
		}
		time.Sleep(10 * time.Millisecond)
	}

	stale := time.Now().Add(-(tabActorStallTimeout + time.Second)).UnixNano()
	atomic.StoreInt64(&m.tabActorHeartbeat, stale)
	if m.isTabActorReady() {
		t.Fatal("expected stale heartbeat to clear readiness before event processing")
	}

	m.tabEvents <- tabEvent{kind: tabEventSelectionClear, tab: &Tab{}}

	deadline = time.Now().Add(time.Second)
	for !m.isTabActorReady() {
		if time.Now().After(deadline) {
			t.Fatal("expected processed event to refresh actor heartbeat")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := atomic.LoadInt64(&m.tabActorHeartbeat); got <= stale {
		t.Fatalf("expected processed event to advance heartbeat, got %d <= %d", got, stale)
	}

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
