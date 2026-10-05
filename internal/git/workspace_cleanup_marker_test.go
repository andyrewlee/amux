package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadWorkspaceCleanupStateFallsBackToBackupMarker(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "pending-cleanup")
	markerPath := prunedWorkspaceRetryMarkerPath(workspacePath)
	backupPath := retryMarkerBackupPath(markerPath)
	want := []byte("repo_path=/tmp/repo-a\ncleanup_path=/tmp/staged\nneeds_unregister=true\nworkspace_git_ref=\nworkspace_git_ref_mtime_unix_nano=0\n")
	if err := os.WriteFile(backupPath, want, 0o600); err != nil {
		t.Fatalf("WriteFile(backupPath) error = %v", err)
	}

	got, marked, err := readWorkspaceCleanupState(workspacePath)
	if err != nil {
		t.Fatalf("readWorkspaceCleanupState() error = %v", err)
	}
	if !marked {
		t.Fatal("expected cleanup marker to be present via backup")
	}
	if got.RepoPath != "/tmp/repo-a" || got.CleanupPath != "/tmp/staged" || !got.NeedsUnregister {
		t.Fatalf("unexpected cleanup state from backup: %+v", got)
	}
}

// The marker write now routes through fsatomic.WriteFile — file Sync before
// rename and parent-dir Sync after (Windows backup-shuffle included) are
// covered by fsatomic's own suite; what remains here is the contract the git
// package itself owns: the marker round-trips through the durable path.
func TestWriteWorkspaceCleanupRetryMetadataRoundTripsThroughAtomicPath(t *testing.T) {
	workspacePath := t.TempDir()
	got, err := ensureWorkspaceCleanupRetryMetadataWithContext(t.Context(), workspacePath, "/tmp/repo", true)
	if err != nil {
		t.Fatalf("ensureWorkspaceCleanupRetryMetadataWithContext() error = %v", err)
	}
	content, err := os.ReadFile(workspaceCleanupRetryMetadataPath(workspacePath))
	if err != nil {
		t.Fatalf("ReadFile(retry metadata) error = %v", err)
	}
	if !strings.Contains(string(content), "repo_path=/tmp/repo") || !strings.Contains(string(content), "needs_unregister=true") {
		t.Fatalf("retry metadata = %q, want repo_path + needs_unregister", string(content))
	}
	loaded, marked, err := readWorkspaceCleanupRetryMetadata(workspacePath)
	if err != nil {
		t.Fatalf("readWorkspaceCleanupRetryMetadata() error = %v", err)
	}
	if !marked || loaded.RepoPath != "/tmp/repo" || !loaded.NeedsUnregister || loaded.WorkspaceFingerprint != got.WorkspaceFingerprint {
		t.Fatalf("round-trip = marked=%v %+v, want repo + unregister + fingerprint %q", marked, loaded, got.WorkspaceFingerprint)
	}
}

func TestReadWorkspaceCleanupRetryMetadataRejectsEmptyFile(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "pending-cleanup")
	if err := os.MkdirAll(filepath.Dir(workspaceCleanupRetryMetadataPath(workspacePath)), 0o755); err != nil {
		t.Fatalf("MkdirAll(metadata dir) error = %v", err)
	}
	if err := os.WriteFile(workspaceCleanupRetryMetadataPath(workspacePath), nil, 0o600); err != nil {
		t.Fatalf("WriteFile(retry metadata) error = %v", err)
	}

	_, marked, err := readWorkspaceCleanupRetryMetadata(workspacePath)
	if err == nil {
		t.Fatal("expected empty retry metadata to be rejected")
	}
	if marked {
		t.Fatal("expected empty retry metadata to be treated as invalid, not marked")
	}
	if !strings.Contains(err.Error(), "empty workspace cleanup retry metadata") {
		t.Fatalf("expected empty retry metadata error, got %v", err)
	}
}
