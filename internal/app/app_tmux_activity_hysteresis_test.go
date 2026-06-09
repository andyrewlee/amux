package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/tmux"
)

// TestSyncActivitySessionStates_DemotionHysteresis proves a running session is
// not demoted on a single non-live observation and only demoted once the miss
// threshold is reached — so a transient tmux glitch cannot tear down a working
// background agent.
func TestSyncActivitySessionStates_DemotionHysteresis(t *testing.T) {
	const sessionName = "amux-ws-sess"
	info := map[string]activity.SessionInfo{
		sessionName: {Status: "running", WorkspaceID: "ws", IsChat: true},
	}
	sessions := []activity.TaggedSession{{Session: tmux.SessionActivity{Name: sessionName}}}
	deadSvc := stubTmuxOps{allStates: map[string]tmux.SessionState{}} // not live
	miss := map[string]int{}

	r1 := syncActivitySessionStates(info, sessions, deadSvc, tmux.Options{}, miss)
	if len(r1) != 0 {
		t.Fatalf("first non-live observation must not demote, got %d stopped", len(r1))
	}
	if info[sessionName].Status != "running" {
		t.Fatalf("first miss must keep status running, got %q", info[sessionName].Status)
	}

	r2 := syncActivitySessionStates(info, sessions, deadSvc, tmux.Options{}, miss)
	if len(r2) != 1 {
		t.Fatalf("second consecutive non-live observation must demote, got %d stopped", len(r2))
	}
	if info[sessionName].Status != "stopped" {
		t.Fatalf("expected stopped after threshold, got %q", info[sessionName].Status)
	}
}

// TestSyncActivitySessionStates_LiveResetsMissCounter proves a live observation
// resets the per-session miss counter, so a session that flickers does not
// accumulate misses toward demotion.
func TestSyncActivitySessionStates_LiveResetsMissCounter(t *testing.T) {
	const sessionName = "amux-ws-sess"
	info := map[string]activity.SessionInfo{
		sessionName: {Status: "running", WorkspaceID: "ws"},
	}
	sessions := []activity.TaggedSession{{Session: tmux.SessionActivity{Name: sessionName}}}
	miss := map[string]int{}

	deadSvc := stubTmuxOps{allStates: map[string]tmux.SessionState{}}
	syncActivitySessionStates(info, sessions, deadSvc, tmux.Options{}, miss)
	if miss[sessionName] != 1 {
		t.Fatalf("expected 1 miss after one non-live observation, got %d", miss[sessionName])
	}

	liveSvc := stubTmuxOps{allStates: map[string]tmux.SessionState{
		sessionName: {Exists: true, HasLivePane: true},
	}}
	syncActivitySessionStates(info, sessions, liveSvc, tmux.Options{}, miss)
	if _, ok := miss[sessionName]; ok {
		t.Fatalf("a live observation must reset the miss counter, still have %d", miss[sessionName])
	}
}
