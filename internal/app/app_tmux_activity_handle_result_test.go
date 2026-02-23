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
		tmuxActiveWorkspaceIDs: map[string]bool{},
		dashboard:              dashboard.New(),
	}

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
