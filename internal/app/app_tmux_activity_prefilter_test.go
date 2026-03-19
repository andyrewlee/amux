package app

import (
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/app/activity"
)

func TestTmuxActivityScan_PrefilterErrorStillAllowsStaleFallback(t *testing.T) {
	app, wsID := newActivityTestAppWithScriptedTmux([]string{"new pane content"})
	ops, ok := app.tmuxService.(*scriptedActivityTmuxOps)
	if !ok {
		t.Fatalf("expected scripted tmux ops, got %T", app.tmuxService)
	}
	ops.prefilterErr = errors.New("prefilter unavailable")
	ops.lastOutputAge = activity.OutputWindow + time.Second

	app.sessionActivityStates[ops.sessionName] = &activity.SessionState{
		Initialized: true,
		Score:       activity.ScoreThreshold - 2,
	}

	runImmediateTmuxActivityScan(t, app)
	if !app.tmuxActiveWorkspaceIDs[wsID] {
		t.Fatalf("expected workspace %s active via stale-tag fallback when prefilter is unavailable", wsID)
	}
}
