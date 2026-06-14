package center

import (
	"sync/atomic"
	"testing"
	"time"
)

// StartPTYReaders only spawns a real PTY read goroutine when a tab is both
// Running and backed by a live *appPty.Agent terminal; with Running=false or no
// Agent it short-circuits in startPTYReader, so the iteration, stall-detection
// and skip guards are exercised here without execing tmux or requiring a live
// Bubble Tea program.
func TestStartPTYReaders(t *testing.T) {
	t.Run("no workspaces is a no-op returning nil cmd", func(t *testing.T) {
		m := newTestModel()
		if cmd := m.StartPTYReaders(); cmd != nil {
			t.Fatalf("expected nil cmd for empty model, got non-nil")
		}
	})

	t.Run("nil and closed tabs are skipped without starting a reader", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		closed := &Tab{ID: TabID("closed"), Assistant: "claude", Workspace: ws, Running: true}
		closed.markClosed()
		m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{nil, closed}

		if cmd := m.StartPTYReaders(); cmd != nil {
			t.Fatalf("expected nil cmd")
		}
		// A closed tab must never have had its reader marked active.
		closed.mu.Lock()
		active := closed.ReaderActive
		closed.mu.Unlock()
		if active {
			t.Fatal("expected closed tab reader to stay inactive")
		}
	})

	t.Run("non-running tab does not spawn a reader", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		tab := &Tab{ID: TabID("idle"), Assistant: "claude", Workspace: ws, Running: false}
		m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{tab}

		if cmd := m.StartPTYReaders(); cmd != nil {
			t.Fatalf("expected nil cmd")
		}
		tab.mu.Lock()
		active := tab.ReaderActive
		tab.mu.Unlock()
		if active {
			t.Fatal("expected non-running tab to have no active reader")
		}
	})

	t.Run("stalled active reader is stopped before restart", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		// ReaderActive with a heartbeat older than the stall timeout triggers the
		// stall branch, which calls stopPTYReader (clearing ReaderActive and
		// zeroing the heartbeat). Running stays false so no new goroutine spawns.
		stale := time.Now().Add(-(ptyReaderStallTimeout + time.Second)).UnixNano()
		tab := &Tab{ID: TabID("stalled"), Assistant: "claude", Workspace: ws}
		tab.ReaderActive = true
		atomic.StoreInt64(&tab.Heartbeat, stale)
		m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{tab}

		if cmd := m.StartPTYReaders(); cmd != nil {
			t.Fatalf("expected nil cmd")
		}
		tab.mu.Lock()
		active := tab.ReaderActive
		tab.mu.Unlock()
		if active {
			t.Fatal("expected stalled reader to be stopped")
		}
		if hb := atomic.LoadInt64(&tab.Heartbeat); hb != 0 {
			t.Fatalf("expected heartbeat reset to 0 after stop, got %d", hb)
		}
	})

	t.Run("fresh active reader is left running", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		fresh := time.Now().UnixNano()
		tab := &Tab{ID: TabID("fresh"), Assistant: "claude", Workspace: ws}
		tab.ReaderActive = true
		atomic.StoreInt64(&tab.Heartbeat, fresh)
		m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{tab}

		if cmd := m.StartPTYReaders(); cmd != nil {
			t.Fatalf("expected nil cmd")
		}
		tab.mu.Lock()
		active := tab.ReaderActive
		tab.mu.Unlock()
		if !active {
			t.Fatal("expected a freshly-beating reader to be left active")
		}
		if hb := atomic.LoadInt64(&tab.Heartbeat); hb != fresh {
			t.Fatalf("expected heartbeat untouched, got %d want %d", hb, fresh)
		}
	})

	t.Run("active reader with zero heartbeat is not treated as stalled", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		tab := &Tab{ID: TabID("nobeat"), Assistant: "claude", Workspace: ws}
		tab.ReaderActive = true
		atomic.StoreInt64(&tab.Heartbeat, 0)
		m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{tab}

		if cmd := m.StartPTYReaders(); cmd != nil {
			t.Fatalf("expected nil cmd")
		}
		tab.mu.Lock()
		active := tab.ReaderActive
		tab.mu.Unlock()
		if !active {
			t.Fatal("expected reader with zero heartbeat to be left untouched (lastBeat > 0 guard)")
		}
	})
}
