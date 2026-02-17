package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestScanTmuxActivityNow_QueuesWhenInFlight(t *testing.T) {
	app := &App{tmuxActivityScanInFlight: true}
	cmd := app.scanTmuxActivityNow()
	if cmd != nil {
		t.Fatal("expected nil cmd when scan already in flight")
	}
	if !app.tmuxActivityRescanPending {
		t.Fatal("expected pending rescan to be queued")
	}
}

func TestHandleTmuxActivityTick_QueuesWhenInFlight(t *testing.T) {
	app := &App{
		tmuxActivityToken:        7,
		tmuxAvailable:            true,
		tmuxActivityScanInFlight: true,
	}
	cmds := app.handleTmuxActivityTick(tmuxActivityTick{Token: 7})
	if len(cmds) != 1 {
		t.Fatalf("expected only ticker reschedule while in flight, got %d cmds", len(cmds))
	}
	if !app.tmuxActivityRescanPending {
		t.Fatal("expected pending rescan to be queued")
	}
	if app.tmuxActivityToken != 7 {
		t.Fatalf("expected token unchanged while in flight, got %d", app.tmuxActivityToken)
	}
}

func TestHandleTmuxActivityResult_ConsumesPendingRescan(t *testing.T) {
	app := &App{
		tmuxActivityToken:         2,
		tmuxAvailable:             true,
		tmuxActivityScanInFlight:  true,
		tmuxActivityRescanPending: true,
		sessionActivityStates:     make(map[string]*activity.SessionState),
		tmuxActiveWorkspaceIDs:    make(map[string]bool),
		dashboard:                 dashboard.New(),
	}
	cmds := app.handleTmuxActivityResult(tmuxActivityResult{
		Token:              2,
		ActiveWorkspaceIDs: map[string]bool{},
		UpdatedStates:      map[string]*activity.SessionState{},
	})
	if len(cmds) == 0 {
		t.Fatal("expected pending rescan command to be enqueued")
	}
	if app.tmuxActivityToken != 3 {
		t.Fatalf("expected next scan token to be allocated, got %d", app.tmuxActivityToken)
	}
	if !app.tmuxActivityScanInFlight {
		t.Fatal("expected follow-up scan to be marked in flight")
	}
	if app.tmuxActivityRescanPending {
		t.Fatal("expected pending flag to be cleared")
	}
}
