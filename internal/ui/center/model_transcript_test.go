package center

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/vterm"
)

// TestActiveTranscriptReturnsFullBuffer proves the export covers the
// combined scrollback+screen: enough writes to push early lines into
// scrollback, then assert both the old and the new text appear.
func TestActiveTranscriptReturnsFullBuffer(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	m.setWorkspace(ws)

	tab := &Tab{
		ID:        TabID("tab-1"),
		Assistant: "claude",
		Workspace: ws,
		Terminal:  vterm.New(40, 3), // 3-row screen: overflow lands in scrollback
		Running:   true,
	}
	m.AddTab(tab)

	// 6 lines on a 3-row screen → the first 3 scroll off into scrollback.
	tab.Terminal.Write([]byte("scrollback-line\r\n"))
	tab.Terminal.Write([]byte("middle\r\n"))
	tab.Terminal.Write([]byte("recent-line\r\n"))
	tab.Terminal.Write([]byte("screen-line"))

	got := m.ActiveTranscript()
	if got == "" {
		t.Fatal("expected transcript text")
	}
	for _, want := range []string{"scrollback-line", "recent-line", "screen-line"} {
		if !strings.Contains(got, want) {
			t.Fatalf("transcript missing %q — got %q", want, got)
		}
	}
}

func TestActiveTranscriptNoTerminal(t *testing.T) {
	m := newTestModel()
	m.setWorkspace(newTestWorkspace("ws", "/repo/ws"))
	if got := m.ActiveTranscript(); got != "" {
		t.Fatalf("no tabs: expected empty transcript, got %q", got)
	}

	// A tab without a terminal (diff-only tab shape) also yields "".
	ws := newTestWorkspace("ws", "/repo/ws")
	m.AddTab(&Tab{ID: TabID("tab-1"), Assistant: "claude", Workspace: ws, Running: true})
	if got := m.ActiveTranscript(); got != "" {
		t.Fatalf("terminal-less tab: expected empty transcript, got %q", got)
	}
}
