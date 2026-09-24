package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// TestSyncActiveWorkspacesToDashboard_ReturnsBellCmd pins the contract that
// replaced emitDashboardStateCmd: the opt-in agent-done bell Cmd is RETURNED
// to the caller for the normal []tea.Cmd path — never executed inline and
// never pushed through the external-message pump. The runtime invokes it, so
// invoking it here must produce the bell message.
func TestSyncActiveWorkspacesToDashboard_ReturnsBellCmd(t *testing.T) {
	dash := dashboard.New()
	dash.SetNotifyOnDone(true)
	// Seed prev=Working so the next publish is a Working→Done edge.
	if cmd := dash.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateWorking}); cmd != nil {
		t.Fatal("seeding a Working state must not bell")
	}

	app := &App{
		tmuxActivity: tmuxActivityState{
			settled:            true,
			activeWorkspaceIDs: map[string]bool{"ws-1": true},
			agentStates:        map[string]activity.AgentState{"ws-1": activity.StateDone},
		},
		dashboard:    dash,
		externalMsgs: make(chan tea.Msg, 4),
	}

	cmd := app.syncActiveWorkspacesToDashboard()
	if cmd == nil {
		t.Fatal("expected the bell Cmd to be returned for a Working→Done edge")
	}
	select {
	case msg := <-app.externalMsgs:
		t.Fatalf("external pump must not be used for Update-produced Cmds, got %#v", msg)
	default:
	}
	msg := cmd()
	if _, ok := msg.(tea.RawMsg); !ok {
		t.Fatalf("bell Cmd must produce tea.RawMsg when the runtime invokes it, got %#v", msg)
	}

	if cmd := app.syncActiveWorkspacesToDashboard(); cmd != nil {
		t.Fatal("steady-state Done must not re-bell on the next sync")
	}
}
