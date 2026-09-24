package sidebar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
)

// TestTerminalTabBarSanitizesTabNames proves a persisted/session-derived tab
// name can't carry escapes or fabricate text into the sidebar tab bar.
func TestTerminalTabBarSanitizesTabNames(t *testing.T) {
	m := NewTerminalModel()
	m.width = 60
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	m.workspace = ws
	wsID := string(ws.ID())
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{
		{ID: "t1", Name: "x\x1b[8m\nspoof"},
	}

	raw := m.renderTabBar()
	if strings.Contains(raw, "\x1b[8m") {
		t.Fatalf("tab name escape reached the frame: %q", raw)
	}
	if plain := ansi.Strip(raw); !strings.Contains(plain, "xspoof") {
		t.Fatalf("sanitized tab name missing, got %q", plain)
	}
}
