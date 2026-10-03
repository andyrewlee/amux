package sidebar

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/vterm"
)

// installTestWriter installs a test-owned writer stream on ts and starts its
// worker — the stream a test can arm with a pre-lock pause before any work
// exists (the plan's "tiny private worker test hook on a test-owned writer
// instance"). preLock is assigned before the worker starts, so handing it to
// the goroutine is race-free. The returned stream is joined by the caller or
// by the registered cleanup.
func installTestWriter(t *testing.T, m *TerminalModel, ts *TerminalState, preLock func()) *sidebarWriterStream {
	t.Helper()
	ts.mu.Lock()
	ts.writeEpoch++
	w := &sidebarWriterStream{
		wake:        make(chan struct{}, 1),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
		epoch:       ts.writeEpoch,
		testPreLock: preLock,
	}
	ts.writer = w
	ts.mu.Unlock()
	safego.Go("test.sidebar_writer", func() { m.runSidebarWriter(ts, w) })
	t.Cleanup(func() {
		ts.mu.Lock()
		retired := ts.stopSidebarWriterLocked()
		ts.mu.Unlock()
		joinSidebarWriter(retired)
	})
	return w
}

// pauseOnceHook builds the preLock barrier used by the integrity tests: the
// first worker iteration closes entered and blocks until release closes;
// later iterations run free — the barrier must sit outside the mutex, which
// is exactly where the hook runs.
func pauseOnceHook() (hook func(), entered, release chan struct{}) {
	entered = make(chan struct{})
	release = make(chan struct{})
	var once sync.Once
	return func() {
		once.Do(func() {
			close(entered)
			<-release
		})
	}, entered, release
}

func renderSidebarScreen(ts *TerminalState) string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.VTerm.Render()
}

// ---------------------------------------------------------------------------
// TestSidebarWriteIntegrity — dequeue+apply atomicity and stream fencing
// ---------------------------------------------------------------------------

func TestSidebarWriteIntegrity(t *testing.T) {
	t.Run("fallback drain cannot overtake the queued seed", func(t *testing.T) {
		m := NewTerminalModel()
		m.SetMsgSink(func(tea.Msg) {})
		ts := &TerminalState{VTerm: vterm.New(60, 20), Running: true}
		m.tabs.ByWorkspace["ws"] = []*TerminalTab{{ID: "t", State: ts}}
		hook, entered, release := pauseOnceHook()
		installTestWriter(t, m, ts, hook)

		// The seed is admitted while the worker is paused before ts.mu. In
		// the removed design the request was already dequeued there; now it
		// still sits in the stream queue.
		if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("SEED"), workspaceID: "ws", tabID: "t"}) {
			t.Fatal("seed enqueue fell back")
		}
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("worker never reached the pre-lock pause")
		}

		// On the UI side, hold ts.mu (the worker cannot have dequeued — the
		// lock is the dequeue boundary) and fill the queue to capacity.
		ts.mu.Lock()
		accepted := 1
		for i := 0; i < sidebarWriteQueueCap*2; i++ {
			req := sidebarWriteReq{chunk: []byte{'q', byte('0' + i), '\r', '\n'}, workspaceID: "ws", tabID: "t"}
			if !m.enqueueSidebarWriteLocked(ts, req) {
				break
			}
			accepted++
		}
		if accepted != sidebarWriteQueueCap {
			t.Fatalf("queue accepted %d requests, want exactly cap %d", accepted, sidebarWriteQueueCap)
		}
		// The rejected newest request goes through the synchronous fallback:
		// every older queued request applies first, then it does — all under
		// the same hold the paused worker is waiting for.
		failed := sidebarWriteReq{chunk: []byte("FAILED"), workspaceID: "ws", tabID: "t"}
		drainSidebarWriteQueueLocked(ts, failed)
		ts.mu.Unlock()

		close(release)
		screen := renderSidebarScreen(ts)

		// Exact sequence: SEED, the seven accepted requests, FAILED.
		want := []string{"SEED", "q0", "q1", "q2", "q3", "q4", "q5", "q6", "FAILED"}
		prev := -1
		for _, marker := range want {
			idx := strings.Index(screen, marker)
			if idx < 0 {
				t.Fatalf("%q missing from screen %q", marker, screen)
			}
			if idx <= prev {
				t.Fatalf("%q out of order in screen %q", marker, screen)
			}
			prev = idx
		}
	})

	t.Run("split UTF-8 fragments stay ordered across the fallback", func(t *testing.T) {
		m := NewTerminalModel()
		m.SetMsgSink(func(tea.Msg) {})
		ts := &TerminalState{VTerm: vterm.New(60, 20), Running: true}
		m.tabs.ByWorkspace["ws"] = []*TerminalTab{{ID: "t", State: ts}}
		hook, entered, release := pauseOnceHook()
		installTestWriter(t, m, ts, hook)

		// "€" is e2 82 ac. Every request completes the previous request's €,
		// writes a position marker, and opens the next € — a reorder either
		// corrupts the encodings or scrambles the marker letters.
		if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte{'\xe2'}, workspaceID: "ws", tabID: "t"}) {
			t.Fatal("seed fell back")
		}
		<-entered

		ts.mu.Lock()
		for i := 0; i < sidebarWriteQueueCap-1; i++ {
			req := sidebarWriteReq{chunk: []byte{'\x82', '\xac', byte('a' + i), '\xe2'}, workspaceID: "ws", tabID: "t"}
			if !m.enqueueSidebarWriteLocked(ts, req) {
				t.Fatalf("request %d rejected before cap", i)
			}
		}
		failed := sidebarWriteReq{chunk: []byte{'\x82', '\xac', '!'}, workspaceID: "ws", tabID: "t"}
		drainSidebarWriteQueueLocked(ts, failed)
		ts.mu.Unlock()
		close(release)

		screen := renderSidebarScreen(ts)
		if got := strings.Count(screen, "€"); got != sidebarWriteQueueCap {
			t.Fatalf("screen has %d € runes, want %d (a reorder would corrupt the chained UTF-8)", got, sidebarWriteQueueCap)
		}
		prev := -1
		for i := 0; i < sidebarWriteQueueCap-1; i++ {
			idx := strings.Index(screen, string(rune('a'+i)))
			if idx < 0 || idx <= prev {
				t.Fatalf("marker %q missing or out of order in screen %q", string(rune('a'+i)), screen)
			}
			prev = idx
		}
		if !strings.Contains(screen, "!") {
			t.Fatalf("failed-enqueue tail missing from screen %q", screen)
		}
	})

	t.Run("retired worker cannot apply after restore", func(t *testing.T) {
		m := NewTerminalModel()
		m.SetMsgSink(func(tea.Msg) {})
		ts := &TerminalState{VTerm: vterm.New(60, 20), Running: true}
		m.tabs.ByWorkspace["ws"] = []*TerminalTab{{ID: "t", State: ts}}
		hook, entered, release := pauseOnceHook()
		installTestWriter(t, m, ts, hook)

		if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("OLD-STREAM-BYTES"), workspaceID: "ws", tabID: "t"}) {
			t.Fatal("seed fell back")
		}
		<-entered

		// The lifecycle boundary the accepted-reattach path performs: the old
		// stream stops and the epoch fences before the restored VTerm mutates.
		ts.mu.Lock()
		retired := ts.stopSidebarWriterLocked()
		ts.VTerm = vterm.New(60, 20)
		ts.VTerm.Write([]byte("restored-pane"))
		ts.mu.Unlock()

		close(release)
		joinSidebarWriter(retired)

		screen := renderSidebarScreen(ts)
		if strings.Contains(screen, "OLD-STREAM-BYTES") {
			t.Fatalf("retired stream applied after restore: %q", screen)
		}
		if !strings.Contains(screen, "restored-pane") {
			t.Fatalf("restored content missing: %q", screen)
		}
	})

	t.Run("queued requests die with the stream", func(t *testing.T) {
		m := NewTerminalModel()
		m.SetMsgSink(func(tea.Msg) {})
		ts := &TerminalState{VTerm: vterm.New(60, 20), Running: true}
		m.tabs.ByWorkspace["ws"] = []*TerminalTab{{ID: "t", State: ts}}
		hook, entered, release := pauseOnceHook()
		installTestWriter(t, m, ts, hook)

		for _, s := range []string{"one", "two", "three"} {
			if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte(s), workspaceID: "ws", tabID: "t"}) {
				t.Fatalf("enqueue %q fell back", s)
			}
		}
		<-entered

		ts.mu.Lock()
		retired := ts.stopSidebarWriterLocked()
		ts.mu.Unlock()
		close(release)
		joinSidebarWriter(retired)

		screen := renderSidebarScreen(ts)
		for _, s := range []string{"one", "two", "three"} {
			if strings.Contains(screen, s) {
				t.Fatalf("queued request %q applied after stream stop: %q", s, screen)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestSidebarWriterEpoch — completion fencing, joins, stream restarts
// ---------------------------------------------------------------------------

func TestSidebarWriterEpoch(t *testing.T) {
	// stubClipboard swaps the OS-clipboard seam for a recorder and returns a
	// snapshot accessor — the drain is safego'd, so reads must synchronize.
	stubClipboard := func(t *testing.T) func() []string {
		t.Helper()
		t.Setenv("AMUX_ENABLE_OSC52_CLIPBOARD", "1")
		var mu sync.Mutex
		var copied []string
		prev := sidebarClipboardCopy
		sidebarClipboardCopy = func(text, _ string) {
			mu.Lock()
			copied = append(copied, text)
			mu.Unlock()
		}
		t.Cleanup(func() { sidebarClipboardCopy = prev })
		return func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), copied...)
		}
	}

	t.Run("stale completion has no clipboard side effects", func(t *testing.T) {
		m := NewTerminalModel()
		ts := &TerminalState{VTerm: vterm.New(40, 10), Running: true}
		ts.writeEpoch = 7
		tabID := TerminalTabID("t")
		m.tabs.ByWorkspace["ws"] = []*TerminalTab{{ID: tabID, State: ts}}

		copied := stubClipboard(t)

		// A completion stamped with a replaced epoch is dropped — even though
		// its clip payload is a valid OSC52 capture.
		m.Update(SidebarTabWritten{WorkspaceID: "ws", TabID: string(tabID), clip: []byte("stale"), epoch: 6})
		time.Sleep(100 * time.Millisecond)
		if got := copied(); len(got) != 0 {
			t.Fatalf("stale completion copied %v", got)
		}

		// Zero epoch is the untagged sentinel — never accepted.
		m.Update(SidebarTabWritten{WorkspaceID: "ws", TabID: string(tabID), clip: []byte("untagged"), epoch: 0})
		time.Sleep(100 * time.Millisecond)
		if got := copied(); len(got) != 0 {
			t.Fatalf("untagged completion copied %v", got)
		}

		// A completion from the current stream drains normally.
		m.Update(SidebarTabWritten{WorkspaceID: "ws", TabID: string(tabID), clip: []byte("fresh"), epoch: 7})
		testutil.Eventually(t, 2*time.Second, 5*time.Millisecond, func() bool {
			got := copied()
			return len(got) == 1 && got[0] == "fresh"
		}, "current completion did not reach the clipboard drain")
	})

	t.Run("workspace rebind still routes a current completion", func(t *testing.T) {
		m := NewTerminalModel()
		ts := &TerminalState{VTerm: vterm.New(40, 10), Running: true}
		ts.writeEpoch = 3
		tabID := TerminalTabID("t")
		// The tab migrated keys but kept its stream — a completion stamped
		// with the old route resolves via fallback and its epoch still
		// matches.
		m.tabs.ByWorkspace["ws-new"] = []*TerminalTab{{ID: tabID, State: ts}}

		copied := stubClipboard(t)
		m.Update(SidebarTabWritten{WorkspaceID: "ws-old", TabID: string(tabID), clip: []byte("rebound"), epoch: 3})
		testutil.Eventually(t, 2*time.Second, 5*time.Millisecond, func() bool {
			got := copied()
			return len(got) == 1 && got[0] == "rebound"
		}, "rebound completion was not routed")
	})

	t.Run("repeated stop/start joins every worker and bumps the epoch", func(t *testing.T) {
		m := NewTerminalModel()
		m.SetMsgSink(func(tea.Msg) {})
		ts := &TerminalState{VTerm: vterm.New(40, 10), Running: true}
		m.tabs.ByWorkspace["ws"] = []*TerminalTab{{ID: "t", State: ts}}

		var lastEpoch uint64
		for i := 0; i < 5; i++ {
			if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("x"), workspaceID: "ws", tabID: "t"}) {
				t.Fatalf("cycle %d: enqueue fell back", i)
			}
			ts.mu.Lock()
			w := ts.writer
			epoch := ts.writeEpoch
			ts.mu.Unlock()
			if w == nil {
				t.Fatalf("cycle %d: no writer installed", i)
			}
			if epoch <= lastEpoch {
				t.Fatalf("cycle %d: epoch %d did not advance past %d", i, epoch, lastEpoch)
			}
			lastEpoch = epoch

			ts.mu.Lock()
			retired := ts.stopSidebarWriterLocked()
			ts.mu.Unlock()
			joinSidebarWriter(retired)
			select {
			case <-retired.done:
			default:
				t.Fatalf("cycle %d: retired worker's done channel is not closed", i)
			}
		}

		// A sixth install still delivers — no orphaned worker state.
		if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("live"), workspaceID: "ws", tabID: "t"}) {
			t.Fatal("post-cycling enqueue fell back")
		}
		testutil.Eventually(t, 2*time.Second, 5*time.Millisecond, func() bool {
			return strings.Contains(renderSidebarScreen(ts), "live")
		}, "fresh stream did not deliver after stop/start cycles")
	})
}

// ---------------------------------------------------------------------------
// TestSidebarWriterPendingPublication — the published pending-byte count
// ---------------------------------------------------------------------------

func TestSidebarWriterPendingPublication(t *testing.T) {
	t.Run("published count gates the idle noise release", func(t *testing.T) {
		m := NewTerminalModel()
		m.SetMsgSink(func(tea.Msg) {})
		ts := &TerminalState{VTerm: vterm.New(60, 20), Running: true}
		m.tabs.ByWorkspace["ws"] = []*TerminalTab{{ID: "t", State: ts}}
		installTestWriter(t, m, ts, nil)

		// A positive published count while the buffer is physically empty is
		// the reservation window: the worker must consult the published count
		// (not PendingOutput) and hold the trailing fragment.
		ts.mu.Lock()
		ts.pendingBufferedBytes = 4
		ts.mu.Unlock()
		if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("ok\nagent(42) malloc: nano"), workspaceID: "ws", tabID: "t"}) {
			t.Fatal("first chunk fell back")
		}
		testutil.Eventually(t, 2*time.Second, 5*time.Millisecond, func() bool {
			return strings.Contains(renderSidebarScreen(ts), "ok")
		}, "first chunk never applied")
		ts.mu.Lock()
		held := len(ts.NoiseTrailing) > 0
		ts.mu.Unlock()
		if !held {
			t.Fatal("published pending bytes did not hold the split diagnostic fragment")
		}
		if strings.Contains(renderSidebarScreen(ts), "malloc") {
			t.Fatal("held fragment leaked to the screen while more work was published")
		}

		// Complete the diagnostic on the next apply: the whole line is
		// filtered, and with nothing published behind it the boundary is idle.
		ts.mu.Lock()
		ts.pendingBufferedBytes = 0
		ts.mu.Unlock()
		if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte(" zone abandoned\ndone\n"), workspaceID: "ws", tabID: "t"}) {
			t.Fatal("completing chunk fell back")
		}
		testutil.Eventually(t, 2*time.Second, 5*time.Millisecond, func() bool {
			return strings.Contains(renderSidebarScreen(ts), "done")
		}, "completing chunk never applied")
		screen := renderSidebarScreen(ts)
		if strings.Contains(screen, "nano zone abandoned") || strings.Contains(screen, "malloc") {
			t.Fatalf("completed diagnostic line leaked to the screen: %q", screen)
		}

		// A prompt-looking tail ("Retry(2)" matches the diagnostic shape but
		// ends the stream) is held by the filter, then released by the same
		// apply because the queue and the published count are both empty.
		if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("Retry(2)"), workspaceID: "ws", tabID: "t"}) {
			t.Fatal("prompt tail fell back")
		}
		testutil.Eventually(t, 2*time.Second, 5*time.Millisecond, func() bool {
			return strings.Contains(renderSidebarScreen(ts), "Retry(2)")
		}, "idle-boundary release never showed the prompt tail")
	})
}
