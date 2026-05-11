package git

import (
	"errors"
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

func TestWriteRetryMarkerFileAtomicallyForWindowsReplacesExistingMarker(t *testing.T) {
	markerPath := filepath.Join(t.TempDir(), ".pending-cleanup.amux-pruned-worktree")
	if err := os.WriteFile(markerPath, []byte("repo_path=/tmp/old\ncleanup_path=/tmp/old-staged\nneeds_unregister=true\nworkspace_git_ref=\nworkspace_git_ref_mtime_unix_nano=0\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(markerPath) error = %v", err)
	}

	payload := []byte("repo_path=/tmp/new\ncleanup_path=/tmp/new-staged\nneeds_unregister=false\nworkspace_git_ref=\nworkspace_git_ref_mtime_unix_nano=0\n")
	if err := writeRetryMarkerFileAtomicallyForGOOS("windows", markerPath, payload, 0o600); err != nil {
		t.Fatalf("writeRetryMarkerFileAtomicallyForGOOS() error = %v", err)
	}

	got, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("ReadFile(markerPath) error = %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("marker contents = %q, want %q", string(got), string(payload))
	}
	if _, err := os.Stat(retryMarkerBackupPath(markerPath)); !os.IsNotExist(err) {
		t.Fatalf("expected backup marker to be removed, err=%v", err)
	}
}

func TestWriteRetryMarkerFileAtomicallyForWindowsKeepsBackupOnlyMarkerOnFailure(t *testing.T) {
	origRename := writeRetryMarkerRenamePath
	origRemove := writeRetryMarkerRemovePath
	defer func() {
		writeRetryMarkerRenamePath = origRename
		writeRetryMarkerRemovePath = origRemove
	}()

	markerPath := filepath.Join(t.TempDir(), ".pending-cleanup.amux-pruned-worktree")
	backupPath := retryMarkerBackupPath(markerPath)
	backupPayload := []byte("repo_path=/tmp/old\ncleanup_path=/tmp/old-staged\nneeds_unregister=true\nworkspace_git_ref=\nworkspace_git_ref_mtime_unix_nano=0\n")
	if err := os.WriteFile(backupPath, backupPayload, 0o600); err != nil {
		t.Fatalf("WriteFile(backupPath) error = %v", err)
	}

	writeRetryMarkerRenamePath = func(oldPath, newPath string) error {
		if newPath == markerPath {
			return errors.New("rename failed")
		}
		return origRename(oldPath, newPath)
	}
	writeRetryMarkerRemovePath = origRemove

	err := writeRetryMarkerFileAtomicallyForGOOS(
		"windows",
		markerPath,
		[]byte("repo_path=/tmp/new\ncleanup_path=/tmp/new-staged\nneeds_unregister=false\nworkspace_git_ref=\nworkspace_git_ref_mtime_unix_nano=0\n"),
		0o600,
	)
	if err == nil {
		t.Fatal("expected writeRetryMarkerFileAtomicallyForGOOS() to fail")
	}

	got, readErr := os.ReadFile(backupPath)
	if readErr != nil {
		t.Fatalf("ReadFile(backupPath) error = %v", readErr)
	}
	if string(got) != string(backupPayload) {
		t.Fatalf("backup marker contents = %q, want %q", string(got), string(backupPayload))
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
