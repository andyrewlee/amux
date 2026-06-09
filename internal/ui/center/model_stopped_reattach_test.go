package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
)

// TestUpdateTabSessionStatus_StoppedClearsReattachInFlight proves a stopped
// reconcile from the activity scan clears reattachInFlight, so a tab that goes
// stopped while a reattach was in flight is not left wedged (every reattach gate
// bails on that flag).
func TestUpdateTabSessionStatus_StoppedClearsReattachInFlight(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:               TabID("tab-stopped-reattach"),
		Assistant:        "claude",
		Workspace:        ws,
		SessionName:      "amux-ws-sess",
		Running:          true,
		reattachInFlight: true,
		Agent:            &appPty.Agent{Workspace: ws, Session: "amux-ws-sess"},
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0

	m.updateTabSessionStatus(messages.TabSessionStatus{
		Status:      "stopped",
		WorkspaceID: wsID,
		SessionName: "amux-ws-sess",
	})

	tab.mu.Lock()
	inFlight, running, detached := tab.reattachInFlight, tab.Running, tab.Detached
	tab.mu.Unlock()
	if inFlight {
		t.Fatal("expected reattachInFlight=false after a stopped reconcile")
	}
	if running {
		t.Fatal("expected Running=false after a stopped reconcile")
	}
	if detached {
		t.Fatal("expected Detached=false after a stopped reconcile")
	}
}
