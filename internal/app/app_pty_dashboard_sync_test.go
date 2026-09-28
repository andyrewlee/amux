package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// TestPTYMessagesDoNotRepublishDashboard pins the removal of the per-PTY
// syncActiveWorkspacesToDashboard call: PTY output/flush messages only move
// bytes into vterm — the active/agent inputs live on tmuxActivity, whose
// writers (activity results, lifecycle handlers) publish themselves. A
// republish on this path would rebuild identical maps at output rate.
func TestPTYMessagesDoNotRepublishDashboard(t *testing.T) {
	dash := dashboard.New()
	app := &App{
		center:    center.New(nil),
		dashboard: dash,
		tmuxActivity: tmuxActivityState{
			settled:            true,
			activeWorkspaceIDs: map[string]bool{"ws-1": true},
			agentStates:        map[string]activity.AgentState{"ws-1": activity.StateWorking},
		},
	}
	// Publish the seeded state once so the dashboard's cache holds it.
	app.syncActiveWorkspacesToDashboard()
	base := dash.ContentVersion()

	for _, msg := range []any{
		center.PTYFlush{WorkspaceID: "ws-1"},
		center.PTYStopped{WorkspaceID: "ws-1"},
	} {
		m, _ := app.Update(msg)
		if updated, ok := m.(*App); ok {
			app = updated
		}
	}
	if got := dash.ContentVersion(); got != base {
		t.Fatalf("PTY messages bumped dashboard contentVersion %d → %d", base, got)
	}
}

// TestPTYStoppedDoesNotRevertActiveState covers the mid-stream edge: a
// PTYStopped arriving while tmuxActivity still marks the workspace active
// must leave the published state untouched (it no longer publishes at all).
func TestPTYStoppedDoesNotRevertActiveState(t *testing.T) {
	dash := dashboard.New()
	app := &App{
		center:    center.New(nil),
		dashboard: dash,
		tmuxActivity: tmuxActivityState{
			settled:            true,
			activeWorkspaceIDs: map[string]bool{"ws-1": true},
			agentStates:        map[string]activity.AgentState{"ws-1": activity.StateWorking},
		},
	}
	app.syncActiveWorkspacesToDashboard()
	m, _ := app.Update(center.PTYStopped{WorkspaceID: "ws-1"})
	if updated, ok := m.(*App); ok {
		app = updated
	}
	// The workspace must still read as active+working — PTYStopped carries
	// no authority over tmuxActivity state.
	if !app.tmuxActivity.activeWorkspaceIDs["ws-1"] {
		t.Fatal("PTYStopped dropped the workspace from activeWorkspaceIDs")
	}
	if app.tmuxActivity.agentStates["ws-1"] != activity.StateWorking {
		t.Fatalf("agentStates[ws-1] = %v after PTYStopped, want working", app.tmuxActivity.agentStates["ws-1"])
	}
}
