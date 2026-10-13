package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
)

// TestUpdatePanicClearsAdvisoryLatches covers the fourth panic-liveness
// class: a handler that sets a single-flight mark and then panics before
// returning its producer cmd leaves the mark latched forever (the result
// message that would clear it never exists). The recover path must reset
// every advisory mark so the next request can run.
func TestUpdatePanicClearsAdvisoryLatches(t *testing.T) {
	ch := make(chan messages.FileWatcherEvent, 1)
	a := &App{
		gitStatus:     panicGitStatus{}, // panics inside the watcher handler
		fileWatcher:   &git.FileWatcher{},
		fileWatcherCh: ch,
		gitStatusInFlight: map[string]bool{
			"/tmp/repo/ws": true,
		},
		runScriptStatusInFlight: true,
		upgradeRunning:          true,
	}
	a.tmuxActivity.scanInFlight = true

	model, _ := a.Update(messages.FileWatcherEvent{Root: "/tmp/repo/ws"})

	if model != a {
		t.Fatalf("Update returned model %T, want the same *App after panic recovery", model)
	}
	if a.err == nil {
		t.Fatal("expected a.err to record the recovered panic")
	}
	if len(a.gitStatusInFlight) != 0 {
		t.Fatalf("gitStatusInFlight still latched: %v", a.gitStatusInFlight)
	}
	if a.runScriptStatusInFlight {
		t.Fatal("runScriptStatusInFlight still latched")
	}
	if a.upgradeRunning {
		t.Fatal("upgradeRunning still latched")
	}
	if a.tmuxActivity.scanInFlight {
		t.Fatal("tmuxActivity.scanInFlight still latched")
	}
}

// TestResetAdvisoryLatchesAfterPanic_NilSafe proves the reset runs on a bare
// App — it executes inside the recover path where a panic would escape
// Update entirely.
func TestResetAdvisoryLatchesAfterPanic_NilSafe(t *testing.T) {
	a := &App{}
	a.resetAdvisoryLatchesAfterPanic()
}

// TestResetAdvisoryLatchesAfterPanic_LeavesObligationMarks proves the reset
// clears only single-flight marks — retry-obligation state like
// settingsUIPersistPending (a write still owed) and rescanPending (a scan
// still requested) must survive: clearing them would silently drop owed work.
func TestResetAdvisoryLatchesAfterPanic_LeavesObligationMarks(t *testing.T) {
	a := &App{}
	a.overlays.settingsUIPersistPending = true
	a.tmuxActivity.rescanPending = true

	a.resetAdvisoryLatchesAfterPanic()

	if !a.overlays.settingsUIPersistPending {
		t.Fatal("settingsUIPersistPending cleared — a pending save must stay owed")
	}
	if !a.tmuxActivity.rescanPending {
		t.Fatal("rescanPending cleared — a queued rescan must stay owed")
	}
}
