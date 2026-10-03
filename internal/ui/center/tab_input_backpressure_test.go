package center

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// ---------------------------------------------------------------------------
// TestTerminalInputBackpressure — bounded admission + visible rejection
// ---------------------------------------------------------------------------

func TestTerminalInputBackpressure(t *testing.T) {
	t.Run("request bound rejects without partial delivery", func(t *testing.T) {
		stubTabInputQueue(t, 4, 1<<20)
		rec := newRecordingSend()
		rec.release = make(chan struct{})
		stubTabInputSend(t, rec.send)

		ws := newTestWorkspace("ws", t.TempDir())
		tab := boundInputTab(t, ws, "tab-sat", "claude")
		m, _, _ := newActionsModel(t, tab)

		for i := 0; i < 4; i++ {
			res, _ := m.admitTabInput(tab, "x", "Input", true)
			if res != tabInputAdmitted {
				t.Fatalf("request %d: expected admission, got %v", i, res)
			}
			if i == 0 {
				<-rec.entered // hold the first send so in-flight counts
			}
		}
		res, gen := m.admitTabInput(tab, "overflow", "Input", true)
		if res != tabInputRejectedFull {
			t.Fatalf("5th request: expected tabInputRejectedFull, got %v", res)
		}
		if gen != 1 {
			t.Fatalf("rejection gen = %d, want binding gen 1", gen)
		}
		close(rec.release)
		// All four admitted requests — and nothing else — must be delivered.
		testutil.Eventually(t, 3*time.Second, time.Millisecond, func() bool {
			return rec.delivered() == 4
		}, "writer did not drain the admitted requests")
		if got := rec.snapshot(); len(got) != 4 {
			t.Fatalf("delivered = %q, want exactly the 4 admitted requests", got)
		}
	})

	t.Run("byte bound rejects atomically", func(t *testing.T) {
		stubTabInputQueue(t, 4096, 16)
		rec := newRecordingSend()
		rec.release = make(chan struct{})
		stubTabInputSend(t, rec.send)

		ws := newTestWorkspace("ws", t.TempDir())
		tab := boundInputTab(t, ws, "tab-bytes", "claude")
		m, _, _ := newActionsModel(t, tab)

		res, _ := m.admitTabInput(tab, "1234567890", "Input", true)
		if res != tabInputAdmitted {
			t.Fatalf("first request rejected: %v", res)
		}
		<-rec.entered
		if res, _ := m.admitTabInput(tab, "123456", "Input", true); res != tabInputAdmitted {
			t.Fatalf("second request rejected: %v", res)
		}
		// 10 + 6 + 1 > 16: rejected as a whole — the writer never sees a
		// truncated fragment.
		if res, _ := m.admitTabInput(tab, "1", "Input", true); res != tabInputRejectedFull {
			t.Fatalf("overflow byte rejected as %v, want tabInputRejectedFull", res)
		}
		// A single request larger than the bound is rejected atomically.
		if res, _ := m.admitTabInput(tab, strings.Repeat("y", 17), "Input", true); res != tabInputRejectedFull {
			t.Fatalf("oversized request rejected as %v, want tabInputRejectedFull", res)
		}

		close(rec.release)
		testutil.Eventually(t, 3*time.Second, time.Millisecond, func() bool {
			return rec.delivered() == 2
		}, "writer did not drain admitted requests")
		if got := rec.snapshot(); !slices.Equal(got, []string{"1234567890", "123456"}) {
			t.Fatalf("delivered = %q, want only the two admitted requests", got)
		}
	})

	t.Run("rejection is visible and keeps the attachment healthy", func(t *testing.T) {
		stubTabInputQueue(t, 1, 1<<20)
		rec := newRecordingSend()
		stubTabInputSend(t, rec.send)

		ws := newTestWorkspace("ws", t.TempDir())
		tab := boundInputTab(t, ws, "tab-rej", "claude")
		m, _, wsID := newActionsModel(t, tab)

		if res, _ := m.admitTabInput(tab, "x", "Input", true); res != tabInputAdmitted {
			t.Fatalf("first request rejected: %v", res)
		}
		res, gen := m.admitTabInput(tab, "y", "Input", true)
		if res != tabInputRejectedFull {
			t.Fatalf("expected tabInputRejectedFull, got %v", res)
		}

		cmd := m.rejectedTabInputCmd(tab, res, gen)
		if cmd == nil {
			t.Fatal("expected a rejection command")
		}
		msg, ok := cmd().(TabInputRejected)
		if !ok {
			t.Fatalf("expected TabInputRejected, got %T", cmd())
		}
		if msg.TabID != tab.ID || msg.WorkspaceID != wsID || msg.Gen != 1 || msg.Reason == "" {
			t.Fatalf("malformed rejection: %+v", msg)
		}
		var critical any = msg
		if _, ok := critical.(common.CriticalExternalMsg); !ok {
			t.Fatal("TabInputRejected must be a critical external message")
		}

		// The tab stays attached and running — a rejection is a dropped
		// keystroke, not a dead terminal.
		if running, detached := tabFlags(tab); !running || detached {
			t.Fatalf("rejection disturbed the attachment: running=%v detached=%v", running, detached)
		}
	})

	t.Run("blocked write admits without blocking the update loop", func(t *testing.T) {
		rec := newRecordingSend()
		rec.release = make(chan struct{})
		stubTabInputSend(t, rec.send)

		ws := newTestWorkspace("ws", t.TempDir())
		tab := boundInputTab(t, ws, "tab-blocked", "claude")
		m, _, _ := newActionsModel(t, tab)

		if res, _ := m.admitTabInput(tab, "x", "Input", true); res != tabInputAdmitted {
			t.Fatalf("first request rejected: %v", res)
		}
		<-rec.entered // the send is now blocked
		// Admission still returns immediately — the queued request is proof
		// the update loop never waits on the PTY write.
		if res, _ := m.admitTabInput(tab, "y", "Input", true); res != tabInputAdmitted {
			t.Fatalf("queued request rejected: %v", res)
		}
		if !tab.mu.TryLock() {
			t.Fatal("tab.mu held while the writer's send was blocked")
		}
		tab.mu.Unlock()
		close(rec.release)
		testutil.Eventually(t, 3*time.Second, time.Millisecond, func() bool {
			return rec.delivered() == 2
		}, "blocked writer did not drain")
		tab.mu.Lock()
		tab.retireTabInputWriterLocked()
		tab.mu.Unlock()
		tab.joinRetiredInputWriters()
	})

	t.Run("no agent and closed tab are silent no-ops", func(t *testing.T) {
		m, _, _ := newActionsModel(t)

		noAgent := &Tab{ID: "tab-noagent", Running: true}
		res, _ := m.admitTabInput(noAgent, "x", "Input", true)
		if res != tabInputNoTerminal {
			t.Fatalf("nil agent: expected tabInputNoTerminal, got %v", res)
		}
		if cmd := m.rejectedTabInputCmd(noAgent, res, 0); cmd != nil {
			t.Fatal("no-terminal admission must not surface a rejection")
		}

		closed := boundInputTab(t, newTestWorkspace("ws2", t.TempDir()), "tab-closed", "claude")
		closed.markClosed()
		if res, _ := m.admitTabInput(closed, "x", "Input", true); res != tabInputNoTerminal {
			t.Fatalf("closed tab: expected tabInputNoTerminal, got %v", res)
		}

		if res, _ := m.admitTabInput(nil, "x", "Input", true); res != tabInputNoTerminal {
			t.Fatalf("nil tab: expected tabInputNoTerminal, got %v", res)
		}
	})
}
