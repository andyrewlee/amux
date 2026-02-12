package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCleanupStaleWorkspacePathRemovesWorkspaceWithoutGitMetadata(t *testing.T) {
	root := t.TempDir()
	workspacePath := filepath.Join(root, "ws")
	if err := os.MkdirAll(filepath.Join(workspacePath, "subdir"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	if err := cleanupStaleWorkspacePath(workspacePath); err != nil {
		t.Fatalf("cleanupStaleWorkspacePath() error = %v", err)
	}

	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected workspace path to be removed, stat err=%v", err)
	}
}

func TestCleanupStaleWorkspacePathRejectsWhenGitMetadataExists(t *testing.T) {
	root := t.TempDir()
	workspacePath := filepath.Join(root, "ws")
	if err := os.MkdirAll(filepath.Join(workspacePath, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.git) error = %v", err)
	}

	err := cleanupStaleWorkspacePath(workspacePath)
	if err == nil {
		t.Fatal("expected cleanupStaleWorkspacePath() to fail when .git exists")
	}
	if !strings.Contains(err.Error(), "still has git metadata") {
		t.Fatalf("expected git-metadata error, got %v", err)
	}
	if _, statErr := os.Stat(workspacePath); statErr != nil {
		t.Fatalf("expected workspace path to remain, stat err=%v", statErr)
	}
}

func TestCleanupStaleWorkspacePathBlocksDeleteOnGitStatError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ENOTDIR behavior for file/.git paths is platform-specific on windows")
	}

	root := t.TempDir()
	workspacePath := filepath.Join(root, "ws-file")
	if err := os.WriteFile(workspacePath, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := cleanupStaleWorkspacePath(workspacePath)
	if err == nil {
		t.Fatal("expected cleanupStaleWorkspacePath() to fail on non-ENOENT git stat error")
	}
	if !strings.Contains(err.Error(), "failed to stat git metadata") {
		t.Fatalf("expected git stat error, got %v", err)
	}
	if _, statErr := os.Stat(workspacePath); statErr != nil {
		t.Fatalf("expected workspace file to remain after failed cleanup, stat err=%v", statErr)
	}
}
