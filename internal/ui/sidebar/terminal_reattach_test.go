package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestReattachPrependsScrollback(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()

	m.tabsByWorkspace[wsID] = []*TerminalTab{
		{
			ID: tabID,
			State: &TerminalState{
				SessionName: "session-1",
				Running:     false,
				Detached:    true,
			},
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	msg := SidebarTerminalReattachResult{
		WorkspaceID: wsID,
		TabID:       tabID,
		SessionName: "session-1",
		Scrollback:  []byte("line-1\nline-2\n"),
	}

	_, _ = m.Update(msg)

	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil || tab.State.VTerm == nil {
		t.Fatal("expected vterm to be created on reattach")
	}
	if len(tab.State.VTerm.Scrollback) == 0 {
		t.Fatal("expected scrollback to be prepended on reattach")
	}

	firstLen := len(tab.State.VTerm.Scrollback)
	_, _ = m.Update(msg)
	if len(tab.State.VTerm.Scrollback) != firstLen {
		t.Fatal("expected scrollback not to duplicate on reattach")
	}
}
