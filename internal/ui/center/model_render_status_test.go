package center

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/vterm"
)

// statusTab builds a tab with a terminal attached, since the status line is only
// rendered for a tab that has one.
func statusTab(m *Model) *Tab {
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-status"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(20, 4),
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	return tab
}

func TestTerminalStatusLine(t *testing.T) {
	tests := []struct {
		name             string
		running          bool
		detached         bool
		reattachInFlight bool
		want             string
	}{
		{
			name:    "attached and running shows nothing",
			running: true,
			want:    "",
		},
		{
			name:     "detached",
			detached: true,
			want:     "DETACHED",
		},
		{
			name: "stopped",
			want: "STOPPED",
		},
		{
			// A reattach is several tmux round-trips against a server that is also
			// pumping output for every other attached agent, so it can visibly take
			// a moment. Leaving DETACHED up reads as "nothing is happening" exactly
			// when something is.
			name:             "reattach in flight reports progress instead of DETACHED",
			detached:         true,
			reattachInFlight: true,
			want:             "REATTACHING",
		},
		{
			name:             "reattach from a stopped tab also reports progress",
			reattachInFlight: true,
			want:             "REATTACHING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			tab := statusTab(m)
			tab.Running = tt.running
			tab.Detached = tt.detached
			tab.reattachInFlight = tt.reattachInFlight

			got := strings.TrimSpace(ansi.Strip(m.ActiveTerminalStatusLine()))
			if got != tt.want {
				t.Fatalf("status line = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTerminalStatusLine_ScrollPositionWinsOverReattach(t *testing.T) {
	// While scrolled back the user is reading history, and the scroll position is
	// the more useful thing to show; the reattach still finishes underneath.
	m := newTestModel()
	tab := statusTab(m)
	tab.Detached = true
	tab.reattachInFlight = true
	tab.Terminal.Scrollback = [][]vterm.Cell{vterm.MakeBlankLine(20), vterm.MakeBlankLine(20)}

	tab.mu.Lock()
	m.scrollTerminalViewLocked(tab, 1)
	tab.mu.Unlock()

	got := ansi.Strip(m.ActiveTerminalStatusLine())
	if !strings.Contains(got, "SCROLL") {
		t.Fatalf("expected the scroll position to take precedence, got %q", got)
	}
}
