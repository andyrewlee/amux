package sidebar

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/vterm"
)

// terminal_input.go owns the sidebar terminal's input binding: bounded FIFO
// admission on Update plus one writer goroutine that owns SendString. These
// tests prove the four contract points the plan requires — non-blocking
// Update, bounded admission, FIFO ordering, and a gen-fenced
// failure-to-detach — using a stubbed sidebarInputSendFor so the writer's
// PTY write can be blocked, gated, or failed deterministically.

// stubSidebarInputSend swaps the writer's send resolution for the test.
func stubSidebarInputSend(t *testing.T, send func(string) error) {
	t.Helper()
	old := sidebarInputSendFor
	sidebarInputSendFor = func(*TerminalState) func(string) error { return send }
	t.Cleanup(func() { sidebarInputSendFor = old })
}

// inputBoundModel returns a focused model with one live terminal tab whose
// writer sends through send, plus the channel that collects every msgSink
// emission (SidebarInputFailed, toasts).
func inputBoundModel(t *testing.T, send func(string) error) (*TerminalModel, *TerminalState, TerminalTabID, chan tea.Msg) {
	t.Helper()
	stubSidebarInputSend(t, send)
	ws := &data.Workspace{Name: "ws", Repo: "/repo/ws", Root: "/repo/ws"}
	state := &TerminalState{VTerm: vterm.New(80, 24), Terminal: &appPty.Terminal{}, Running: true}
	tab := &TerminalTab{ID: generateTerminalTabID(), Name: "Terminal 1", State: state}
	m := NewTerminalModel()
	m.workspace = ws
	m.focused = true
	m.tabs.ByWorkspace[string(ws.ID())] = []*TerminalTab{tab}
	m.tabs.ActiveByWorkspace[string(ws.ID())] = 0
	sink := make(chan tea.Msg, 64)
	m.SetMsgSink(func(msg tea.Msg) { sink <- msg })
	return m, state, tab.ID, sink
}

// awaitInputFailure waits for the writer's SidebarInputFailed report.
func awaitInputFailure(t *testing.T, sink chan tea.Msg) SidebarInputFailed {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-sink:
			if f, ok := msg.(SidebarInputFailed); ok {
				return f
			}
		case <-deadline:
			t.Fatal("input writer never emitted SidebarInputFailed")
		}
	}
}

// awaitWriterInstall waits until the lazy writer exists on the state.
func awaitWriterInstall(t *testing.T, ts *TerminalState) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ts.mu.Lock()
		installed := ts.input.writer != nil
		ts.mu.Unlock()
		if installed {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("input writer never installed")
}

// TestSidebarInputUpdateDoesNotBlock is the plan's non-blocking proof: a
// send that parks indefinitely must not stall the Update goroutine — the
// handler returns promptly, and the bytes land once the send unblocks.
func TestSidebarInputUpdateDoesNotBlock(t *testing.T) {
	release := make(chan struct{})
	sent := make(chan string, 1)
	send := func(s string) error {
		<-release // park inside the write until the test allows it
		sent <- s
		return nil
	}
	m, _, _, _ := inputBoundModel(t, send)

	done := make(chan struct{})
	go func() {
		m.Update(tea.PasteMsg{Content: "payload"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Update blocked inside a parked SendString — the input queue must admit without waiting on the PTY write")
	}
	close(release)
	select {
	case s := <-sent:
		if !strings.Contains(s, "payload") {
			t.Fatalf("writer delivered %q, want the bracketed paste payload", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("writer never delivered the admitted paste after the send unblocked")
	}
}

// TestSidebarInputQueueByteBound proves admission rejects once queued bytes
// exceed the bound — the writer parks, the queue fills, and the overflow
// paste is refused with a visible rejection signal, never dropped silently.
func TestSidebarInputQueueByteBound(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	send := func(s string) error {
		<-release // park so the queue stays full
		return nil
	}
	m, _, _, sink := inputBoundModel(t, send)

	// Tighten the byte bound so a handful of pastes fills it.
	old := sidebarInputQueueMaxBytes
	sidebarInputQueueMaxBytes = 64
	t.Cleanup(func() { sidebarInputQueueMaxBytes = old })

	payload := strings.Repeat("x", 48) // > bound/2 so two fill, third rejects
	// Paste 1 parks inside send; paste 2 queues (48B); every later paste
	// would push queuedBytes past 64 and must be refused with a toast.
	for i := 0; i < 3; i++ {
		m.Update(tea.PasteMsg{Content: payload})
	}
	select {
	case msg := <-sink:
		toast, ok := msg.(messages.Toast)
		if !ok || toast.Level != messages.ToastWarning {
			t.Fatalf("expected a warning toast for the rejected paste, got %#v", msg)
		}
		if !strings.Contains(toast.Message, "queue full") {
			t.Fatalf("toast %q does not name the queue-full rejection", toast.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("overflow paste produced no rejection signal")
	}
}

// TestSidebarInputFIFODelivery admits several writes and asserts the writer
// delivers them in admission order.
func TestSidebarInputFIFODelivery(t *testing.T) {
	var mu sync.Mutex
	var got []string
	delivered := make(chan struct{}, 16)
	send := func(s string) error {
		mu.Lock()
		got = append(got, s)
		mu.Unlock()
		delivered <- struct{}{}
		return nil
	}
	m, _, _, _ := inputBoundModel(t, send)

	keys := []rune{'a', 'b', 'c', 'd'}
	for _, r := range keys {
		m.Update(tea.KeyPressMsg{Code: r})
	}
	for range keys {
		select {
		case <-delivered:
		case <-time.After(5 * time.Second):
			t.Fatalf("writer delivered %d/%d inputs", len(got), len(keys))
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != len(keys) {
		t.Fatalf("delivered %d writes, want %d", len(got), len(keys))
	}
	for i, r := range keys {
		if got[i] != string(r) {
			t.Fatalf("write %d = %q, want %q — FIFO order broken", i, got[i], string(r))
		}
	}
}

// TestSidebarInputFailureDetaches drives a send failure end to end: the
// writer reports one gen-stamped SidebarInputFailed and the Update handler
// detaches the tab.
func TestSidebarInputFailureDetaches(t *testing.T) {
	m, state, tabID, sink := inputBoundModel(t, func(string) error {
		return errors.New("pty exploded")
	})
	m.Update(tea.KeyPressMsg{Code: 'x'})

	msg := awaitInputFailure(t, sink)
	if msg.TabID != string(tabID) {
		t.Fatalf("failure stamped TabID %q, want %q", msg.TabID, tabID)
	}
	m.Update(msg)

	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.Detached || state.Running || state.UserDetached {
		t.Fatalf("expected non-user detach after input failure, got detached=%v running=%v user=%v",
			state.Detached, state.Running, state.UserDetached)
	}
	if state.Terminal != nil {
		t.Fatal("expected Terminal cleared on detach")
	}
}

// TestSidebarInputStaleFailureDoesNotDetach fences a failure stamped under a
// retired gen: after the binding is replaced, the old writer's late report
// must not detach the replacement.
func TestSidebarInputStaleFailureDoesNotDetach(t *testing.T) {
	m, state, _, _ := inputBoundModel(t, func(string) error { return nil })
	// Install then retire a writer so input.gen advances past 0.
	m.Update(tea.KeyPressMsg{Code: 'x'})
	awaitWriterInstall(t, state)
	state.mu.Lock()
	retired := state.retireSidebarInputWriterLocked()
	state.mu.Unlock()
	joinSidebarInputWriter(retired)

	// A new writer installs on the next admission under the bumped gen.
	m.Update(tea.KeyPressMsg{Code: 'y'})
	awaitWriterInstall(t, state)

	m.Update(SidebarInputFailed{WorkspaceID: m.workspaceID(), TabID: "bogus-gen", Gen: 0, Err: errors.New("stale")})
	// The stale failure's gen (0) is below the live binding's gen after the
	// retire — the handler must drop it.
	state.mu.Lock()
	detached := state.Detached
	state.mu.Unlock()
	if detached {
		t.Fatal("stale input failure detached the replacement binding")
	}
}

// TestSidebarInputReattachClearsFailureLatch proves a failed binding's latch
// does not poison the reattached terminal's admissions.
func TestSidebarInputReattachClearsFailureLatch(t *testing.T) {
	m, state, _, sink := inputBoundModel(t, func(string) error {
		return errors.New("pty exploded")
	})
	m.Update(tea.KeyPressMsg{Code: 'x'})
	msg := awaitInputFailure(t, sink)

	// The latch is set by the writer before the report reaches Update —
	// while it is set, admissions are refused for the dead binding.
	state.mu.Lock()
	failed := state.input.failed
	state.mu.Unlock()
	if !failed {
		t.Fatal("expected the failure latch set by the failing writer")
	}

	m.Update(msg)

	// detachStateForInputGen retired the binding, clearing the latch — a
	// fresh writer must be able to install for a replacement terminal.
	state.mu.Lock()
	if state.input.failed {
		state.mu.Unlock()
		t.Fatal("failure latch survived binding retirement")
	}
	state.mu.Unlock()
}
