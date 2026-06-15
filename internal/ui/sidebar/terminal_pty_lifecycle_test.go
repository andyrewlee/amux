package sidebar

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
)

// terminal_pty_lifecycle.go owns the per-tab PTY lifecycle: state construction,
// reader (re)start, teardown, detach, and direct input. These tests drive the
// pure/in-memory behavior of every function. The PTY-resize side effect is
// captured through the setTerminalSizeFn seam (already present for tests), and
// terminals are zero-value *pty.Terminal values: their Close()/SetSize() are
// no-ops without a live process and Write()/SendString() return io.ErrClosedPipe,
// which lets us exercise both the happy and error paths without ever execing a
// real shell or spawning a read goroutine.
//
// startPTYReader / StartPTYReaders only spawn the real read loop when
// AcquireTerm() returns a non-nil reader (Terminal != nil AND Running). All
// reader tests here keep at least one of those false so StartReader takes its
// synchronous "not readable" branch and never launches a goroutine that would
// busy-spin on a zero-value terminal's io.EOF reads.

// stubTerminalResize swaps setTerminalSizeFn for the duration of a test and
// records the (rows, cols) of each call so size propagation can be asserted.
type resizeRecord struct {
	rows uint16
	cols uint16
}

func stubTerminalResize(t *testing.T) *[]resizeRecord {
	t.Helper()
	old := setTerminalSizeFn
	calls := &[]resizeRecord{}
	setTerminalSizeFn = func(term *pty.Terminal, rows, cols uint16) error {
		*calls = append(*calls, resizeRecord{rows: rows, cols: cols})
		return nil
	}
	t.Cleanup(func() { setTerminalSizeFn = old })
	return calls
}

func newBoundModel(t *testing.T) (*TerminalModel, string) {
	t.Helper()
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo", "/repo/ws")
	m.setWorkspace(ws)
	return m, string(ws.ID())
}

func TestCreateTerminalStateForTab(t *testing.T) {
	tests := []struct {
		name        string
		term        *pty.Terminal
		sessionName string
		wantResize  bool
	}{
		{
			name:        "live terminal resizes and stores session",
			term:        &pty.Terminal{},
			sessionName: "amux-session-1",
			wantResize:  true,
		},
		{
			name:        "nil terminal skips resize",
			term:        nil,
			sessionName: "",
			wantResize:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resizes := stubTerminalResize(t)
			m, wsID := newBoundModel(t)
			// Pre-mark a pending creation so we can prove it is cleared.
			m.pendingCreation[wsID] = true
			tabID := generateTerminalTabID()

			ts := m.createTerminalStateForTab(wsID, tabID, tt.term, tt.sessionName)

			if ts == nil {
				t.Fatal("expected a non-nil terminal state")
			}
			if ts.Terminal != tt.term {
				t.Fatal("expected the supplied terminal stored on the state")
			}
			if ts.VTerm == nil {
				t.Fatal("expected a VTerm allocated for the new tab")
			}
			if !ts.Running {
				t.Fatal("expected a freshly created tab marked Running")
			}
			if ts.Detached {
				t.Fatal("expected a freshly created tab not Detached")
			}
			if ts.SessionName != tt.sessionName {
				t.Fatalf("SessionName = %q, want %q", ts.SessionName, tt.sessionName)
			}
			if ts.lastWidth <= 0 || ts.lastHeight <= 0 {
				t.Fatalf("expected positive default size, got %dx%d", ts.lastWidth, ts.lastHeight)
			}

			// The tab must be installed, named, and selected as active.
			tabs := m.tabs.ByWorkspace[wsID]
			if len(tabs) != 1 {
				t.Fatalf("expected exactly one tab installed, got %d", len(tabs))
			}
			if tabs[0].ID != tabID {
				t.Fatalf("installed tab ID = %q, want %q", tabs[0].ID, tabID)
			}
			if tabs[0].Name != "Terminal 1" {
				t.Fatalf("first tab name = %q, want %q", tabs[0].Name, "Terminal 1")
			}
			if got := m.tabs.ActiveByWorkspace[wsID]; got != 0 {
				t.Fatalf("active index = %d, want 0", got)
			}
			if m.pendingCreation[wsID] {
				t.Fatal("expected pendingCreation cleared after the tab exists")
			}

			gotResize := len(*resizes) > 0
			if gotResize != tt.wantResize {
				t.Fatalf("resize called = %v, want %v (calls=%+v)", gotResize, tt.wantResize, *resizes)
			}
			if tt.wantResize {
				// VTerm is sized HxW; the PTY resize is called rows=height, cols=width.
				last := (*resizes)[len(*resizes)-1]
				if last.rows != uint16(ts.lastHeight) || last.cols != uint16(ts.lastWidth) {
					t.Fatalf("resize args = %dx%d, want rows=%d cols=%d", last.rows, last.cols, ts.lastHeight, ts.lastWidth)
				}
			}
		})
	}
}

func TestCreateTerminalStateForTabNamesSequentially(t *testing.T) {
	stubTerminalResize(t)
	m, wsID := newBoundModel(t)

	first := m.createTerminalStateForTab(wsID, generateTerminalTabID(), &pty.Terminal{}, "s1")
	second := m.createTerminalStateForTab(wsID, generateTerminalTabID(), &pty.Terminal{}, "s2")

	if first == nil || second == nil {
		t.Fatal("expected both states created")
	}
	tabs := m.tabs.ByWorkspace[wsID]
	if len(tabs) != 2 {
		t.Fatalf("expected two tabs, got %d", len(tabs))
	}
	if tabs[0].Name != "Terminal 1" || tabs[1].Name != "Terminal 2" {
		t.Fatalf("expected sequential names, got %q and %q", tabs[0].Name, tabs[1].Name)
	}
	// The second creation must become the active tab.
	if got := m.tabs.ActiveByWorkspace[wsID]; got != 1 {
		t.Fatalf("active index after second create = %d, want 1", got)
	}
}

func TestHandleTerminalCreated(t *testing.T) {
	tests := []struct {
		name    string
		term    *pty.Terminal
		wantCmd bool
	}{
		{
			// Running is true after creation but Terminal is nil, so
			// AcquireTerm() returns nil and startPTYReader returns nil.
			name:    "nil terminal installs tab and returns no reader command",
			term:    nil,
			wantCmd: false,
		},
		{
			// Running && Terminal != nil means AcquireTerm() yields the reader;
			// StartReader spawns the loop and startPTYReader still returns nil.
			name:    "live terminal installs tab and starts reader",
			term:    &pty.Terminal{},
			wantCmd: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubTerminalResize(t)
			m, wsID := newBoundModel(t)
			tabID := generateTerminalTabID()

			cmd := m.HandleTerminalCreated(wsID, tabID, tt.term, "sess")

			if (cmd != nil) != tt.wantCmd {
				t.Fatalf("HandleTerminalCreated cmd!=nil=%v, want %v", cmd != nil, tt.wantCmd)
			}
			tab := m.getTabByID(wsID, tabID)
			if tab == nil || tab.State == nil {
				t.Fatal("expected the tab and its state to be installed")
			}
			if tt.term != nil {
				// A live terminal started the reader; clean it up so the test's
				// read goroutine exits promptly (zero-value terminal Read => EOF).
				m.CloseTerminal(wsID)
			}
		})
	}
}

func TestStartPTYReaders(t *testing.T) {
	stubTerminalResize(t)
	m, wsID := newBoundModel(t)

	// One readable-but-not-running tab (Terminal set, Running false) and one
	// detached tab (Terminal nil). Neither passes AcquireTerm(), so no read
	// goroutine is launched and the call stays synchronous.
	notRunning := &TerminalTab{
		ID:    generateTerminalTabID(),
		Name:  "Terminal 1",
		State: &TerminalState{Terminal: &pty.Terminal{}, Running: false},
	}
	detached := &TerminalTab{
		ID:    generateTerminalTabID(),
		Name:  "Terminal 2",
		State: &TerminalState{Terminal: nil, Running: true},
	}
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{notRunning, detached}
	m.tabs.ActiveByWorkspace[wsID] = 0

	if cmd := m.StartPTYReaders(); cmd != nil {
		t.Fatal("expected StartPTYReaders to return nil")
	}

	// Because neither tab was readable, no reader should be marked active.
	for _, tab := range []*TerminalTab{notRunning, detached} {
		tab.State.mu.Lock()
		active := tab.State.ReaderActive
		tab.State.mu.Unlock()
		if active {
			t.Fatalf("tab %q reader should not be active for an unreadable terminal", tab.Name)
		}
	}
}

func TestStartPTYReadersEmptyIsNoop(t *testing.T) {
	m := NewTerminalModel()
	if cmd := m.StartPTYReaders(); cmd != nil {
		t.Fatal("expected nil command with no workspaces")
	}
}

func TestStartPTYReadersSkipsNilTab(t *testing.T) {
	m, wsID := newBoundModel(t)
	// A nil tab in the slice must be skipped by StartPTYReaders' own guard
	// before it ever dereferences the entry.
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{nil}
	m.tabs.ActiveByWorkspace[wsID] = 0

	if cmd := m.StartPTYReaders(); cmd != nil {
		t.Fatal("expected nil command when the only tab is nil")
	}
}

func TestCloseTerminal(t *testing.T) {
	stubTerminalResize(t)
	m, wsID := newBoundModel(t)
	term := &pty.Terminal{}
	tab := &TerminalTab{
		ID:   generateTerminalTabID(),
		Name: "Terminal 1",
		State: &TerminalState{
			Terminal: term,
			Running:  true,
		},
	}
	// RestartBackoff is a promoted field of the embedded ptyio.State, so it
	// cannot be set in the composite literal above; seed it to prove the close
	// path resets it to zero.
	tab.State.RestartBackoff = 5 * time.Second
	// A second tab with a nil State must be tolerated.
	nilStateTab := &TerminalTab{ID: generateTerminalTabID(), Name: "Terminal 2", State: nil}
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{tab, nilStateTab}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.pendingCreation[wsID] = true

	m.CloseTerminal(wsID)

	if !term.IsClosed() {
		t.Fatal("expected the live terminal closed")
	}
	tab.State.mu.Lock()
	running := tab.State.Running
	backoff := tab.State.RestartBackoff
	tab.State.mu.Unlock()
	if running {
		t.Fatal("expected Running cleared on close")
	}
	if backoff != 0 {
		t.Fatalf("expected RestartBackoff reset to 0, got %d", backoff)
	}
	if _, ok := m.tabs.ByWorkspace[wsID]; ok {
		t.Fatal("expected the workspace tab entry deleted")
	}
	if m.pendingCreation[wsID] {
		t.Fatal("expected pendingCreation cleared")
	}
}

func TestCloseTerminalUnknownWorkspaceIsNoop(t *testing.T) {
	m := NewTerminalModel()
	// No panic and no spurious map entries for an unknown workspace.
	m.CloseTerminal("does-not-exist")
	if len(m.tabs.ByWorkspace) != 0 {
		t.Fatalf("expected no workspace entries, got %d", len(m.tabs.ByWorkspace))
	}
}

func TestCloseAll(t *testing.T) {
	stubTerminalResize(t)
	m := NewTerminalModel()

	wsA := data.NewWorkspace("a", "main", "main", "/repo", "/repo/a")
	wsB := data.NewWorkspace("b", "main", "main", "/repo", "/repo/b")
	termA := &pty.Terminal{}
	termB := &pty.Terminal{}
	m.tabs.ByWorkspace[string(wsA.ID())] = []*TerminalTab{
		{ID: generateTerminalTabID(), Name: "Terminal 1", State: &TerminalState{Terminal: termA, Running: true}},
	}
	m.tabs.ByWorkspace[string(wsB.ID())] = []*TerminalTab{
		{ID: generateTerminalTabID(), Name: "Terminal 1", State: &TerminalState{Terminal: termB, Running: true}},
	}
	m.tabs.ActiveByWorkspace[string(wsA.ID())] = 0
	m.tabs.ActiveByWorkspace[string(wsB.ID())] = 0

	m.CloseAll()

	if len(m.tabs.ByWorkspace) != 0 {
		t.Fatalf("expected all workspaces removed, got %d", len(m.tabs.ByWorkspace))
	}
	if !termA.IsClosed() || !termB.IsClosed() {
		t.Fatal("expected every terminal closed by CloseAll")
	}
}

func TestCloseAllEmptyIsNoop(t *testing.T) {
	m := NewTerminalModel()
	m.CloseAll()
	if len(m.tabs.ByWorkspace) != 0 {
		t.Fatal("expected no workspaces after CloseAll on an empty model")
	}
}

func TestDetachState(t *testing.T) {
	tests := []struct {
		name          string
		userInitiated bool
	}{
		{name: "user-initiated detach", userInitiated: true},
		{name: "implicit detach", userInitiated: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTerminalModel()
			term := &pty.Terminal{}
			ts := &TerminalState{
				Terminal: term,
				Running:  true,
			}
			// PendingOutput/NoiseTrailing are promoted fields of the embedded
			// ptyio.State; seed them so detach's nil-out is observable.
			ts.PendingOutput = []byte("queued")
			ts.NoiseTrailing = []byte("noise")

			m.detachState(ts, tt.userInitiated)

			ts.mu.Lock()
			defer ts.mu.Unlock()
			if ts.Terminal != nil {
				t.Fatal("expected Terminal cleared on detach")
			}
			if ts.Running {
				t.Fatal("expected Running cleared on detach")
			}
			if !ts.Detached {
				t.Fatal("expected Detached set on detach")
			}
			if ts.UserDetached != tt.userInitiated {
				t.Fatalf("UserDetached = %v, want %v", ts.UserDetached, tt.userInitiated)
			}
			if ts.PendingOutput != nil {
				t.Fatal("expected PendingOutput cleared on detach")
			}
			if ts.NoiseTrailing != nil {
				t.Fatal("expected NoiseTrailing cleared on detach")
			}
			if !term.IsClosed() {
				t.Fatal("expected the underlying terminal closed on detach")
			}
		})
	}
}

func TestDetachStateNilIsNoop(t *testing.T) {
	m := NewTerminalModel()
	// Must not panic on a nil state.
	m.detachState(nil, true)
}

func TestDetachStateNilTerminalIsSafe(t *testing.T) {
	m := NewTerminalModel()
	ts := &TerminalState{Terminal: nil, Running: true}
	m.detachState(ts, false)
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !ts.Detached || ts.Running {
		t.Fatal("expected detach flags applied even without a live terminal")
	}
}

func TestSendToTerminal(t *testing.T) {
	stubTerminalResize(t)

	t.Run("no active terminal is a no-op", func(t *testing.T) {
		m, _ := newBoundModel(t)
		// No tabs: getTerminal() is nil; SendToTerminal must early-return.
		m.SendToTerminal("hello")
	})

	t.Run("nil terminal on active tab is a no-op", func(t *testing.T) {
		m, wsID := newBoundModel(t)
		tab := &TerminalTab{ID: generateTerminalTabID(), Name: "Terminal 1", State: &TerminalState{Terminal: nil, Running: true}}
		m.tabs.ByWorkspace[wsID] = []*TerminalTab{tab}
		m.tabs.ActiveByWorkspace[wsID] = 0

		m.SendToTerminal("hello")

		// A nil terminal means the write never happens, so Running stays true.
		tab.State.mu.Lock()
		running := tab.State.Running
		tab.State.mu.Unlock()
		if !running {
			t.Fatal("expected Running untouched when there is no terminal to write to")
		}
	})

	t.Run("write error marks the terminal detached", func(t *testing.T) {
		m, wsID := newBoundModel(t)
		// A zero-value terminal has a nil ptyFile, so SendString returns
		// io.ErrClosedPipe, exercising the failure branch.
		tab := &TerminalTab{
			ID:    generateTerminalTabID(),
			Name:  "Terminal 1",
			State: &TerminalState{Terminal: &pty.Terminal{}, Running: true, UserDetached: true},
		}
		m.tabs.ByWorkspace[wsID] = []*TerminalTab{tab}
		m.tabs.ActiveByWorkspace[wsID] = 0

		m.SendToTerminal("boom")

		tab.State.mu.Lock()
		defer tab.State.mu.Unlock()
		if tab.State.Running {
			t.Fatal("expected Running cleared after a failed send")
		}
		if !tab.State.Detached {
			t.Fatal("expected Detached set after a failed send")
		}
		if tab.State.UserDetached {
			t.Fatal("expected UserDetached cleared (the detach was not user-initiated)")
		}
	})
}
