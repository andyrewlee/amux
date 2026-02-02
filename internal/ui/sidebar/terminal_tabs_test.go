package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestAddTabsFromSessionsDedupes(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())

	cmds := m.AddTabsFromSessions(ws, []string{"sess-1", "sess-2"})
	if len(cmds) != 2 {
		t.Fatalf("expected 2 cmds, got %d", len(cmds))
	}
	if got := len(m.tabsByWorkspace[wsID]); got != 2 {
		t.Fatalf("expected 2 tabs, got %d", got)
	}

	cmds = m.AddTabsFromSessions(ws, []string{"sess-1", "sess-2"})
	if len(cmds) != 0 {
		t.Fatalf("expected 0 cmds on duplicate add, got %d", len(cmds))
	}
	if got := len(m.tabsByWorkspace[wsID]); got != 2 {
		t.Fatalf("expected 2 tabs after dedupe, got %d", got)
	}
}

func TestAddTabsFromSessionInfosAttachRespectsFlag(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())

	cmds := m.AddTabsFromSessionInfos(ws, []SessionAttachInfo{
		{Name: "sess-1", Attach: true, DetachExisting: true},
		{Name: "sess-2", Attach: true, DetachExisting: false},
	})
	if len(cmds) != 2 {
		t.Fatalf("expected 2 cmds for attachable sessions, got %d", len(cmds))
	}
	if got := len(m.tabsByWorkspace[wsID]); got != 2 {
		t.Fatalf("expected 2 tabs, got %d", got)
	}
}
