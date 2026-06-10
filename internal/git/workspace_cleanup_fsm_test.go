package git

import (
	"context"
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
