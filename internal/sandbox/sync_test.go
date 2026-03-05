package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyDownloadedWorkspacePrunesDeletedPaths(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(srcRoot, "keep.txt"), "new")
	mustWriteFile(t, filepath.Join(srcRoot, "nested", "renamed.txt"), "present")

	mustWriteFile(t, filepath.Join(dstRoot, "keep.txt"), "old")
	mustWriteFile(t, filepath.Join(dstRoot, "stale.txt"), "delete me")
	mustWriteFile(t, filepath.Join(dstRoot, "nested", "oldname.txt"), "delete me too")

	if err := applyDownloadedWorkspace(srcRoot, dstRoot, nil); err != nil {
		t.Fatalf("applyDownloadedWorkspace() error = %v", err)
	}

	if data, err := os.ReadFile(filepath.Join(dstRoot, "keep.txt")); err != nil {
		t.Fatalf("reading keep.txt failed: %v", err)
	} else if string(data) != "new" {
		t.Fatalf("keep.txt content = %q, want %q", string(data), "new")
	}

	if _, err := os.Stat(filepath.Join(dstRoot, "nested", "renamed.txt")); err != nil {
		t.Fatalf("expected renamed file to exist: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dstRoot, "stale.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale.txt removed, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dstRoot, "nested", "oldname.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected oldname.txt removed, got err=%v", err)
	}
}

func TestApplyDownloadedWorkspacePreservesIgnoredPaths(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(srcRoot, "app.txt"), "ok")
	mustWriteFile(t, filepath.Join(srcRoot, ".git", "HEAD"), "remote-head")
	mustWriteFile(t, filepath.Join(dstRoot, ".git", "config"), "keep local git metadata")

	if err := applyDownloadedWorkspace(srcRoot, dstRoot, []string{".git"}); err != nil {
		t.Fatalf("applyDownloadedWorkspace() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dstRoot, ".git", "config")); err != nil {
		t.Fatalf("expected ignored .git/config to remain, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dstRoot, ".git", "HEAD")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected ignored remote .git/HEAD not to be copied, got err=%v", err)
	}
}

func TestApplyDownloadedWorkspaceIgnoredFileDoesNotReplaceLocalIgnoredDirectory(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(srcRoot, ".git"), "remote-file")
	mustWriteFile(t, filepath.Join(dstRoot, ".git", "config"), "local-config")

	if err := applyDownloadedWorkspace(srcRoot, dstRoot, []string{".git"}); err != nil {
		t.Fatalf("applyDownloadedWorkspace() error = %v", err)
	}

	info, err := os.Stat(filepath.Join(dstRoot, ".git"))
	if err != nil {
		t.Fatalf("expected local .git to exist, got err=%v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected local .git to remain a directory")
	}
	if data, err := os.ReadFile(filepath.Join(dstRoot, ".git", "config")); err != nil {
		t.Fatalf("expected local .git/config to exist, got err=%v", err)
	} else if string(data) != "local-config" {
		t.Fatalf(".git/config content = %q, want %q", string(data), "local-config")
	}
}

func TestApplyDownloadedWorkspacePreservesIgnoredDescendantsUnderDeletedParent(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(srcRoot, "app.txt"), "ok")
	mustWriteFile(t, filepath.Join(dstRoot, "gone", "plain.txt"), "remove-me")
	mustWriteFile(t, filepath.Join(dstRoot, "gone", "node_modules", "cache.bin"), "keep-me")

	if err := applyDownloadedWorkspace(srcRoot, dstRoot, []string{"node_modules"}); err != nil {
		t.Fatalf("applyDownloadedWorkspace() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dstRoot, "gone", "plain.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected non-ignored file under deleted parent removed, got err=%v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dstRoot, "gone", "node_modules", "cache.bin")); err != nil {
		t.Fatalf("expected ignored descendant preserved, got err=%v", err)
	} else if string(data) != "keep-me" {
		t.Fatalf("ignored descendant content = %q, want %q", string(data), "keep-me")
	}
}

func TestGetIgnorePatternsForRootUsesProvidedRoot(t *testing.T) {
	localRoot := t.TempDir()
	remoteRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(localRoot, ".amuxignore"), "local-only\n")
	mustWriteFile(t, filepath.Join(remoteRoot, ".amuxignore"), "remote-only\n")

	got, err := getIgnorePatternsForRoot(remoteRoot, SyncOptions{
		Cwd:        localRoot,
		IncludeGit: false,
	})
	if err != nil {
		t.Fatalf("getIgnorePatternsForRoot() error = %v", err)
	}

	if !containsPattern(got, "remote-only") {
		t.Fatalf("expected remote-only pattern from provided root, got %v", got)
	}
	if containsPattern(got, "local-only") {
		t.Fatalf("did not expect stale local-only pattern, got %v", got)
	}
}

func TestApplyDownloadedWorkspaceHandlesUnreadableSnapshotFile(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()

	secretPath := filepath.Join(srcRoot, "secret.txt")
	mustWriteFile(t, secretPath, "top-secret")
	if err := os.Chmod(secretPath, 0o000); err != nil {
		t.Fatalf("chmod secret source file failed: %v", err)
	}

	if err := applyDownloadedWorkspace(srcRoot, dstRoot, nil); err != nil {
		t.Fatalf("applyDownloadedWorkspace() error = %v", err)
	}

	dstSecret := filepath.Join(dstRoot, "secret.txt")
	info, err := os.Stat(dstSecret)
	if err != nil {
		t.Fatalf("expected destination secret file, got err=%v", err)
	}
	if got := info.Mode().Perm(); got != 0o000 {
		t.Fatalf("destination mode = %03o, want 000", got)
	}

	if err := os.Chmod(dstSecret, 0o600); err != nil {
		t.Fatalf("chmod destination secret file failed: %v", err)
	}
	if data, err := os.ReadFile(dstSecret); err != nil {
		t.Fatalf("reading destination secret file failed: %v", err)
	} else if string(data) != "top-secret" {
		t.Fatalf("destination content = %q, want %q", string(data), "top-secret")
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s failed: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s failed: %v", path, err)
	}
}

func containsPattern(patterns []string, want string) bool {
	for _, pattern := range patterns {
		if pattern == want {
			return true
		}
	}
	return false
}
