package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestHandleTmuxActivityResult_SettlesAfterTwoSuccessfulScans(t *testing.T) {
	app := &App{
		tmuxActivityToken:        1,
		tmuxActivityScanInFlight: true,
		sessionActivityStates:    make(map[string]*activity.SessionState),
		tmuxActiveWorkspaceIDs:   make(map[string]bool),
		dashboard:                dashboard.New(),
	}

	app.handleTmuxActivityResult(tmuxActivityResult{
		Token:              1,
		ActiveWorkspaceIDs: map[string]bool{"ws1": true},
		UpdatedStates:      map[string]*activity.SessionState{},
	})
	if app.tmuxActivitySettled {
		t.Fatal("expected activity to remain unsettled after first successful scan")
	}
	if app.tmuxActivitySettledScans != 1 {
		t.Fatalf("expected settled scan count=1, got %d", app.tmuxActivitySettledScans)
	}

	app.tmuxActivityToken = 2
	app.tmuxActivityScanInFlight = true
	app.handleTmuxActivityResult(tmuxActivityResult{
		Token:              2,
		ActiveWorkspaceIDs: map[string]bool{"ws1": true},
		UpdatedStates:      map[string]*activity.SessionState{},
	})
	if !app.tmuxActivitySettled {
		t.Fatal("expected activity to settle after second successful scan")
	}
	if app.tmuxActivitySettledScans != 2 {
		t.Fatalf("expected settled scan count=2, got %d", app.tmuxActivitySettledScans)
	}
}

func TestHandleTmuxAvailableResult_ResetsActivitySettlement(t *testing.T) {
	app := &App{
		tmuxAvailable:            true,
		tmuxActivitySettled:      true,
		tmuxActivitySettledScans: 5,
		tmuxActiveWorkspaceIDs:   map[string]bool{"ws-old": true},
		toast:                    common.NewToastModel(),
	}

	_ = app.handleTmuxAvailableResult(tmuxAvailableResult{available: true})
	if app.tmuxActivitySettled {
		t.Fatal("expected settled flag reset on tmux availability result")
	}
	if app.tmuxActivitySettledScans != 0 {
		t.Fatalf("expected settled scan count reset to 0, got %d", app.tmuxActivitySettledScans)
	}
	if len(app.tmuxActiveWorkspaceIDs) != 0 {
		t.Fatalf("expected active workspace map reset, got %v", app.tmuxActiveWorkspaceIDs)
	}
}
