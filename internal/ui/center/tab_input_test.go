package center

import (
	"errors"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/testutil"
)

// ---------------------------------------------------------------------------
// input-writer test seams
// ---------------------------------------------------------------------------

// recordingSend is a deterministic replacement for Terminal.SendString wired
// through the tabInputSendFor seam. When release is non-nil every send blocks
// until it is closed, which lets a test hold the writer mid-delivery while it
// queues more input or drives a lifecycle transition.
type recordingSend struct {
	mu      sync.Mutex
	got     []string
	release chan struct{}
	entered chan struct{} // closed when the first send is entered
	err     error         // returned by every send once released
}

func newRecordingSend() *recordingSend {
	return &recordingSend{entered: make(chan struct{})}
}

func (r *recordingSend) send(data string) error {
	select {
	case <-r.entered:
	default:
		close(r.entered)
	}
	if r.release != nil {
		<-r.release
	}
	r.mu.Lock()
	r.got = append(r.got, data)
	err := r.err
	r.mu.Unlock()
	return err
}

func (r *recordingSend) delivered() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func (r *recordingSend) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.got...)
}

// stubTabInputSend swaps the send resolution used by lazily created input
// writers; production resolves to the binding terminal's SendString.
func stubTabInputSend(t *testing.T, send func(string) error) {
	t.Helper()
	prev := tabInputSendFor
	tabInputSendFor = func(*Tab) func(string) error { return send }
	t.Cleanup(func() { tabInputSendFor = prev })
}

// stubTabInputQueue shrinks the admission bounds so saturation tests need only
// a handful of requests instead of 4096/4MiB.
func stubTabInputQueue(t *testing.T, requests, maxBytes int) {
	t.Helper()
	prevReqs, prevBytes := tabInputQueueRequests, tabInputQueueMaxBytes
	tabInputQueueRequests, tabInputQueueMaxBytes = requests, maxBytes
	t.Cleanup(func() {
		tabInputQueueRequests, tabInputQueueMaxBytes = prevReqs, prevBytes
	})
}

// reapInputWriters retires whatever writer the tab's binding still owns and
// joins it. Registered as test cleanup by the tab builders so an admitted
// writer never outlives the test; every blocked-send test releases its send
// before returning, so the join cannot hang.
func reapInputWriters(tab *Tab) {
	tab.mu.Lock()
	tab.retireTabInputWriterLocked()
	tab.mu.Unlock()
	tab.joinRetiredInputWriters()
}

// boundInputTab builds a running tab with a live terminal and a nonzero input
// generation — what a production binding looks like after attach.
func boundInputTab(t *testing.T, ws *data.Workspace, id, assistant string) *Tab {
	t.Helper()
	term, err := appPty.NewWithSize("cat >/dev/null", t.TempDir(), nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	tab := &Tab{
		ID:        TabID(id),
		Name:      id,
		Assistant: assistant,
		Workspace: ws,
		Agent:     &appPty.Agent{Terminal: term},
		Running:   true,
		tabInput:  tabInputState{gen: 1},
	}
	t.Cleanup(func() {
		reapInputWriters(tab)
		_ = term.Close()
	})
	return tab
}

// newClosedPTYTab spawns a real PTY-backed terminal and closes it so the next
// SendString fails deterministically (a closed terminal's write returns
// io.ErrClosedPipe). The gen parameter installs a binding generation so the
// failure carries a stamped, non-sentinel gen.
func newClosedPTYTab(t *testing.T, id TabID, gen uint64) *Tab {
	t.Helper()
	term, err := appPty.NewWithSize("cat >/dev/null", t.TempDir(), nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("close terminal: %v", err)
	}
	tab := &Tab{
		ID:        id,
		Assistant: "bash",
		Agent:     &appPty.Agent{Terminal: term},
		Running:   true,
		tabInput:  tabInputState{gen: gen},
	}
	t.Cleanup(func() { reapInputWriters(tab) })
	return tab
}

// awaitInputWriter polls until the tab's binding has a live writer.
func awaitInputWriter(t *testing.T, tab *Tab) *tabInputWriter {
	t.Helper()
	var w *tabInputWriter
	testutil.Eventually(t, 3*time.Second, time.Millisecond, func() bool {
		tab.mu.Lock()
		defer tab.mu.Unlock()
		w = tab.tabInput.writer
		return w != nil
	}, "input writer was not created")
	return w
}

// awaitWriterExit asserts the writer goroutine exits within the deadline —
// the bounded wait every lifecycle join must satisfy.
func awaitWriterExit(t *testing.T, w *tabInputWriter) {
	t.Helper()
	select {
	case <-w.done:
	case <-time.After(3 * time.Second):
		t.Fatal("input writer did not exit")
	}
}

// msgCollector makes a goroutine-safe msgSink: the input writer reports
// completions/failures off the update loop.
type msgCollector struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (c *msgCollector) sink() func(tea.Msg) {
	return func(msg tea.Msg) {
		c.mu.Lock()
		c.msgs = append(c.msgs, msg)
		c.mu.Unlock()
	}
}

func (c *msgCollector) snapshot() []tea.Msg {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]tea.Msg(nil), c.msgs...)
}

func (c *msgCollector) countOf(pred func(tea.Msg) bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, msg := range c.msgs {
		if pred(msg) {
			n++
		}
	}
	return n
}

func tabFlags(tab *Tab) (running, detached bool) {
	tab.mu.Lock()
	defer tab.mu.Unlock()
	return tab.Running, tab.Detached
}

func readPTYUntil(t *testing.T, term *appPty.Terminal, want string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var buf strings.Builder
	tmp := make([]byte, 256)
	for time.Now().Before(deadline) {
		_ = term.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := term.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
			if strings.Contains(buf.String(), want) {
				return buf.String()
			}
		}
		if err != nil && n == 0 {
			var ne net.Error
			if !errors.As(err, &ne) || !ne.Timeout() {
				t.Fatalf("pty read failed waiting for %q: %v (read %q)", want, err, buf.String())
			}
		}
	}
	t.Fatalf("timed out waiting for %q; read %q", want, buf.String())
	return ""
}

// ---------------------------------------------------------------------------
// TestTerminalInputFIFO — admission-order delivery through the real producers
// ---------------------------------------------------------------------------

// wantMixedInput is the exact byte sequence the mixed-producer drive must
// deliver: text → bracketed paste → Escape → literal NUL → carriage return.
var wantMixedInput = []string{
	"x",
	ansi.BracketedPasteStart + "pasted" + ansi.BracketedPasteEnd,
	"\x1b",
	"\x00",
	"\r",
}

func TestTerminalInputFIFO(t *testing.T) {
	scenarios := []struct {
		name    string
		prepare func(t *testing.T, m *Model)
	}{
		{
			name:    "output actor not started",
			prepare: func(*testing.T, *Model) {},
		},
		{
			name: "output actor queue full",
			prepare: func(t *testing.T, m *Model) {
				t.Helper()
				m.tabEvents = make(chan tabEvent, 4)
				m.setTabActorReady()
				fillTabEvents(m.tabEvents, cap(m.tabEvents))
			},
		},
		{
			name: "output actor stalled",
			prepare: func(t *testing.T, m *Model) {
				t.Helper()
				atomic.StoreInt64(&m.tabActorHeartbeat, time.Now().Add(-tabActorStallTimeout-time.Second).UnixNano())
				atomic.StoreUint32(&m.tabActorReady, 1)
			},
		},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			rec := newRecordingSend()
			rec.release = make(chan struct{})
			stubTabInputSend(t, rec.send)

			ws := newTestWorkspace("ws", t.TempDir())
			tab := boundInputTab(t, ws, "tab-fifo", "claude")
			m, _, _ := newActionsModel(t, tab)
			m.focused = true
			sc.prepare(t, m)

			// The first admission's send blocks; everything after it must
			// queue behind it in admission order — never overtake it.
			m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
			<-rec.entered

			m.Update(tea.PasteMsg{Content: "pasted"})
			m.Update(tea.KeyPressMsg{Code: '[', Mod: tea.ModCtrl})
			_ = m.SendToTerminal("\x00")
			m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

			// A blocked send must not hold tab.mu — the update loop would
			// otherwise serialize behind a stuck PTY.
			if !tab.mu.TryLock() {
				t.Fatal("tab.mu held while a send was blocked")
			}
			tab.mu.Unlock()

			close(rec.release)
			testutil.Eventually(t, 3*time.Second, time.Millisecond, func() bool {
				return rec.delivered() == len(wantMixedInput)
			}, "writer did not drain the queue")
			if got := rec.snapshot(); !slices.Equal(got, wantMixedInput) {
				t.Fatalf("delivery order = %q, want %q", got, wantMixedInput)
			}
		})
	}

	// The step-1 regression: an older request admitted before a newer Enter
	// must land first even while the writer is mid-send. The mixed drive above
	// proves the full sequence; this isolates the text-then-Enter pair that the
	// removed direct-send fallback used to reorder.
	t.Run("older input cannot be overtaken by newer Enter", func(t *testing.T) {
		rec := newRecordingSend()
		rec.release = make(chan struct{})
		stubTabInputSend(t, rec.send)

		ws := newTestWorkspace("ws", t.TempDir())
		tab := boundInputTab(t, ws, "tab-order", "claude")
		m, _, _ := newActionsModel(t, tab)
		m.focused = true

		m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
		<-rec.entered
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

		close(rec.release)
		testutil.Eventually(t, 3*time.Second, time.Millisecond, func() bool {
			return rec.delivered() == 2
		}, "writer did not drain")
		if got := rec.snapshot(); !slices.Equal(got, []string{"o", "\r"}) {
			t.Fatalf("delivery order = %q, want %q", got, []string{"o", "\r"})
		}
	})

	// Real bytes through a real raw-mode PTY: the child echoes what it
	// received back to the master, so reading "ab\r" proves the bytes arrived
	// intact and in order.
	t.Run("real raw PTY delivers ordered bytes", func(t *testing.T) {
		dir := t.TempDir()
		term, err := appPty.NewWithSize("stty raw -echo; printf READY; cat", dir, nil, 24, 80)
		if err != nil {
			t.Fatalf("expected test PTY terminal: %v", err)
		}
		defer func() { _ = term.Close() }()
		readPTYUntil(t, term, "READY")

		ws := newTestWorkspace("ws", dir)
		tab := &Tab{
			ID:        TabID("tab-raw"),
			Assistant: "claude",
			Workspace: ws,
			Agent:     &appPty.Agent{Terminal: term},
			Running:   true,
			tabInput:  tabInputState{gen: 1},
		}
		m, _, _ := newActionsModel(t, tab)
		m.focused = true

		m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

		readPTYUntil(t, term, "ab\r")
		reapInputWriters(tab)
	})
}
