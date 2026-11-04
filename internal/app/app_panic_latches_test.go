package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/app/bulkops"
	"github.com/andyrewlee/amux/internal/coalesce"
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
	// Latch git-status mid-flight with a pending follow-up owed.
	latched := coalesce.New(mergeGitStatusWant)
	latched.Admit(gitStatusWant{full: true})
	latched.Admit(gitStatusWant{full: true})
	a := &App{
		gitStatus:     panicGitStatus{}, // panics inside the watcher handler
		fileWatcher:   &git.FileWatcher{},
		fileWatcherCh: ch,
		gitStatusCoalesce: map[string]*coalesce.Entry[gitStatusWant]{
			"/tmp/repo/ws": latched,
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
	a.tmuxActivity.scanEntry().Admit(scanWant{})

	model, _ := a.Update(messages.FileWatcherEvent{Root: "/tmp/repo/ws"})

	if model != a {
		t.Fatalf("Update returned model %T, want the same *App after panic recovery", model)
	}
	if a.err == nil {
		t.Fatal("expected a.err to record the recovered panic")
	}
	if len(a.gitStatusCoalesce) != 0 {
		t.Fatalf("git-status coalescer still latched: %v", a.gitStatusCoalesce)
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
	if a.tmuxActivity.scanEntry().InFlight() {
		t.Fatal("tmux activity scan still latched")
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
// settingsUIPersistPending (a write still owed) and a pending rescan (a scan
// still requested) must survive: clearing them would silently drop owed work.
func TestResetAdvisoryLatchesAfterPanic_LeavesObligationMarks(t *testing.T) {
	a := &App{}
	a.overlays.settingsUIPersistPending = true
	scan := a.tmuxActivity.scanEntry()
	scan.Admit(scanWant{}) // start
	scan.Admit(scanWant{}) // pending rescan owed

	a.resetAdvisoryLatchesAfterPanic()

	if !a.overlays.settingsUIPersistPending {
		t.Fatal("settingsUIPersistPending cleared — a pending save must stay owed")
	}
	if got := a.tmuxActivity.scanEntry().State(); got != coalesce.IdlePending {
		t.Fatalf("pending rescan cleared (state %v) — a queued rescan must stay owed", got)
	}
	if rerun, _ := a.tmuxActivity.scanEntry().Complete(); !rerun {
		t.Fatal("the surviving obligation must still discharge a follow-up")
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
