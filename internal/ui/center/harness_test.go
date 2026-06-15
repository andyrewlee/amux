package center

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/vterm"
)

// TestAddTab_AppendsAndSeedsActiveIndex covers the happy path: a tab with a real
// workspace is appended under its workspace ID, the active index is seeded to 0,
// and the tabs-changed bookkeeping (revision counter) advances.
func TestAddTab_AppendsAndSeedsActiveIndex(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wtID := string(ws.ID())
	tab := &Tab{Assistant: "claude", Workspace: ws}

	beforeRev := m.tabsRevision
	m.AddTab(tab)

	got := m.tabs.ByWorkspace[wtID]
	if len(got) != 1 {
		t.Fatalf("expected exactly one tab stored, got %d", len(got))
	}
	if got[0] != tab {
		t.Fatalf("stored tab pointer = %p, want %p", got[0], tab)
	}
	if idx, ok := m.tabs.ActiveByWorkspace[wtID]; !ok || idx != 0 {
		t.Fatalf("active index = (%d, present=%v), want (0, true)", idx, ok)
	}
	if m.tabsRevision == beforeRev {
		t.Fatalf("expected noteTabsChanged to bump revision from %d", beforeRev)
	}
}

// TestAddTab_NilReceiverInputsAreNoOps verifies every guard clause: a nil model,
// a nil tab, and a tab with a nil workspace must all be inert. The nil-model and
// nil-tab cases must not panic; the nil-workspace case must leave the store empty.
func TestAddTab_NilReceiverInputsAreNoOps(t *testing.T) {
	t.Run("nil model does not panic", func(t *testing.T) {
		var m *Model
		ws := newTestWorkspace("ws", "/repo/ws")
		m.AddTab(&Tab{Workspace: ws}) // must not panic
	})

	t.Run("nil tab does not panic and stores nothing", func(t *testing.T) {
		m := newTestModel()
		m.AddTab(nil)
		for wsID, tabs := range m.tabs.ByWorkspace {
			if len(tabs) != 0 {
				t.Fatalf("workspace %q gained %d tabs from a nil add", wsID, len(tabs))
			}
		}
	})

	t.Run("nil workspace stores nothing", func(t *testing.T) {
		m := newTestModel()
		beforeRev := m.tabsRevision
		m.AddTab(&Tab{Assistant: "claude", Workspace: nil})
		for wsID, tabs := range m.tabs.ByWorkspace {
			if len(tabs) != 0 {
				t.Fatalf("workspace %q gained %d tabs from a nil-workspace add", wsID, len(tabs))
			}
		}
		if m.tabsRevision != beforeRev {
			t.Fatalf("expected no revision bump for nil-workspace add, got %d -> %d", beforeRev, m.tabsRevision)
		}
	})
}

// TestAddTab_LazyInitializesNilMaps exercises the lazy-init branches: a bare
// Model with nil ByWorkspace/ActiveByWorkspace maps must allocate them on first
// add rather than panicking on a nil-map write.
func TestAddTab_LazyInitializesNilMaps(t *testing.T) {
	m := &Model{}
	if m.tabs.ByWorkspace != nil || m.tabs.ActiveByWorkspace != nil {
		t.Fatal("precondition: bare Model should start with nil tab maps")
	}
	ws := newTestWorkspace("ws", "/repo/ws")
	wtID := string(ws.ID())
	tab := &Tab{Assistant: "claude", Workspace: ws}

	m.AddTab(tab)

	if m.tabs.ByWorkspace == nil {
		t.Fatal("expected ByWorkspace map to be lazily allocated")
	}
	if m.tabs.ActiveByWorkspace == nil {
		t.Fatal("expected ActiveByWorkspace map to be lazily allocated")
	}
	if len(m.tabs.ByWorkspace[wtID]) != 1 {
		t.Fatalf("expected tab stored after lazy init, got %d", len(m.tabs.ByWorkspace[wtID]))
	}
	if m.tabs.ActiveByWorkspace[wtID] != 0 {
		t.Fatalf("active index = %d, want 0", m.tabs.ActiveByWorkspace[wtID])
	}
}

// TestAddTab_PreservesExistingActiveIndex confirms that appending a second tab
// keeps an already-chosen active index instead of resetting it to 0. Both tabs
// share a workspace, so the second add must not clobber the selection.
func TestAddTab_PreservesExistingActiveIndex(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wtID := string(ws.ID())

	first := &Tab{Assistant: "claude", Workspace: ws}
	m.AddTab(first)
	// Caller selects the (soon-to-exist) second tab before adding it.
	m.tabs.ActiveByWorkspace[wtID] = 1

	second := &Tab{Assistant: "codex", Workspace: ws}
	m.AddTab(second)

	if got := m.tabs.ByWorkspace[wtID]; len(got) != 2 {
		t.Fatalf("expected two tabs after second add, got %d", len(got))
	}
	if idx := m.tabs.ActiveByWorkspace[wtID]; idx != 1 {
		t.Fatalf("active index = %d, want preserved value 1", idx)
	}
}

// TestAddTab_AppendOrderIsStable checks that successive adds preserve insertion
// order within a workspace and keep distinct workspaces isolated from each other.
func TestAddTab_AppendOrderIsStable(t *testing.T) {
	m := newTestModel()
	ws1 := newTestWorkspace("ws1", "/repo/ws1")
	ws2 := newTestWorkspace("ws2", "/repo/ws2")

	a := &Tab{Assistant: "claude", Workspace: ws1}
	b := &Tab{Assistant: "codex", Workspace: ws1}
	c := &Tab{Assistant: "claude", Workspace: ws2}

	m.AddTab(a)
	m.AddTab(b)
	m.AddTab(c)

	gotWS1 := m.tabs.ByWorkspace[string(ws1.ID())]
	if len(gotWS1) != 2 || gotWS1[0] != a || gotWS1[1] != b {
		t.Fatalf("ws1 tabs out of order: %v", gotWS1)
	}
	gotWS2 := m.tabs.ByWorkspace[string(ws2.ID())]
	if len(gotWS2) != 1 || gotWS2[0] != c {
		t.Fatalf("ws2 tabs incorrect: %v", gotWS2)
	}
}

// TestAddTab_ClearsIgnoreCursorVisibilityControls verifies the documented side
// effect: when the tab carries a terminal, AddTab forces
// IgnoreCursorVisibilityControls to false regardless of its prior value.
func TestAddTab_ClearsIgnoreCursorVisibilityControls(t *testing.T) {
	tests := []struct {
		name    string
		initial bool
	}{
		{name: "starts true is cleared", initial: true},
		{name: "starts false stays false", initial: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			ws := newTestWorkspace("ws", "/repo/ws")
			term := vterm.New(20, 4)
			term.IgnoreCursorVisibilityControls = tt.initial
			tab := &Tab{Assistant: "claude", Workspace: ws, Terminal: term}

			m.AddTab(tab)

			if term.IgnoreCursorVisibilityControls {
				t.Fatalf("expected IgnoreCursorVisibilityControls cleared, still true (initial=%v)", tt.initial)
			}
		})
	}
}

// TestAddTab_NilTerminalDoesNotPanic guards the branch where Terminal is nil: the
// terminal-side effect must be skipped without dereferencing a nil pointer.
func TestAddTab_NilTerminalDoesNotPanic(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := &Tab{Assistant: "claude", Workspace: ws, Terminal: nil}

	m.AddTab(tab) // must not panic

	if len(m.tabs.ByWorkspace[string(ws.ID())]) != 1 {
		t.Fatal("expected nil-terminal tab to still be stored")
	}
}

// TestWriteToTerminal_RendersWrittenBytes covers the happy path: bytes handed to
// WriteToTerminal are parsed by the underlying VTerm and become visible in the
// rendered screen.
func TestWriteToTerminal_RendersWrittenBytes(t *testing.T) {
	tests := []struct {
		name  string
		write []byte
		want  string
	}{
		{name: "ascii word", write: []byte("hello"), want: "hello"},
		{name: "digits", write: []byte("12345"), want: "12345"},
		{name: "with trailing newline", write: []byte("abc\n"), want: "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tab := &Tab{Terminal: vterm.New(20, 4)}

			tab.WriteToTerminal(tt.write)

			if rendered := tab.Terminal.Render(); !strings.Contains(rendered, tt.want) {
				t.Fatalf("rendered screen %q does not contain %q", rendered, tt.want)
			}
		})
	}
}

// TestWriteToTerminal_Appends confirms successive writes accumulate on the same
// line rather than replacing prior content.
func TestWriteToTerminal_Appends(t *testing.T) {
	tab := &Tab{Terminal: vterm.New(20, 4)}

	tab.WriteToTerminal([]byte("foo"))
	tab.WriteToTerminal([]byte("bar"))

	if rendered := tab.Terminal.Render(); !strings.Contains(rendered, "foobar") {
		t.Fatalf("expected accumulated output 'foobar', got %q", rendered)
	}
}

// TestWriteToTerminal_EmptyAndNilDataAreInert verifies boundary inputs: writing
// empty or nil data must not panic and must leave the screen blank.
func TestWriteToTerminal_EmptyAndNilDataAreInert(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "nil slice", data: nil},
		{name: "empty slice", data: []byte{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A fresh terminal of identical dimensions is the reference: an inert
			// write must leave the screen byte-for-byte identical to never having
			// written at all.
			want := vterm.New(20, 4).Render()
			tab := &Tab{Terminal: vterm.New(20, 4)}

			tab.WriteToTerminal(tt.data)

			if got := tab.Terminal.Render(); got != want {
				t.Fatalf("expected screen unchanged after inert write\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

// TestWriteToTerminal_NilReceiverAndNilTerminal exercises the two early-return
// guards: a nil tab and a tab with no terminal must both be no-ops that never
// panic.
func TestWriteToTerminal_NilReceiverAndNilTerminal(t *testing.T) {
	t.Run("nil tab", func(t *testing.T) {
		var tab *Tab
		tab.WriteToTerminal([]byte("data")) // must not panic
	})

	t.Run("nil terminal", func(t *testing.T) {
		tab := &Tab{Terminal: nil}
		tab.WriteToTerminal([]byte("data")) // must not panic
		if tab.Terminal != nil {
			t.Fatal("expected terminal to remain nil")
		}
	})
}

// TestWriteToTerminal_HandlesControlSequences feeds an ANSI clear-screen escape
// followed by text to confirm the bytes route through the real parser (not a raw
// byte append) and the post-clear text is what survives on screen.
func TestWriteToTerminal_HandlesControlSequences(t *testing.T) {
	tab := &Tab{Terminal: vterm.New(20, 4)}

	tab.WriteToTerminal([]byte("stale"))
	// CSI 2J clears the screen; the cursor home + new text should be all that
	// renders afterward.
	tab.WriteToTerminal([]byte("\x1b[2J\x1b[Hfresh"))

	rendered := tab.Terminal.Render()
	if !strings.Contains(rendered, "fresh") {
		t.Fatalf("expected post-clear text 'fresh', got %q", rendered)
	}
	if strings.Contains(rendered, "stale") {
		t.Fatalf("expected cleared text 'stale' to be gone, got %q", rendered)
	}
}
