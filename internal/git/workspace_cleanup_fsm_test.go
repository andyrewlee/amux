package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The resume FSM must finish a cleanup correctly no matter which phase
// boundary a previous run was interrupted at; each test below starts from the
// persisted state such an interruption leaves behind.

func cleanupStagePath(workspacePath string) string {
	return filepath.Join(filepath.Dir(workspacePath), "."+filepath.Base(workspacePath)+".amux-prune-1")
}

func TestResumeWorkspaceCleanup_InterruptedBeforeStageRemoval(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	stage := cleanupStagePath(ws)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	state := workspaceCleanupState{CleanupPath: stage}
	if err := writeWorkspaceCleanupState(ws, state); err != nil {
		t.Fatal(err)
	}

	if err := resumeWorkspaceCleanup(context.Background(), dir, ws, state); err != nil {
		t.Fatalf("resumeWorkspaceCleanup error = %v", err)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("expected staged dir removed, err=%v", err)
	}
	if _, marked, err := readWorkspaceCleanupState(ws); err != nil || marked {
		t.Fatalf("expected marker cleared, marked=%v err=%v", marked, err)
	}
}

func TestResumeWorkspaceCleanup_InterruptedAfterStageRemoval(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	stage := cleanupStagePath(ws)
	// The staged directory is already gone (removed by the interrupted run),
	// only the marker survived.
	state := workspaceCleanupState{CleanupPath: stage}
	if err := writeWorkspaceCleanupState(ws, state); err != nil {
		t.Fatal(err)
	}

	if err := resumeWorkspaceCleanup(context.Background(), dir, ws, state); err != nil {
		t.Fatalf("resumeWorkspaceCleanup error = %v", err)
	}
	if _, marked, err := readWorkspaceCleanupState(ws); err != nil || marked {
		t.Fatalf("expected marker cleared, marked=%v err=%v", marked, err)
	}
}

func TestResumeWorkspaceCleanup_LegacyAmbiguousWorkspaceGone(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	if err := os.WriteFile(prunedWorkspaceRetryMarkerPath(ws), []byte("pruned workspace cleanup pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, marked, err := readWorkspaceCleanupState(ws)
	if err != nil || !marked || !state.LegacyAmbiguous {
		t.Fatalf("setup: (%+v, %v, %v)", state, marked, err)
	}

	if err := resumeWorkspaceCleanup(context.Background(), dir, ws, state); err != nil {
		t.Fatalf("resumeWorkspaceCleanup error = %v", err)
	}
	if _, marked, err := readWorkspaceCleanupState(ws); err != nil || marked {
		t.Fatalf("expected legacy marker cleared, marked=%v err=%v", marked, err)
	}
}

func TestResumeWorkspaceCleanup_LegacyAmbiguousWorkspaceAliveFails(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	state := workspaceCleanupState{LegacyAmbiguous: true}

	if err := resumeWorkspaceCleanup(context.Background(), dir, ws, state); err == nil {
		t.Fatal("expected legacy marker with live workspace to require manual cleanup")
	}
}

// The finalize phase is the persist-vs-clear branch that decides whether an
// interrupted cleanup is declared done or must retry. A wrong choice here
// leaks a git worktree, so the branch is exercised directly at the FSM
// boundary (cleanupFinalizeStep) rather than only end-to-end through
// RemoveWorkspace.

// Outcome (1a): the unregister is satisfied and nothing else is pending, so
// finalize clears the marker and declares the run done.
func TestCleanupFinalizeStep_ClearsMarkerWhenDone(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	// A surviving marker from an earlier phase that finalize must clear.
	if err := writeWorkspaceCleanupState(ws, workspaceCleanupState{NeedsUnregister: true}); err != nil {
		t.Fatal(err)
	}

	run := &workspaceCleanupRun{
		ctx:                 context.Background(),
		repoPath:            dir,
		workspacePath:       ws,
		state:               workspaceCleanupState{NeedsUnregister: false},
		unregisterSatisfied: true,
	}
	next, err := cleanupFinalizeStep(run)
	if err != nil {
		t.Fatalf("cleanupFinalizeStep error = %v", err)
	}
	if next != cleanupPhaseDone {
		t.Fatalf("expected finalize -> cleanupPhaseDone, got phase %d", next)
	}
	if _, marked, err := readWorkspaceCleanupState(ws); err != nil || marked {
		t.Fatalf("expected marker cleared after clean finalize, marked=%v err=%v", marked, err)
	}
}

// Outcome (1b): the unregister could not be satisfied, so finalize persists
// the deferred-unregister state (NeedsUnregister=true) and reports the run as
// still pending. A fresh resume from the persisted marker must then recompute
// and retry instead of declaring the worktree gone.
func TestCleanupFinalizeStep_PersistsDeferredUnregister(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")

	run := &workspaceCleanupRun{
		ctx:                 context.Background(),
		repoPath:            dir,
		workspacePath:       ws,
		state:               workspaceCleanupState{NeedsUnregister: true},
		unregisterSatisfied: false,
	}
	next, err := cleanupFinalizeStep(run)
	if next != cleanupPhaseDone {
		t.Fatalf("expected finalize -> cleanupPhaseDone, got phase %d", next)
	}
	if !IsWorkspaceCleanupPendingError(err) {
		t.Fatalf("expected workspace-cleanup-pending error, got %v", err)
	}
	// The persisted marker keeps the unregister retry alive on the next resume.
	persisted, marked, readErr := readWorkspaceCleanupState(ws)
	if readErr != nil || !marked {
		t.Fatalf("expected pending marker persisted, marked=%v err=%v", marked, readErr)
	}
	if !persisted.NeedsUnregister {
		t.Fatalf("expected persisted state to keep NeedsUnregister, got %+v", persisted)
	}
}

// Outcome (2): the finalize persist write fails (e.g. interrupted before the
// marker rewrite). The in-memory state must not be trusted — the on-disk
// marker is whatever it was before — so a subsequent resume recomputes from
// disk and retries instead of treating the worktree as cleaned up.
func TestCleanupFinalizeStep_WriteFailureLeavesDiskMarkerForRetry(t *testing.T) {
	origWriteRetryMarkerFile := writeRetryMarkerFile
	defer func() { writeRetryMarkerFile = origWriteRetryMarkerFile }()

	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	// The pending state an earlier phase left on disk (unregister still owed).
	preState := workspaceCleanupState{NeedsUnregister: true}
	if err := writeWorkspaceCleanupState(ws, preState); err != nil {
		t.Fatal(err)
	}

	writeRetryMarkerFile = func(string, []byte, os.FileMode) error {
		return errors.New("marker write failed")
	}

	// The unregister is still unsatisfied, so finalize takes the persist branch
	// (writePendingWorkspaceCleanupState -> writeRetryMarkerFile), which fails.
	run := &workspaceCleanupRun{
		ctx:                 context.Background(),
		repoPath:            dir,
		workspacePath:       ws,
		state:               workspaceCleanupState{NeedsUnregister: true},
		unregisterSatisfied: false,
	}
	next, err := cleanupFinalizeStep(run)
	if next != cleanupPhaseDone {
		t.Fatalf("expected finalize -> cleanupPhaseDone, got phase %d", next)
	}
	if err == nil || err.Error() != "marker write failed" {
		t.Fatalf("expected marker write failure to propagate, got %v", err)
	}

	// On-disk marker must still report the pending unregister: the failed write
	// did not advance the recorded state, so the next resume can recompute.
	persisted, marked, readErr := readWorkspaceCleanupState(ws)
	if readErr != nil || !marked {
		t.Fatalf("expected pending marker to survive failed finalize, marked=%v err=%v", marked, readErr)
	}
	if !persisted.NeedsUnregister {
		t.Fatalf("expected disk state to still need unregister after failed write, got %+v", persisted)
	}

	// Restore the write seam and model the next resume: the FSM recomputes from
	// the persisted disk state and the unregister now succeeds on retry. The
	// retried finalize then clears the marker, declaring the cleanup done — the
	// worktree is not leaked.
	writeRetryMarkerFile = origWriteRetryMarkerFile
	retry := &workspaceCleanupRun{
		ctx:                 context.Background(),
		repoPath:            dir,
		workspacePath:       ws,
		state:               persisted,
		unregisterSatisfied: true,
	}
	if next, err := cleanupFinalizeStep(retry); err != nil || next != cleanupPhaseDone {
		t.Fatalf("retried finalize: next=%d err=%v", next, err)
	}
	if _, marked, err := readWorkspaceCleanupState(ws); err != nil || marked {
		t.Fatalf("expected marker cleared after recompute+retry, marked=%v err=%v", marked, err)
	}
}

func TestResumeWorkspaceCleanup_RefusesForeignStagePath(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	foreign := filepath.Join(dir, "not-a-prune-dir")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	state := workspaceCleanupState{CleanupPath: foreign}

	if err := resumeWorkspaceCleanup(context.Background(), dir, ws, state); err == nil {
		t.Fatal("expected unreserved cleanup path to be refused")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("expected foreign path untouched, err=%v", err)
	}
}
