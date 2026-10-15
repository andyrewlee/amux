package app

import (
	"sync"
	"testing"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/testutil/tmuxops"
	"github.com/andyrewlee/amux/internal/tmux"
)

// TestFetchAndSyncActivitySessionStates_ConcurrentScans exercises the
// missBySession overlap the panic reset can create: scanInFlight normally
// serializes scans, but resetAdvisoryLatchesAfterPanic clears it while a
// scan is still running — two goroutines then race on missBySession. The
// missMu guard makes that overlap a serialized, race-free double-sync
// instead of a concurrent-map-write fatal.
func TestFetchAndSyncActivitySessionStates_ConcurrentScans(t *testing.T) {
	// A session absent from AllSessionStates counts as a miss on every
	// scan — the miss counter for a shared key is what the race targets.
	fake := &tmuxops.FakeTmuxOps{
		AllSessionStatesFunc: func(tmux.Options) (map[string]tmux.SessionState, error) {
			return map[string]tmux.SessionState{}, nil
		},
		SessionsWithTagsFunc: func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
			return []tmux.SessionTagValues{
				{Name: "session-a", Tags: map[string]string{}},
				{Name: "session-b", Tags: map[string]string{}},
			}, nil
		},
	}
	app := &App{
		tmuxService:  fake,
		tmuxActivity: newTmuxActivityState(),
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				infoBySession := map[string]activity.SessionInfo{
					"session-a": {Status: "running", WorkspaceID: "ws-a"},
					"session-b": {Status: "running", WorkspaceID: "ws-a"},
				}
				_, _, _, err := app.fetchAndSyncActivitySessionStates(infoBySession, tmux.Options{}, fake)
				if err != nil {
					t.Errorf("fetchAndSyncActivitySessionStates: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
