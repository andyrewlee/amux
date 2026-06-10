package app

import (
	"reflect"
	"testing"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func dashboardActiveWorkspaceCount(m *dashboard.Model) int {
	if m == nil {
		return 0
	}
	return reflect.ValueOf(m).Elem().FieldByName("activeWorkspaceIDs").Len()
}

func TestHandleTmuxActivityResult_SettlesAfterTwoSuccessfulScans(t *testing.T) {
	app := &App{
		tmuxActivity: tmuxActivityState{
			token:              1,
			scanInFlight:       true,
			sessionStates:      make(map[string]*activity.SessionState),
			activeWorkspaceIDs: make(map[string]bool),
		},
		dashboard: dashboard.New(),
	}

	app.handleTmuxActivityResult(tmuxActivityResult{
		Token:              1,
		ActiveWorkspaceIDs: map[string]bool{"ws1": true},
		UpdatedStates:      map[string]*activity.SessionState{},
	})
	if app.tmuxActivity.settled {
		t.Fatal("expected activity to remain unsettled after first successful scan")
	}
	if app.tmuxActivity.settledScans != 1 {
		t.Fatalf("expected settled scan count=1, got %d", app.tmuxActivity.settledScans)
	}

	app.tmuxActivity.token = 2
	app.tmuxActivity.scanInFlight = true
	app.handleTmuxActivityResult(tmuxActivityResult{
		Token:              2,
		ActiveWorkspaceIDs: map[string]bool{"ws1": true},
		UpdatedStates:      map[string]*activity.SessionState{},
	})
	if !app.tmuxActivity.settled {
		t.Fatal("expected activity to settle after second successful scan")
	}
	if app.tmuxActivity.settledScans != 2 {
		t.Fatalf("expected settled scan count=2, got %d", app.tmuxActivity.settledScans)
	}
}

func TestHandleTmuxAvailableResult_ResetsActivitySettlement(t *testing.T) {
	dash := dashboard.New()
	dash.SetActiveWorkspaces(map[string]bool{"ws-old": true})
	app := &App{
		tmuxAvailable: true,
		tmuxActivity: tmuxActivityState{
			settled:            true,
			settledScans:       5,
			activeWorkspaceIDs: map[string]bool{"ws-old": true},
		},
		dashboard: dash,
		toast:     common.NewToastModel(),
	}

	_ = app.handleTmuxAvailableResult(tmuxAvailableResult{available: true})
	if app.tmuxActivity.settled {
		t.Fatal("expected settled flag reset on tmux availability result")
	}
	if app.tmuxActivity.settledScans != 0 {
		t.Fatalf("expected settled scan count reset to 0, got %d", app.tmuxActivity.settledScans)
	}
	if len(app.tmuxActivity.activeWorkspaceIDs) != 0 {
		t.Fatalf("expected active workspace map reset, got %v", app.tmuxActivity.activeWorkspaceIDs)
	}
	if got := dashboardActiveWorkspaceCount(dash); got != 0 {
		t.Fatalf("expected dashboard active workspace state reset, got %d", got)
	}
}
