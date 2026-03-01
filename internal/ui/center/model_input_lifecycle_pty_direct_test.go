package center

import (
	"sync"
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
