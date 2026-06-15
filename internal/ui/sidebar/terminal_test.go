package sidebar

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

// newWorkspaceTab builds an in-memory terminal tab with a VTerm so the
// cursor-visibility and navigation paths run without a live PTY/tmux session.
// Keeping width/height at zero means refreshTerminalSize is a no-op, so none of
// the exercised methods exec an external process.
func newWorkspaceTab(t *testing.T, name string) *TerminalTab {
	t.Helper()
	return &TerminalTab{
		ID:    generateTerminalTabID(),
		Name:  name,
		State: &TerminalState{VTerm: vterm.New(10, 3)},
	}
}

// seedTabs installs the given tabs for the model's current workspace and marks
// the first tab active.
func seedTabs(t *testing.T, m *TerminalModel, tabs ...*TerminalTab) {
	t.Helper()
	wsID := m.workspaceID()
	m.tabs.ByWorkspace[wsID] = tabs
	m.tabs.ActiveByWorkspace[wsID] = 0
}

func TestSetTmuxOptions(t *testing.T) {
	m := NewTerminalModel()
	if got := m.tmuxOpts.ServerName; got == "" {
		t.Fatalf("expected default server name on construction, got empty")
	}

	custom := tmux.Options{
		ServerName:      "custom-server",
		ConfigPath:      "/tmp/custom.conf",
		HideStatus:      false,
		DisableMouse:    false,
		DefaultTerminal: "screen-256color",
		CommandTimeout:  7 * time.Second,
	}
	m.SetTmuxOptions(custom)

	if m.tmuxOpts != custom {
		t.Fatalf("SetTmuxOptions did not store options, got %+v", m.tmuxOpts)
	}
}

func TestSetTmuxOptionsAcceptsZeroValue(t *testing.T) {
	m := NewTerminalModel()
	m.SetTmuxOptions(tmux.Options{})
	if m.tmuxOpts != (tmux.Options{}) {
		t.Fatalf("expected zero-value options to be stored, got %+v", m.tmuxOpts)
	}
}

func TestSetInstanceID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "non-empty", id: "instance-42"},
		{name: "empty", id: ""},
		{name: "whitespace", id: "  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTerminalModel()
			m.SetInstanceID(tt.id)
			if m.instanceID != tt.id {
				t.Fatalf("expected instanceID %q, got %q", tt.id, m.instanceID)
			}
		})
	}
}

func TestSetInstanceIDOverwrites(t *testing.T) {
	m := NewTerminalModel()
	m.SetInstanceID("first")
	m.SetInstanceID("second")
	if m.instanceID != "second" {
		t.Fatalf("expected later SetInstanceID to overwrite, got %q", m.instanceID)
	}
}

func TestSetStyles(t *testing.T) {
	m := NewTerminalModel()

	styles := common.DefaultStyles()
	styles.Muted = styles.Muted.SetString("MUTED-MARKER")
	m.SetStyles(styles)

	if got := m.styles.Muted.Value(); got != "MUTED-MARKER" {
		t.Fatalf("SetStyles did not propagate, got %q", got)
	}
}

func TestSetMsgSink(t *testing.T) {
	m := NewTerminalModel()
	if m.msgSink != nil {
		t.Fatal("expected nil msgSink on construction")
	}

	var got tea.Msg
	m.SetMsgSink(func(msg tea.Msg) { got = msg })
	if m.msgSink == nil {
		t.Fatal("expected msgSink to be set")
	}

	want := tea.KeyPressMsg{Code: tea.KeyEnter}
	m.msgSink(want)
	if got != tea.Msg(want) {
		t.Fatalf("stored sink did not forward msg, got %#v", got)
	}
}

func TestSetMsgSinkNilClearsSink(t *testing.T) {
	m := NewTerminalModel()
	m.SetMsgSink(func(tea.Msg) {})
	m.SetMsgSink(nil)
	if m.msgSink != nil {
		t.Fatal("expected SetMsgSink(nil) to clear the sink")
	}
}

func TestSetWorkspace(t *testing.T) {
	m := NewTerminalModel()
	if m.workspaceID() != "" {
		t.Fatalf("expected empty workspace id initially, got %q", m.workspaceID())
	}

	ws := &data.Workspace{Repo: "/repo", Root: "/repo/ws"}
	m.setWorkspace(ws)
	if m.workspace != ws {
		t.Fatal("setWorkspace did not store the workspace pointer")
	}
	if got, want := m.workspaceID(), string(ws.ID()); got != want {
		t.Fatalf("workspaceID after setWorkspace = %q, want %q", got, want)
	}

	// Setting back to nil must be tolerated and reported as the empty id.
	m.setWorkspace(nil)
	if m.workspace != nil {
		t.Fatal("expected workspace to be cleared")
	}
	if m.workspaceID() != "" {
		t.Fatalf("expected empty workspace id after clearing, got %q", m.workspaceID())
	}
}

func TestSetActiveTabIdx(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"), newWorkspaceTab(t, "Terminal 2"))

	m.setActiveTabIdx(1)
	if got := m.getActiveTabIdx(); got != 1 {
		t.Fatalf("expected active idx 1, got %d", got)
	}

	// setActiveTabIdx is a thin store with no bounds checking; an out-of-range
	// value is recorded verbatim and getActiveTab must defend against it.
	m.setActiveTabIdx(99)
	if got := m.getActiveTabIdx(); got != 99 {
		t.Fatalf("expected stored idx 99, got %d", got)
	}
	if tab := m.getActiveTab(); tab != nil {
		t.Fatalf("expected nil active tab for out-of-range idx, got %+v", tab)
	}
}

func TestNextTab(t *testing.T) {
	tests := []struct {
		name    string
		tabs    int
		start   int
		wantIdx int
	}{
		{name: "single tab does not move", tabs: 1, start: 0, wantIdx: 0},
		{name: "advance forward", tabs: 3, start: 0, wantIdx: 1},
		{name: "wraps from last to first", tabs: 3, start: 2, wantIdx: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTerminalModel()
			m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
			tabs := make([]*TerminalTab, tt.tabs)
			for i := range tabs {
				tabs[i] = newWorkspaceTab(t, "Terminal")
			}
			seedTabs(t, m, tabs...)
			m.setActiveTabIdx(tt.start)

			m.NextTab()
			if got := m.getActiveTabIdx(); got != tt.wantIdx {
				t.Fatalf("NextTab from %d = %d, want %d", tt.start, got, tt.wantIdx)
			}
		})
	}
}

func TestNextTabNoTabsIsNoop(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	m.NextTab() // empty workspace
	if got := m.getActiveTabIdx(); got != 0 {
		t.Fatalf("expected idx 0 with no tabs, got %d", got)
	}
}

func TestPrevTab(t *testing.T) {
	tests := []struct {
		name    string
		tabs    int
		start   int
		wantIdx int
	}{
		{name: "single tab does not move", tabs: 1, start: 0, wantIdx: 0},
		{name: "move backward", tabs: 3, start: 2, wantIdx: 1},
		{name: "wraps from first to last", tabs: 3, start: 0, wantIdx: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTerminalModel()
			m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
			tabs := make([]*TerminalTab, tt.tabs)
			for i := range tabs {
				tabs[i] = newWorkspaceTab(t, "Terminal")
			}
			seedTabs(t, m, tabs...)
			m.setActiveTabIdx(tt.start)

			m.PrevTab()
			if got := m.getActiveTabIdx(); got != tt.wantIdx {
				t.Fatalf("PrevTab from %d = %d, want %d", tt.start, got, tt.wantIdx)
			}
		})
	}
}

func TestNextPrevTabRoundTrip(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	seedTabs(t, m,
		newWorkspaceTab(t, "Terminal 1"),
		newWorkspaceTab(t, "Terminal 2"),
		newWorkspaceTab(t, "Terminal 3"),
	)

	m.NextTab()
	m.PrevTab()
	if got := m.getActiveTabIdx(); got != 0 {
		t.Fatalf("expected round-trip to return to idx 0, got %d", got)
	}
}

func TestSelectTab(t *testing.T) {
	tests := []struct {
		name    string
		tabs    int
		idx     int
		wantIdx int
	}{
		{name: "valid middle index", tabs: 3, idx: 1, wantIdx: 1},
		{name: "valid last index", tabs: 3, idx: 2, wantIdx: 2},
		{name: "negative index rejected", tabs: 3, idx: -1, wantIdx: 0},
		{name: "out-of-range index rejected", tabs: 3, idx: 5, wantIdx: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTerminalModel()
			m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
			tabs := make([]*TerminalTab, tt.tabs)
			for i := range tabs {
				tabs[i] = newWorkspaceTab(t, "Terminal")
			}
			seedTabs(t, m, tabs...)

			m.SelectTab(tt.idx)
			if got := m.getActiveTabIdx(); got != tt.wantIdx {
				t.Fatalf("SelectTab(%d) left active idx %d, want %d", tt.idx, got, tt.wantIdx)
			}
		})
	}
}

func TestTerminalFocusBlurFocused(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))

	if m.Focused() {
		t.Fatal("expected model unfocused on construction")
	}

	m.Focus()
	if !m.Focused() {
		t.Fatal("expected Focused() true after Focus()")
	}
	ts := m.getTerminal()
	ts.mu.Lock()
	cursorOnFocus := ts.VTerm.ShowCursor
	ts.mu.Unlock()
	if !cursorOnFocus {
		t.Fatal("expected active terminal cursor visible after Focus()")
	}

	m.Blur()
	if m.Focused() {
		t.Fatal("expected Focused() false after Blur()")
	}
	ts.mu.Lock()
	cursorOnBlur := ts.VTerm.ShowCursor
	ts.mu.Unlock()
	if cursorOnBlur {
		t.Fatal("expected active terminal cursor hidden after Blur()")
	}
}

func TestFocusIsIdempotent(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))

	m.Focus()
	ts := m.getTerminal()
	// Prime a cache entry, then call Focus again. The early-return guard must
	// leave the existing cache intact because focus state did not change.
	ts.mu.Lock()
	ts.CachedSnap = nil
	ts.CachedVersion = 7
	ts.mu.Unlock()

	m.Focus() // already focused -> no-op
	ts.mu.Lock()
	version := ts.CachedVersion
	ts.mu.Unlock()
	if version != 7 {
		t.Fatalf("expected redundant Focus() to be a no-op, cache version = %d", version)
	}
}

func TestBlurIsIdempotentWhenUnfocused(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))

	ts := m.getTerminal()
	ts.mu.Lock()
	ts.CachedVersion = 9
	ts.mu.Unlock()

	m.Blur() // already unfocused -> no-op
	ts.mu.Lock()
	version := ts.CachedVersion
	ts.mu.Unlock()
	if version != 9 {
		t.Fatalf("expected redundant Blur() to be a no-op, cache version = %d", version)
	}
	if m.Focused() {
		t.Fatal("expected model to remain unfocused")
	}
}

func TestFocusWithoutTabsDoesNotPanic(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	// No tabs seeded: getTerminal() is nil, so the cursor toggle must early-return.
	m.Focus()
	if !m.Focused() {
		t.Fatal("expected focus state to flip even without an active terminal")
	}
	m.Blur()
	if m.Focused() {
		t.Fatal("expected blur to clear focus even without an active terminal")
	}
}

func TestSetActiveTerminalCursorVisibility(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))

	ts := m.getTerminal()
	// Seed a stale cache to prove it gets invalidated on every visibility change.
	ts.mu.Lock()
	ts.VTerm.ShowCursor = false
	ts.CachedVersion = 5
	ts.mu.Unlock()

	m.setActiveTerminalCursorVisibility(true)
	ts.mu.Lock()
	gotVisible := ts.VTerm.ShowCursor
	gotSnap := ts.CachedSnap
	gotVersion := ts.CachedVersion
	ts.mu.Unlock()
	if !gotVisible {
		t.Fatal("expected ShowCursor true")
	}
	if gotSnap != nil {
		t.Fatal("expected cached snapshot invalidated to nil")
	}
	if gotVersion != 0 {
		t.Fatalf("expected cached version reset to 0, got %d", gotVersion)
	}

	m.setActiveTerminalCursorVisibility(false)
	ts.mu.Lock()
	hidden := ts.VTerm.ShowCursor
	ts.mu.Unlock()
	if hidden {
		t.Fatal("expected ShowCursor false after hiding")
	}
}

func TestSetActiveTerminalCursorVisibilityNoTerminalIsNoop(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	// No tabs: getTerminal() returns nil and the method must early-return cleanly.
	m.setActiveTerminalCursorVisibility(true)
}

func TestSetActiveTerminalCursorVisibilityNilVTermIsNoop(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	tab := &TerminalTab{ID: generateTerminalTabID(), Name: "Terminal 1", State: &TerminalState{}}
	seedTabs(t, m, tab)

	// VTerm is nil; the method must still invalidate the cache without panicking.
	ts := m.getTerminal()
	ts.mu.Lock()
	ts.CachedVersion = 3
	ts.mu.Unlock()

	m.setActiveTerminalCursorVisibility(true)
	ts.mu.Lock()
	version := ts.CachedVersion
	snap := ts.CachedSnap
	ts.mu.Unlock()
	if version != 0 {
		t.Fatalf("expected cache version reset even with nil VTerm, got %d", version)
	}
	if snap != nil {
		t.Fatal("expected cached snapshot cleared even with nil VTerm")
	}
}
