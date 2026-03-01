package sidebar

import (
	"sync"
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

	ts := tab.State
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
