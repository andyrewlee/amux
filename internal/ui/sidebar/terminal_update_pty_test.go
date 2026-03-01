package sidebar

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestHandleDirectPTYOutputChunk_UsesTabPointerWithoutLookup(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &TerminalTab{
		ID: generateTerminalTabID(),
		State: &TerminalState{
			VTerm:   vterm.New(80, 24),
			Running: true,
		},
	}
	flushCh := make(chan messages.SidebarPTYFlush, 1)
	m.msgSink = func(msg tea.Msg) {
		if flush, ok := msg.(messages.SidebarPTYFlush); ok {
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
		if flush.WorkspaceID != wsID || flush.TabID != string(tab.ID) {
			t.Fatalf("unexpected flush message: %+v", flush)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected direct path to emit SidebarPTYFlush")
	}

	ts := tab.State
	ts.mu.Lock()
	ts.pendingOutput.Clear()
	ts.flushScheduled = false
	ts.flushPendingSince = time.Time{}
	ts.mu.Unlock()
}

func TestHandleDirectPTYOutputChunk_RetriesFlushAfterDroppedSinkMessage(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	tab := &TerminalTab{
		ID: tabID,
		State: &TerminalState{
			VTerm:   vterm.New(80, 24),
			Running: true,
		},
	}
	m.tabsByWorkspace[wsID] = []*TerminalTab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	oldRetry := sidebarDirectFlushRetryInterval
	sidebarDirectFlushRetryInterval = 5 * time.Millisecond
	defer func() {
		sidebarDirectFlushRetryInterval = oldRetry
	}()

	var mu sync.Mutex
	sinkCalls := 0
	flushCh := make(chan messages.SidebarPTYFlush, 2)
	m.msgSink = func(msg tea.Msg) {
		flush, ok := msg.(messages.SidebarPTYFlush)
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
	ts := tab.State
	ts.mu.Lock()
	ts.Running = false
	ts.mu.Unlock()

	var flush messages.SidebarPTYFlush
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

	ts.mu.Lock()
	ts.lastOutputAt = time.Now().Add(-time.Second)
	ts.flushPendingSince = ts.lastOutputAt
	ts.mu.Unlock()
	_ = m.handlePTYFlush(flush)

	ts.mu.Lock()
	ts.pendingOutput.Clear()
	ts.flushScheduled = false
	ts.flushPendingSince = time.Time{}
	ts.mu.Unlock()
}

func TestHandleDirectPTYOutputChunk_RetryStopsAfterTeardown(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	tab := &TerminalTab{
		ID: tabID,
		State: &TerminalState{
			VTerm:   vterm.New(80, 24),
			Running: true,
		},
	}
	m.tabsByWorkspace[wsID] = []*TerminalTab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	oldRetry := sidebarDirectFlushRetryInterval
	sidebarDirectFlushRetryInterval = 5 * time.Millisecond
	defer func() {
		sidebarDirectFlushRetryInterval = oldRetry
	}()

	var sinkCalls atomic.Int64
	m.msgSink = func(msg tea.Msg) {
		if _, ok := msg.(messages.SidebarPTYFlush); ok {
			sinkCalls.Add(1)
		}
	}

	if ok := m.handleDirectPTYOutputChunk(wsID, tab, []byte("keep-busy")); !ok {
		t.Fatal("expected direct PTY chunk handler to continue")
	}

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if sinkCalls.Load() >= 2 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := sinkCalls.Load(); got < 2 {
		t.Fatalf("expected retry loop to emit at least twice before teardown, got %d", got)
	}

	ts := tab.State
	ts.mu.Lock()
	resetPTYOutputStateLocked(ts)
	ts.Running = false
	ts.mu.Unlock()
	delete(m.tabsByWorkspace, wsID)

	time.Sleep(30 * time.Millisecond)
	stableFrom := sinkCalls.Load()
	time.Sleep(30 * time.Millisecond)
	stableTo := sinkCalls.Load()
	if stableTo != stableFrom {
		t.Fatalf("expected retry loop to stop after teardown, calls grew from %d to %d", stableFrom, stableTo)
	}

	ts.mu.Lock()
	retryArmed := ts.directFlushRetryArmed
	ts.mu.Unlock()
	if retryArmed {
		t.Fatal("expected retry loop to disarm after teardown")
	}
}

func TestHandleDirectPTYOutputChunk_RetryUsesReboundWorkspaceID(t *testing.T) {
	m := NewTerminalModel()
	oldWS := data.NewWorkspace("old", "old", "main", "/repo/old", "/repo/old")
	newWS := data.NewWorkspace("new", "new", "main", "/repo/new", "/repo/new")
	oldID := string(oldWS.ID())
	newID := string(newWS.ID())
	if oldID == newID {
		t.Fatalf("expected different workspace IDs, got %q", oldID)
	}
	tabID := generateTerminalTabID()
	tab := &TerminalTab{
		ID: tabID,
		State: &TerminalState{
			VTerm:       vterm.New(80, 24),
			Running:     true,
			workspaceID: oldID,
		},
	}
	m.tabsByWorkspace[oldID] = []*TerminalTab{tab}
	m.activeTabByWorkspace[oldID] = 0
	m.workspace = oldWS

	oldRetry := sidebarDirectFlushRetryInterval
	sidebarDirectFlushRetryInterval = 20 * time.Millisecond
	defer func() {
		sidebarDirectFlushRetryInterval = oldRetry
	}()

	var sinkCalls atomic.Int64
	flushCh := make(chan messages.SidebarPTYFlush, 2)
	m.msgSink = func(msg tea.Msg) {
		flush, ok := msg.(messages.SidebarPTYFlush)
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
		if flush.TabID != string(tabID) {
			t.Fatalf("expected retry flush tab %q, got %q", tabID, flush.TabID)
		}
	case <-time.After(400 * time.Millisecond):
		t.Fatal("expected retry flush after workspace rebind")
	}

	ts := tab.State
	ts.mu.Lock()
	ts.pendingOutput.Clear()
	ts.flushScheduled = false
	ts.flushPendingSince = time.Time{}
	ts.mu.Unlock()
}
