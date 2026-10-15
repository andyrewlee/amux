package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/app/bulkops"
	"github.com/andyrewlee/amux/internal/data"
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
		gitStatusPending: map[string]bool{
			"/tmp/repo/ws": true,
		},
		gitStatusPendingFull: map[string]bool{
			"/tmp/repo/ws": true,
		},
		pendingWorkspaceCreate: pendingWorkspaceCreateState{
			project: &data.Project{Name: "repo", Path: "/repo"},
			name:    "staged",
		},
		pendingLaunchTask: pendingLaunchTaskState{
			assistant: "claude",
			workspace: &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"},
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
	if len(a.gitStatusPending) != 0 {
		t.Fatalf("gitStatusPending still latched: %v", a.gitStatusPending)
	}
	if len(a.gitStatusPendingFull) != 0 {
		t.Fatalf("gitStatusPendingFull still latched: %v", a.gitStatusPendingFull)
	}
	if a.pendingWorkspaceCreate != (pendingWorkspaceCreateState{}) {
		t.Fatal("pendingWorkspaceCreate still armed — a stale handoff survives panics")
	}
	if a.pendingLaunchTask != (pendingLaunchTaskState{}) {
		t.Fatal("pendingLaunchTask still armed — a stale handoff survives panics")
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

// TestResetAdvisoryLatchesAfterPanic_AbandonsBulk covers the wedge: a panic
// inside advanceBulk strands headID + queue forever, rejecting every later
// bulk op. The reset must abandon the batch — clearing the dashboard marks
// finishBulk would have dropped — while bulkFinished's empty-headID gate
// keeps the in-flight head op's late completion from corrupting a future
// batch.
func TestResetAdvisoryLatchesAfterPanic_AbandonsBulk(t *testing.T) {
	a := newBulkTestApp()
	targets := []bulkops.Target{makeBulkTarget("a"), makeBulkTarget("b")}
	// Arm the state a mid-advance panic leaves: head op in flight, one
	// target still queued, all marks lit on the dashboard.
	if cmd := a.bulk.Start(bulkops.Shelve, targets); cmd == nil {
		t.Fatal("expected the batch's first op cmds")
	}
	a.dashboard.MarkWorkspaceIDs([]string{targets[0].MarkID, targets[1].MarkID})
	if a.dashboard.MarkedCount() != 2 {
		t.Fatalf("fixture marks = %d, want 2", a.dashboard.MarkedCount())
	}

	a.resetAdvisoryLatchesAfterPanic()

	if a.bulk.Active() {
		t.Fatal("wedged bulk batch survived the panic reset")
	}
	if got := a.dashboard.MarkedCount(); got != 0 {
		t.Fatalf("dashboard marks = %d after abandoned batch, want 0", got)
	}
	// The in-flight head op's late completion must not corrupt anything:
	// headID was cleared with the batch, so Finished ignores it.
	if cmd := a.bulk.Finished(targets[0].Workspace, true); cmd != nil {
		t.Fatal("orphaned head completion produced a cmd against the cleared batch")
	}
	// And a fresh batch can start — the wedge is actually gone.
	if cmd := a.bulk.Start(bulkops.Delete, targets); cmd == nil {
		t.Fatal("post-reset bulk op rejected — the wedge is still armed")
	}
}

// TestResetAdvisoryLatchesAfterPanic_PreservesMutationMarks is the negative
// pin: lifecycle mutation marks serialize worktree add/remove, so a false
// clear admits concurrent mutation — strictly worse than a wedge. The reset
// must leave them alone (they are NOT idempotent read marks like the rest).
func TestResetAdvisoryLatchesAfterPanic_PreservesMutationMarks(t *testing.T) {
	a := newBulkTestApp()
	ws := makeBulkTarget("pinned").Workspace
	if !a.markWorkspaceMutationInFlight(ws, true) {
		t.Fatal("fixture mark did not take")
	}

	a.resetAdvisoryLatchesAfterPanic()

	if !a.isWorkspaceMutationInFlight(string(ws.ID())) {
		t.Fatal("mutation mark cleared by the panic reset — a concurrent lifecycle op can now interleave")
	}
}
