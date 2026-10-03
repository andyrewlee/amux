package center

import (
	"errors"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/vterm"
)

// ---------------------------------------------------------------------------
// TestTerminalInputFailure — failure latch, once-notification, gen fencing
// ---------------------------------------------------------------------------

func TestTerminalInputFailure(t *testing.T) {
	t.Run("closed PTY failure latches and notifies without detaching", func(t *testing.T) {
		tab := newClosedPTYTab(t, TabID("tab-fail"), 7)
		ws := newTestWorkspace("ws", t.TempDir())
		tab.Workspace = ws
		m, _, wsID := newActionsModel(t, tab)
		var sink msgCollector
		m.msgSink = sink.sink()

		res, gen := m.admitTabInput(tab, "x", "Input", true)
		if res != tabInputAdmitted || gen != 7 {
			t.Fatalf("admission = %v gen %d, want admitted gen 7", res, gen)
		}

		testutil.Eventually(t, 3*time.Second, time.Millisecond, func() bool {
			tab.mu.Lock()
			defer tab.mu.Unlock()
			return tab.tabInput.failed
		}, "delivery failure did not latch")

		fails := sink.countOf(func(msg tea.Msg) bool {
			_, ok := msg.(TabInputFailed)
			return ok
		})
		if fails != 1 {
			t.Fatalf("expected exactly one TabInputFailed, got %d", fails)
		}
		var failed TabInputFailed
		for _, msg := range sink.snapshot() {
			if f, ok := msg.(TabInputFailed); ok {
				failed = f
			}
		}
		if failed.TabID != tab.ID || failed.WorkspaceID != wsID || failed.Gen != 7 || failed.Err == nil {
			t.Fatalf("malformed failure: %+v", failed)
		}

		// The worker must not detach — the app failure handler owns the
		// visible transition.
		if running, detached := tabFlags(tab); !running || detached {
			t.Fatalf("worker detached the tab: running=%v detached=%v", running, detached)
		}
		awaitWriterExit(t, awaitInputWriter(t, tab))
	})

	t.Run("failure latch rejects further input without recreating the writer", func(t *testing.T) {
		tab := newClosedPTYTab(t, TabID("tab-latch"), 3)
		m, _, _ := newActionsModel(t, tab)
		m.msgSink = func(tea.Msg) {}

		m.admitTabInput(tab, "x", "Input", true)
		testutil.Eventually(t, 3*time.Second, time.Millisecond, func() bool {
			tab.mu.Lock()
			defer tab.mu.Unlock()
			return tab.tabInput.failed
		}, "delivery failure did not latch")

		tab.mu.Lock()
		w := tab.tabInput.writer
		tab.mu.Unlock()
		res, gen := m.admitTabInput(tab, "y", "Input", true)
		if res != tabInputRejectedFailed {
			t.Fatalf("expected tabInputRejectedFailed, got %v", res)
		}
		tab.mu.Lock()
		if tab.tabInput.writer != w {
			t.Fatal("failed binding recreated its writer before the failure was handled")
		}
		tab.mu.Unlock()

		cmd := m.rejectedTabInputCmd(tab, res, gen)
		msg, ok := cmd().(TabInputRejected)
		if !ok {
			t.Fatalf("expected TabInputRejected, got %T", cmd())
		}
		if msg.Gen != 3 || msg.Reason == "" {
			t.Fatalf("malformed rejection: %+v", msg)
		}
	})

	t.Run("failure after workspace rebind still detaches the tab", func(t *testing.T) {
		tab := newClosedPTYTab(t, TabID("tab-rebind"), 5)
		ws := newTestWorkspace("ws", t.TempDir())
		tab.Workspace = ws
		m, _, wsOld := newActionsModel(t, tab)

		// Rebind: the tab now lives under a different workspace key, but the
		// in-flight failure was stamped with the old one.
		wsNew := "ws-rebound"
		m.tabs.ByWorkspace[wsNew] = []*Tab{tab}
		delete(m.tabs.ByWorkspace, wsOld)

		cmd, handled := m.DetachTabForInputFailure(wsOld, tab.ID, 5)
		if !handled {
			t.Fatal("valid failure was fenced out after rebind")
		}
		if _, detached := tabFlags(tab); !detached {
			t.Fatal("valid failure did not detach the tab")
		}
		if cmd == nil {
			t.Fatal("expected a TabDetached command")
		}
		if _, ok := cmd().(messages.TabDetached); !ok {
			t.Fatalf("expected TabDetached, got %T", cmd())
		}
	})

	t.Run("stale generation failure cannot detach the replacement", func(t *testing.T) {
		tab := boundInputTab(t, newTestWorkspace("ws", t.TempDir()), "tab-stalegen", "claude")
		m, _, wsID := newActionsModel(t, tab)
		m.msgSink = func(tea.Msg) {}

		// The failure claims gen 1; the binding has since moved to gen 2.
		tab.mu.Lock()
		tab.tabInput.gen = 2
		tab.mu.Unlock()

		if _, handled := m.DetachTabForInputFailure(wsID, tab.ID, 1); handled {
			t.Fatal("stale failure was not fenced")
		}
		if _, detached := tabFlags(tab); detached {
			t.Fatal("stale failure detached the replacement binding")
		}
		// A failure stamped with the current gen still applies.
		if _, handled := m.DetachTabForInputFailure(wsID, tab.ID, 2); !handled {
			t.Fatal("current failure was fenced")
		}
		if _, detached := tabFlags(tab); !detached {
			t.Fatal("current failure did not detach")
		}
	})
}

// ---------------------------------------------------------------------------
// TestTerminalInputRetirement — writer fencing, discard, shutdown joins
// ---------------------------------------------------------------------------

func TestTerminalInputRetirement(t *testing.T) {
	t.Run("retired writer's late success cannot stamp echo on the replacement", func(t *testing.T) {
		rec := newRecordingSend()
		rec.release = make(chan struct{})
		stubTabInputSend(t, rec.send)
		var sink msgCollector

		ws := newTestWorkspace("ws", t.TempDir())
		tab := boundInputTab(t, ws, "tab-lateok", "claude")
		m, _, _ := newActionsModel(t, tab)
		m.msgSink = sink.sink()

		if res, _ := m.admitTabInput(tab, "old", "Input", true); res != tabInputAdmitted {
			t.Fatalf("old request rejected: %v", res)
		}
		<-rec.entered // send is blocked mid-flight
		oldW := awaitInputWriter(t, tab)

		// Reattach: retire the old writer and bump the binding under one lock.
		tab.mu.Lock()
		tab.markDetachedLocked()
		tab.markAttachedLocked()
		gen := tab.tabInput.gen
		tab.mu.Unlock()
		if gen != 2 {
			t.Fatalf("expected attach to bump gen to 2, got %d", gen)
		}

		close(rec.release)
		awaitWriterExit(t, oldW)

		// The late success must not mutate echo/cursor state on the new
		// binding or emit a refresh stamped with the dead generation.
		tab.mu.Lock()
		echoed := !tab.lastUserInputAt.IsZero() || !tab.lastPromptInputAt.IsZero()
		tab.mu.Unlock()
		if echoed {
			t.Fatal("retired writer stamped echo state onto the replacement binding")
		}
		staleRefresh := sink.countOf(func(msg tea.Msg) bool {
			r, ok := msg.(PTYCursorRefresh)
			return ok && r.InputGeneration == 1
		})
		if staleRefresh != 0 {
			t.Fatalf("replacement saw %d refreshes stamped with the retired generation", staleRefresh)
		}

		// The replacement binding still delivers new input.
		if res, gen := m.admitTabInput(tab, "new", "Input", true); res != tabInputAdmitted || gen != 2 {
			t.Fatalf("new binding admission = %v gen %d", res, gen)
		}
		testutil.Eventually(t, 3*time.Second, time.Millisecond, func() bool {
			return rec.delivered() == 2
		}, "new binding writer did not deliver")
	})

	t.Run("retired writer's late failure cannot poison the replacement", func(t *testing.T) {
		rec := newRecordingSend()
		rec.release = make(chan struct{})
		rec.err = errors.New("boom")
		stubTabInputSend(t, rec.send)
		var sink msgCollector

		ws := newTestWorkspace("ws", t.TempDir())
		tab := boundInputTab(t, ws, "tab-latefail", "claude")
		m, _, _ := newActionsModel(t, tab)
		m.msgSink = sink.sink()

		m.admitTabInput(tab, "old", "Input", true)
		<-rec.entered
		oldW := awaitInputWriter(t, tab)

		tab.mu.Lock()
		tab.markDetachedLocked()
		tab.markAttachedLocked()
		tab.mu.Unlock()

		close(rec.release)
		awaitWriterExit(t, oldW)

		if fails := sink.countOf(func(msg tea.Msg) bool {
			_, ok := msg.(TabInputFailed)
			return ok
		}); fails != 0 {
			t.Fatalf("stale failure emitted %d TabInputFailed", fails)
		}
		tab.mu.Lock()
		failed := tab.tabInput.failed
		tab.mu.Unlock()
		if failed {
			t.Fatal("retired writer's failure latched the replacement binding")
		}
	})

	t.Run("detach retires the writer and joins it", func(t *testing.T) {
		rec := newRecordingSend()
		rec.release = make(chan struct{})
		stubTabInputSend(t, rec.send)

		ws := newTestWorkspace("ws", t.TempDir())
		tab := boundInputTab(t, ws, "tab-detach", "claude")
		m, _, _ := newActionsModel(t, tab)
		m.msgSink = func(tea.Msg) {}

		m.admitTabInput(tab, "x", "Input", true)
		<-rec.entered
		w := awaitInputWriter(t, tab)

		detached := make(chan tea.Cmd, 1)
		go func() { detached <- m.detachTabCore(tab, 0) }()

		// detach waits on the writer's join; the blocked send only returns
		// when the terminal is closed, which detachTabCore does itself — but
		// the seam's release stands in for that unblock here.
		select {
		case <-detached:
			t.Fatal("detach joined the writer before the in-flight send returned")
		case <-time.After(100 * time.Millisecond):
		}
		close(rec.release)
		select {
		case <-detached:
		case <-time.After(3 * time.Second):
			t.Fatal("detach did not join the retired writer")
		}
		awaitWriterExit(t, w)
		if _, detachedTab := tabFlags(tab); !detachedTab {
			t.Fatal("detach did not mark the tab detached")
		}
	})

	t.Run("queued input is discarded on retirement", func(t *testing.T) {
		rec := newRecordingSend()
		rec.release = make(chan struct{})
		stubTabInputSend(t, rec.send)

		ws := newTestWorkspace("ws", t.TempDir())
		tab := boundInputTab(t, ws, "tab-discard", "claude")
		m, _, _ := newActionsModel(t, tab)

		m.admitTabInput(tab, "first", "Input", true)
		<-rec.entered
		w := awaitInputWriter(t, tab)
		m.admitTabInput(tab, "second", "Input", true)
		m.admitTabInput(tab, "third", "Input", true)

		tab.mu.Lock()
		tab.markDetachedLocked()
		tab.mu.Unlock()
		close(rec.release)
		awaitWriterExit(t, w)

		// Only the in-flight send recorded; the queued requests were skipped.
		if got := rec.snapshot(); !slices.Equal(got, []string{"first"}) {
			t.Fatalf("delivered = %q, want only the in-flight request", got)
		}
	})

	t.Run("model Close joins writers", func(t *testing.T) {
		rec := newRecordingSend()
		rec.release = make(chan struct{})
		stubTabInputSend(t, rec.send)

		ws := newTestWorkspace("ws", t.TempDir())
		tab := boundInputTab(t, ws, "tab-close", "claude")
		m, _, _ := newActionsModel(t, tab)

		m.admitTabInput(tab, "x", "Input", true)
		<-rec.entered

		closed := make(chan struct{})
		go func() {
			m.Close()
			close(closed)
		}()
		select {
		case <-closed:
			t.Fatal("Close returned before the in-flight send unblocked")
		case <-time.After(100 * time.Millisecond):
		}
		close(rec.release)
		select {
		case <-closed:
		case <-time.After(3 * time.Second):
			t.Fatal("Close did not join the input writer")
		}
	})
}

// ---------------------------------------------------------------------------
// TestTerminalInputCompletion — echo stamping, cursor refresh, initial task
// ---------------------------------------------------------------------------

func TestTerminalInputCompletion(t *testing.T) {
	t.Run("delivery stamps post-delivery local echo", func(t *testing.T) {
		rec := newRecordingSend()
		stubTabInputSend(t, rec.send)
		var sink msgCollector

		ws := newTestWorkspace("ws", t.TempDir())
		tab := boundInputTab(t, ws, "tab-echo", "claude")
		m, _, wsID := newActionsModel(t, tab)
		m.msgSink = sink.sink()

		if res, _ := m.admitTabInput(tab, "x", "Input", true); res != tabInputAdmitted {
			t.Fatalf("request rejected: %v", res)
		}
		// Admission alone must not stamp echo — only completed delivery may.
		tab.mu.Lock()
		stamped := !tab.lastUserInputAt.IsZero()
		tab.mu.Unlock()
		if stamped {
			t.Fatal("echo stamped at admission time")
		}
		testutil.Eventually(t, 3*time.Second, time.Millisecond, func() bool {
			tab.mu.Lock()
			defer tab.mu.Unlock()
			return !tab.lastUserInputAt.IsZero() && !tab.lastPromptInputAt.IsZero()
		}, "delivery did not stamp echo state")

		// A chat tab's completed delivery re-arms the cursor policy with the
		// binding's input generation.
		testutil.Eventually(t, 3*time.Second, time.Millisecond, func() bool {
			return sink.countOf(func(msg tea.Msg) bool {
				r, ok := msg.(PTYCursorRefresh)
				return ok && r.InputGeneration == 1 && r.WorkspaceID == wsID && r.TabID == tab.ID
			}) == 1
		}, "delivery did not emit the gen-stamped cursor refresh")
	})

	t.Run("non-chat delivery stamps echo without a cursor refresh", func(t *testing.T) {
		rec := newRecordingSend()
		stubTabInputSend(t, rec.send)
		var sink msgCollector

		ws := newTestWorkspace("ws", t.TempDir())
		tab := boundInputTab(t, ws, "tab-echo-nc", "bash")
		m, _, _ := newActionsModel(t, tab)
		m.msgSink = sink.sink()

		m.admitTabInput(tab, "x", "Input", true)
		testutil.Eventually(t, 3*time.Second, time.Millisecond, func() bool {
			tab.mu.Lock()
			defer tab.mu.Unlock()
			return !tab.lastUserInputAt.IsZero()
		}, "delivery did not stamp echo")
		if refreshes := sink.countOf(func(msg tea.Msg) bool {
			_, ok := msg.(PTYCursorRefresh)
			return ok
		}); refreshes != 0 {
			t.Fatalf("non-chat delivery emitted %d cursor refreshes", refreshes)
		}
	})

	t.Run("readiness task admits once against its captured generation", func(t *testing.T) {
		rec := newRecordingSend()
		stubTabInputSend(t, rec.send)

		ws := newTestWorkspace("ws", t.TempDir())
		tab := boundInputTab(t, ws, "tab-task", "claude")
		tab.Terminal = vterm.New(80, 24)
		tab.tabInput.gen = 5
		tab.pendingInitialTask = "fix the bug"
		m, _, _ := newActionsModel(t, tab)
		m.msgSink = func(tea.Msg) {}

		// A private-mode set marks the agent's input loop live; the queued
		// task enters the same FIFO as user input, stamped with the binding's
		// generation captured beside it.
		ev := enqueueWriteEvent(t, tab, []byte("\x1b[?1000h"))
		ev.workspaceID = "ws"
		ev.tabID = tab.ID
		m.handleWriteOutput(ev)

		testutil.Eventually(t, 3*time.Second, time.Millisecond, func() bool {
			return rec.delivered() == 1
		}, "task was not delivered after readiness")
		if got := rec.snapshot(); !slices.Equal(got, []string{"fix the bug\r"}) {
			t.Fatalf("delivered = %q, want task+CR", got)
		}

		// A task pinned to a retired generation can never admit — the
		// reattach cannot inherit the old stream's launch task.
		if res, _ := m.admitTabInputBound(tab, "ws", 4, "stale\r", "InitialTask", true); res != tabInputRejectedStale {
			t.Fatalf("stale-gen task admitted as %v", res)
		}
	})
}
