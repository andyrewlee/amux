package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/vterm"
)

// TestFocusedAgentTitle_ReturnsOSCTitle verifies that the accessor returns the
// OSC-reported title from the active tab's terminal.
func TestFocusedAgentTitle_ReturnsOSCTitle(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	m.workspace = ws

	term := vterm.New(80, 24)
	// Feed an OSC 0 title sequence.
	term.Write([]byte("\x1b]0;my-task\x07"))
	tab := &Tab{Assistant: "test-agent", Workspace: ws, Terminal: term}
	m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[string(ws.ID())] = 0

	got := m.FocusedAgentTitle()
	if got != "my-task" {
		t.Fatalf("FocusedAgentTitle() = %q, want %q", got, "my-task")
	}
}

// TestFocusedAgentTitle_ReturnsEmptyWhenNoTabs verifies the "" fallback when
// there are no tabs in the active workspace.
func TestFocusedAgentTitle_ReturnsEmptyWhenNoTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	m.workspace = ws

	got := m.FocusedAgentTitle()
	if got != "" {
		t.Fatalf("FocusedAgentTitle() with no tabs = %q, want empty string", got)
	}
}

// TestFocusedAgentTitle_ReturnsEmptyWhenTitleNotSet verifies the "" fallback
// when the terminal has received no OSC title sequence yet.
func TestFocusedAgentTitle_ReturnsEmptyWhenTitleNotSet(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	m.workspace = ws

	term := vterm.New(80, 24)
	tab := &Tab{Assistant: "test-agent", Workspace: ws, Terminal: term}
	m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[string(ws.ID())] = 0

	got := m.FocusedAgentTitle()
	if got != "" {
		t.Fatalf("FocusedAgentTitle() with no OSC title = %q, want empty string", got)
	}
}

// TestFocusedAgentTitle_ReturnsEmptyWhenNilTerminal verifies the "" fallback
// when the active tab has a nil Terminal.
func TestFocusedAgentTitle_ReturnsEmptyWhenNilTerminal(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	m.workspace = ws

	tab := &Tab{Assistant: "test-agent", Workspace: ws, Terminal: nil}
	m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[string(ws.ID())] = 0

	got := m.FocusedAgentTitle()
	if got != "" {
		t.Fatalf("FocusedAgentTitle() with nil terminal = %q, want empty string", got)
	}
}
