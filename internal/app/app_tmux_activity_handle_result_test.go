package app

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestHandleTmuxActivityResult_OwnerTransitionErrorResetsHysteresis(t *testing.T) {
	app := &App{
		tmuxActivityToken:        5,
		tmuxActivityScanInFlight: true,
		tmuxActivityOwnershipSet: true,
		tmuxActivityScannerOwner: false,
		tmuxActivityOwnerEpoch:   1,
		sessionActivityStates: map[string]*activity.SessionState{
			"stale-session": {},
		},
		tmuxActivitySettled:      true,
		tmuxActivitySettledScans: 2,
		tmuxActiveWorkspaceIDs:   map[string]bool{"ws-stale": true},
		dashboard:                dashboard.New(),
	}
	app.syncActiveWorkspacesToDashboard()

	app.handleTmuxActivityResult(tmuxActivityResult{
		Token:        5,
		RoleKnown:    true,
		ScannerOwner: true,
		ScannerEpoch: 2,
		Err:          errors.New("owner scan failed"),
	})
	if len(app.sessionActivityStates) != 0 {
		t.Fatalf("expected hysteresis reset on owner transition despite scan error, got %v", app.sessionActivityStates)
	}
	if len(app.tmuxActiveWorkspaceIDs) != 0 {
		t.Fatalf("expected stale active-workspace map cleared on owner transition, got %v", app.tmuxActiveWorkspaceIDs)
	}
	if got := dashboardActiveWorkspaceCount(app.dashboard); got != 0 {
		t.Fatalf("expected dashboard activity cleared on owner transition, got %d", got)
	}

	app.handleTmuxActivityResult(tmuxActivityResult{
		Token:              5,
		RoleKnown:          true,
		ScannerOwner:       true,
		ScannerEpoch:       2,
		ActiveWorkspaceIDs: map[string]bool{"ws-new": true},
		UpdatedStates: map[string]*activity.SessionState{
			"new-session": {},
		},
	})
	if len(app.sessionActivityStates) != 1 {
		t.Fatalf("expected only fresh owner state after recovery scan, got %v", app.sessionActivityStates)
	}
	if _, ok := app.sessionActivityStates["stale-session"]; ok {
		t.Fatalf("expected stale pre-transition state to remain cleared, got %v", app.sessionActivityStates)
	}
	if !app.tmuxActiveWorkspaceIDs["ws-new"] {
		t.Fatalf("expected recovered owner activity to apply, got %v", app.tmuxActiveWorkspaceIDs)
	}
}

// TestHandleTmuxActivityResult_OwnerTransitionResetsSettlement proves a
// follower->owner handoff re-enters the unsettled state, so the transient empty
// active set the transition publishes is not treated as a confirmed all-idle set
// (which would blink every working-agent spinner off until the new owner's first
// scan lands). Indicators repopulate only after the owner re-settles.
func TestHandleTmuxActivityResult_OwnerTransitionResetsSettlement(t *testing.T) {
	dash := dashboard.New()
	dash.SetActiveWorkspaces(map[string]bool{"ws-old": true})
	app := &App{
		tmuxActivityToken:        7,
		tmuxActivityScanInFlight: true,
		tmuxActivityOwnershipSet: true,
		tmuxActivityScannerOwner: false,
		tmuxActivityOwnerEpoch:   1,
		sessionActivityStates:    map[string]*activity.SessionState{},
		tmuxActivitySettled:      true,
		tmuxActivitySettledScans: 2,
		tmuxActiveWorkspaceIDs:   map[string]bool{"ws-old": true},
		dashboard:                dash,
	}

	// Follower->owner transition (epoch bump). The owner scan succeeds and reports
	// an active workspace, but settlement must restart from zero so the active set
	// is not yet treated as authoritative.
	app.handleTmuxActivityResult(tmuxActivityResult{
		Token:              7,
		RoleKnown:          true,
		ScannerOwner:       true,
		ScannerEpoch:       2,
		ActiveWorkspaceIDs: map[string]bool{"ws-new": true},
		UpdatedStates:      map[string]*activity.SessionState{},
	})
	if app.tmuxActivitySettled {
		t.Fatal("expected settlement reset on owner transition")
	}
	if app.tmuxActivitySettledScans != 1 {
		t.Fatalf("expected settle scans to restart at 1 after the transition scan, got %d", app.tmuxActivitySettledScans)
	}
	if got := dashboardActiveWorkspaceCount(dash); got != 0 {
		t.Fatalf("expected transient active set withheld while unsettled, got %d", got)
	}

	// Second successful owner scan re-settles and republishes the active set.
	app.tmuxActivityToken = 8
	app.tmuxActivityScanInFlight = true
	app.handleTmuxActivityResult(tmuxActivityResult{
		Token:              8,
		RoleKnown:          true,
		ScannerOwner:       true,
		ScannerEpoch:       2,
		ActiveWorkspaceIDs: map[string]bool{"ws-new": true},
		UpdatedStates:      map[string]*activity.SessionState{},
	})
	if !app.tmuxActivitySettled {
		t.Fatal("expected owner to re-settle after two successful scans")
	}
	if got := dashboardActiveWorkspaceCount(dash); got != 1 {
		t.Fatalf("expected indicators to repopulate after re-settle, got %d", got)
	}
}
