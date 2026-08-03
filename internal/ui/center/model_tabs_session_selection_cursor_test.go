package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

// TestTabSelectionChangedCmd_ArmsChatCursorRefresh covers the hand-off for a
// chat tab that was working while hidden: background tabs get no post-write
// redraw, so selection is what arms the timer that repaints the cursor when the
// activity window expires.
func TestTabSelectionChangedCmd_ArmsChatCursorRefresh(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:        TabID("tab-chat"),
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
		Terminal:  vterm.New(80, 24),
		tabActivityState: tabActivityState{
			lastVisibleOutput: time.Now(),
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.workspace = ws

	if cmd := m.tabSelectionChangedCmd(true); cmd == nil {
		t.Fatalf("expected non-nil cmd")
	}

	tab.mu.Lock()
	pending := tab.cursorRefreshPending
	tab.mu.Unlock()
	if !pending {
		t.Fatalf("expected tab selection to arm the chat cursor refresh")
	}
}
