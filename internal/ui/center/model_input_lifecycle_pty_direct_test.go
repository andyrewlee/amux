package center

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestHandleDirectPTYOutputChunk_UsesTabPointerWithoutLookup(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-direct"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
	}
	flushCh := make(chan PTYFlush, 1)
	m.msgSink = func(msg tea.Msg) {
		if flush, ok := msg.(PTYFlush); ok {
			select {
			case flushCh <- flush:
			default:
			}
		}
	}

	if ok := m.handleDirectPTYOutputChunk(wsID, tab, []byte("hello")); !ok {
		t.Fatal("expected direct PTY chunk handler to continue")
	}

	select {
	case flush := <-flushCh:
		if flush.WorkspaceID != wsID || flush.TabID != tab.ID {
			t.Fatalf("unexpected flush message: %+v", flush)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected direct path to emit PTYFlush")
	}

	tab.mu.Lock()
	tab.pendingOutput.Clear()
	tab.flushScheduled = false
	tab.flushPendingSince = time.Time{}
	tab.mu.Unlock()
}

func TestHandleDirectPTYOutputChunk_RetriesFlushAfterDroppedSinkMessage(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-direct-retry"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	oldRetry := ptyDirectFlushRetryInterval
	ptyDirectFlushRetryInterval = 5 * time.Millisecond
	defer func() {
		ptyDirectFlushRetryInterval = oldRetry
	}()

	var mu sync.Mutex
	sinkCalls := 0
	flushCh := make(chan PTYFlush, 2)
	m.msgSink = func(msg tea.Msg) {
		flush, ok := msg.(PTYFlush)
		if !ok {
			return
		}
		mu.Lock()
		sinkCalls++
		callNum := sinkCalls
		mu.Unlock()
		if callNum == 1 {
			return // Simulate one dropped non-critical external message.
		}
		select {
		case flushCh <- flush:
		default:
		}
	}

	if ok := m.handleDirectPTYOutputChunk(wsID, tab, []byte("retry-me")); !ok {
		t.Fatal("expected direct PTY chunk handler to continue")
	}

	var flush PTYFlush
	select {
	case flush = <-flushCh:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected retry flush after dropped direct-path enqueue")
	}

	mu.Lock()
	if sinkCalls < 2 {
		mu.Unlock()
		t.Fatalf("expected at least two sink attempts, got %d", sinkCalls)
	}
	mu.Unlock()

	tab.mu.Lock()
	tab.lastOutputAt = time.Now().Add(-time.Second)
	tab.flushPendingSince = tab.lastOutputAt
	tab.mu.Unlock()
	_ = m.updatePTYFlush(flush)

	tab.mu.Lock()
	tab.pendingOutput.Clear()
	tab.flushScheduled = false
	tab.flushPendingSince = time.Time{}
	tab.mu.Unlock()
}

func TestHandleDirectPTYOutputChunk_RetryUsesReboundWorkspaceID(t *testing.T) {
	m := newTestModel()
	oldWS := newTestWorkspace("old", "/repo/old")
	newWS := newTestWorkspace("new", "/repo/new")
	oldID := string(oldWS.ID())
	newID := string(newWS.ID())
	if oldID == newID {
		t.Fatalf("expected different workspace IDs, got %q", oldID)
	}

	tab := &Tab{
		ID:        TabID("tab-direct-rebind"),
		Assistant: "codex",
		Workspace: oldWS,
		Terminal:  vterm.New(80, 24),
		Running:   true,
	}
	m.tabsByWorkspace[oldID] = []*Tab{tab}
	m.activeTabByWorkspace[oldID] = 0
	m.workspace = oldWS

	oldRetry := ptyDirectFlushRetryInterval
	ptyDirectFlushRetryInterval = 20 * time.Millisecond
	defer func() {
		ptyDirectFlushRetryInterval = oldRetry
	}()

	var sinkCalls atomic.Int64
	flushCh := make(chan PTYFlush, 2)
	m.msgSink = func(msg tea.Msg) {
		flush, ok := msg.(PTYFlush)
		if !ok {
			return
		}
		if sinkCalls.Add(1) == 1 {
			return // Drop initial direct flush so assertion targets retry message.
		}
		select {
		case flushCh <- flush:
		default:
		}
	}

	if ok := m.handleDirectPTYOutputChunk(oldID, tab, []byte("rebind-retry")); !ok {
		t.Fatal("expected direct PTY chunk handler to continue")
	}

	_ = m.RebindWorkspaceID(oldWS, newWS)

	select {
	case flush := <-flushCh:
		if flush.WorkspaceID != newID {
			t.Fatalf("expected retry flush workspace %q after rebind, got %q", newID, flush.WorkspaceID)
		}
		if flush.TabID != tab.ID {
			t.Fatalf("expected retry flush tab %q, got %q", tab.ID, flush.TabID)
		}
	case <-time.After(400 * time.Millisecond):
		t.Fatal("expected retry flush after workspace rebind")
	}

	tab.mu.Lock()
	tab.pendingOutput.Clear()
	tab.flushScheduled = false
	tab.flushPendingSince = time.Time{}
	tab.mu.Unlock()
}

func TestHandleDirectPTYOutputChunk_RetryClearsStaleScheduledStateWhenPendingCleared(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-direct-stale-scheduled"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	oldRetry := ptyDirectFlushRetryInterval
	ptyDirectFlushRetryInterval = 5 * time.Millisecond
	defer func() {
		ptyDirectFlushRetryInterval = oldRetry
	}()

	var sinkCalls atomic.Int64
	m.msgSink = func(msg tea.Msg) {
		if _, ok := msg.(PTYFlush); ok {
			sinkCalls.Add(1)
		}
	}

	if ok := m.handleDirectPTYOutputChunk(wsID, tab, []byte("first")); !ok {
		t.Fatal("expected direct PTY chunk handler to continue")
	}

	// Simulate external teardown/reset path that clears buffered output but
	// leaves flushScheduled stale.
	tab.mu.Lock()
	tab.pendingOutput.Clear()
	tab.mu.Unlock()

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		tab.mu.Lock()
		armed := tab.directFlushRetryArmed
		scheduled := tab.flushScheduled
		tab.mu.Unlock()
		if !armed {
			if scheduled {
				t.Fatal("expected retry loop to clear stale flushScheduled when pending becomes empty")
			}
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	tab.mu.Lock()
	scheduledAfter := tab.flushScheduled
	tab.mu.Unlock()
	if scheduledAfter {
		t.Fatal("expected flushScheduled=false after retry exits on empty pending output")
	}

	before := sinkCalls.Load()
	if ok := m.handleDirectPTYOutputChunk(wsID, tab, []byte("second")); !ok {
		t.Fatal("expected direct PTY chunk handler to continue")
	}

	deadline = time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if sinkCalls.Load() > before {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if sinkCalls.Load() <= before {
		t.Fatal("expected subsequent chunk to emit PTYFlush after stale scheduling state is cleared")
	}

	tab.mu.Lock()
	tab.pendingOutput.Clear()
	tab.flushScheduled = false
	tab.flushPendingSince = time.Time{}
	tab.mu.Unlock()
}
