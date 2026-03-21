package center

import (
	"bytes"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestUpdatePTYFlush_UsesLargerChunkForActiveTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-active"),
		Workspace:         ws,
		Terminal:          vterm.New(80, 24),
		Running:           true,
		lastOutputAt:      time.Now().Add(-time.Second),
		flushPendingSince: time.Now().Add(-time.Second),
		pendingOutput:     bytes.Repeat([]byte("x"), ptyFlushChunkSizeActive+17),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})

	if got, want := len(tab.pendingOutput), 17; got != want {
		t.Fatalf("pending output = %d, want %d", got, want)
	}
}

func TestUpdatePTYFlush_CatchUpWithoutActorKeepsActiveChunkCap(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                   TabID("tab-active-catch-up"),
		Workspace:            ws,
		Terminal:             vterm.New(80, 24),
		Running:              true,
		lastOutputAt:         time.Now().Add(-time.Second),
		flushPendingSince:    time.Now().Add(-time.Second),
		pendingOutput:        bytes.Repeat([]byte("x"), ptyFlushChunkSizeActive+17),
		catchUpPendingOutput: true,
		catchUpTargetBytes:   ptyFlushChunkSizeActive + 17,
		ptyBytesReceived:     ptyFlushChunkSizeActive + 17,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID, CatchUp: true})

	if got, want := len(tab.pendingOutput), 17; got != want {
		t.Fatalf("pending output = %d, want %d after synchronous catch-up flush", got, want)
	}
}

func TestUpdatePTYFlush_FastForwardsCatchUpActiveTabViaActor(t *testing.T) {
	m := newTestModel()
	m.setTabActorReady()
	m.tabEvents = make(chan tabEvent, 1)

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	payload := bytes.Repeat([]byte("x"), ptyFlushChunkSizeActive+17)
	tab := &Tab{
		ID:                   TabID("tab-active-catch-up-actor"),
		Workspace:            ws,
		Terminal:             vterm.New(80, 24),
		Running:              true,
		lastOutputAt:         time.Now().Add(-time.Second),
		flushPendingSince:    time.Now().Add(-time.Second),
		pendingOutput:        append([]byte(nil), payload...),
		catchUpPendingOutput: true,
		catchUpTargetBytes:   uint64(len(payload)),
		ptyBytesReceived:     uint64(len(payload)),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID, CatchUp: true})

	select {
	case ev := <-m.tabEvents:
		if ev.kind != tabEventWriteOutput {
			t.Fatalf("expected tabEventWriteOutput, got %v", ev.kind)
		}
		if got, want := len(ev.output), len(payload); got != want {
			t.Fatalf("queued output len = %d, want %d", got, want)
		}
		if ev.hasMoreBuffered {
			t.Fatalf("expected catch-up flush to queue the full backlog in one write")
		}
	default:
		t.Fatalf("expected actor-backed catch-up flush to queue a write event")
	}

	if got := len(tab.pendingOutput); got != 0 {
		t.Fatalf("pending output = %d, want 0 after actor catch-up flush", got)
	}
}

func TestUpdatePTYFlush_CatchUpUsesBoundedActorChunk(t *testing.T) {
	m := newTestModel()
	m.setTabActorReady()
	m.tabEvents = make(chan tabEvent, 1)

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	payload := bytes.Repeat([]byte("x"), ptyFlushChunkSizeCatchUp+17)
	tab := &Tab{
		ID:                   TabID("tab-active-catch-up-bounded"),
		Workspace:            ws,
		Terminal:             vterm.New(80, 24),
		Running:              true,
		lastOutputAt:         time.Now().Add(-time.Second),
		flushPendingSince:    time.Now().Add(-time.Second),
		pendingOutput:        append([]byte(nil), payload...),
		catchUpPendingOutput: true,
		catchUpTargetBytes:   uint64(len(payload)),
		ptyBytesReceived:     uint64(len(payload)),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID, CatchUp: true})

	select {
	case ev := <-m.tabEvents:
		if ev.kind != tabEventWriteOutput {
			t.Fatalf("expected tabEventWriteOutput, got %v", ev.kind)
		}
		if got, want := len(ev.output), ptyFlushChunkSizeCatchUp; got != want {
			t.Fatalf("queued output len = %d, want %d", got, want)
		}
		if !ev.hasMoreBuffered {
			t.Fatalf("expected bounded catch-up write to leave buffered output behind")
		}
	default:
		t.Fatalf("expected bounded catch-up flush to queue a write event")
	}

	if got, want := len(tab.pendingOutput), 17; got != want {
		t.Fatalf("pending output = %d, want %d after bounded actor catch-up flush", got, want)
	}
}

func TestUpdatePTYFlush_PendingCatchUpOverridesStaleFlushMessage(t *testing.T) {
	m := newTestModel()
	m.setTabActorReady()
	m.tabEvents = make(chan tabEvent, 1)

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	payload := bytes.Repeat([]byte("x"), ptyFlushChunkSizeCatchUp+17)
	tab := &Tab{
		ID:                   TabID("tab-active-catch-up-stale-flush"),
		Workspace:            ws,
		Terminal:             vterm.New(80, 24),
		Running:              true,
		lastOutputAt:         time.Now().Add(-time.Second),
		flushPendingSince:    time.Now().Add(-time.Second),
		pendingOutput:        append([]byte(nil), payload...),
		catchUpPendingOutput: true,
		catchUpTargetBytes:   uint64(len(payload)),
		ptyBytesReceived:     uint64(len(payload)),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})

	select {
	case ev := <-m.tabEvents:
		if ev.kind != tabEventWriteOutput {
			t.Fatalf("expected tabEventWriteOutput, got %v", ev.kind)
		}
		if got, want := len(ev.output), ptyFlushChunkSizeCatchUp; got != want {
			t.Fatalf("queued output len = %d, want %d", got, want)
		}
	default:
		t.Fatalf("expected stale flush message to honor latched catch-up state")
	}

	if got, want := len(tab.pendingOutput), 17; got != want {
		t.Fatalf("pending output = %d, want %d after latched catch-up flush", got, want)
	}
}

func TestUpdatePTYFlush_StaleCatchUpMessageDoesNotRelatchAfterBacklogDrains(t *testing.T) {
	m := newTestModel()
	m.setTabActorReady()
	m.tabEvents = make(chan tabEvent, 1)

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-active-catch-up-stale-relatch"),
		Workspace:         ws,
		Terminal:          vterm.New(80, 24),
		Running:           true,
		lastOutputAt:      time.Now().Add(-time.Second),
		flushPendingSince: time.Now().Add(-time.Second),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID, CatchUp: true})
	if tab.catchUpPendingOutput {
		t.Fatalf("expected stale catch-up message with empty backlog not to relatch catch-up state")
	}

	tab.pendingOutput = bytes.Repeat([]byte("x"), ptyFlushChunkSizeActive+17)
	tab.lastOutputAt = time.Now().Add(-time.Second)
	tab.flushPendingSince = time.Now().Add(-time.Second)

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})

	select {
	case ev := <-m.tabEvents:
		if got, want := len(ev.output), ptyFlushChunkSizeActive; got != want {
			t.Fatalf("queued output len = %d, want normal active cap %d after stale catch-up", got, want)
		}
	default:
		t.Fatalf("expected active flush after new backlog")
	}
}

func TestUpdatePTYFlush_UsesBaseChunkForInactiveTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	active := &Tab{
		ID:        TabID("tab-active"),
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
	}
	inactive := &Tab{
		ID:                TabID("tab-inactive"),
		Workspace:         ws,
		Terminal:          vterm.New(80, 24),
		Running:           true,
		lastOutputAt:      time.Now().Add(-time.Second),
		flushPendingSince: time.Now().Add(-time.Second),
		pendingOutput:     bytes.Repeat([]byte("x"), ptyFlushChunkSize+17),
	}
	m.tabsByWorkspace[wsID] = []*Tab{active, inactive}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: inactive.ID})

	if got, want := len(inactive.pendingOutput), 17; got != want {
		t.Fatalf("pending output = %d, want %d", got, want)
	}
}

func TestUpdatePTYFlush_CatchUpHintIgnoredWhenTabIsNoLongerActive(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	active := &Tab{
		ID:        TabID("tab-active"),
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
	}
	inactive := &Tab{
		ID:                   TabID("tab-inactive-catch-up"),
		Workspace:            ws,
		Terminal:             vterm.New(80, 24),
		Running:              true,
		lastOutputAt:         time.Now().Add(-time.Second),
		flushPendingSince:    time.Now().Add(-time.Second),
		pendingOutput:        bytes.Repeat([]byte("x"), ptyFlushChunkSize+17),
		catchUpPendingOutput: true,
		catchUpTargetBytes:   ptyFlushChunkSize + 17,
		ptyBytesReceived:     ptyFlushChunkSize + 17,
	}
	m.tabsByWorkspace[wsID] = []*Tab{active, inactive}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: inactive.ID})

	if got, want := len(inactive.pendingOutput), 17; got != want {
		t.Fatalf("pending output = %d, want %d when catch-up tab is inactive", got, want)
	}
	if inactive.catchUpPendingOutput {
		t.Fatalf("expected inactive flush to clear latched catch-up state")
	}
}

func TestUpdatePTYFlush_CatchUpFallsBackToActiveChunkWhenActorQueueIsFull(t *testing.T) {
	m := newTestModel()
	m.setTabActorReady()
	m.tabEvents = make(chan tabEvent, 1)
	m.tabEvents <- tabEvent{kind: tabEventWriteOutput}

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	payload := bytes.Repeat([]byte("x"), ptyFlushChunkSizeActive+17)
	tab := &Tab{
		ID:                   TabID("tab-active-catch-up-queue-full"),
		Workspace:            ws,
		Terminal:             vterm.New(80, 24),
		Running:              true,
		lastOutputAt:         time.Now().Add(-time.Second),
		flushPendingSince:    time.Now().Add(-time.Second),
		pendingOutput:        append([]byte(nil), payload...),
		catchUpPendingOutput: true,
		catchUpTargetBytes:   uint64(len(payload)),
		ptyBytesReceived:     uint64(len(payload)),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID, CatchUp: true})

	if got, want := len(tab.pendingOutput), 17; got != want {
		t.Fatalf("pending output = %d, want %d after queue-full catch-up fallback", got, want)
	}
	if tab.actorWritesPending != 0 {
		t.Fatalf("expected no actor writes pending after queue-full catch-up fallback, got %d", tab.actorWritesPending)
	}
	if !tab.flushScheduled {
		t.Fatalf("expected queue-full catch-up fallback to schedule another flush")
	}
	if !tab.catchUpPendingOutput {
		t.Fatalf("expected queue-full catch-up fallback to keep catch-up latched for the remainder")
	}
	if tab.Terminal.Render() == vterm.New(80, 24).Render() {
		t.Fatalf("expected queue-full catch-up fallback to advance the visible terminal synchronously")
	}
}

func TestUpdatePTYFlush_CatchUpClearsAfterInitialBacklogPass(t *testing.T) {
	m := newTestModel()
	m.setTabActorReady()
	m.tabEvents = make(chan tabEvent, 2)

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	initialBacklog := bytes.Repeat([]byte("x"), ptyFlushChunkSizeCatchUp)
	steadyState := bytes.Repeat([]byte("y"), ptyFlushChunkSizeActive+17)
	tab := &Tab{
		ID:                   TabID("tab-active-catch-up-clears"),
		Workspace:            ws,
		Terminal:             vterm.New(80, 24),
		Running:              true,
		lastOutputAt:         time.Now().Add(-time.Second),
		flushPendingSince:    time.Now().Add(-time.Second),
		pendingOutput:        append(append([]byte(nil), initialBacklog...), steadyState...),
		catchUpPendingOutput: true,
		catchUpTargetBytes:   uint64(len(initialBacklog)),
		ptyBytesReceived:     uint64(len(initialBacklog) + len(steadyState)),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID, CatchUp: true})

	var first tabEvent
	select {
	case first = <-m.tabEvents:
		if got, want := len(first.output), len(initialBacklog); got != want {
			t.Fatalf("first catch-up output len = %d, want %d", got, want)
		}
	default:
		t.Fatalf("expected first actor-backed catch-up flush")
	}

	m.handleTabEvent(first)

	if tab.catchUpPendingOutput {
		t.Fatalf("expected catch-up latch to clear once the selected backlog target is settled")
	}

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})

	select {
	case second := <-m.tabEvents:
		if got, want := len(second.output), ptyFlushChunkSizeActive; got != want {
			t.Fatalf("steady-state output len = %d, want active cap %d after catch-up clears", got, want)
		}
	default:
		t.Fatalf("expected steady-state flush after catch-up target settles")
	}
}
