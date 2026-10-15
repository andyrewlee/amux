package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGetUntrackedFileContent_RegularFile is the positive control: a plain
// untracked file still previews its content through --no-index.
func TestGetUntrackedFileContent_RegularFile(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := GetUntrackedFileContent(root, "new.txt")
	if err != nil {
		t.Fatalf("GetUntrackedFileContent: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("regular file preview errored: %s", res.Error)
	}
	if !strings.Contains(res.Content, "hello") {
		t.Fatalf("preview content = %q, want the file's bytes", res.Content)
	}
}

// TestGetUntrackedFileContent_SymlinkRefused is the exploit case: a
// repo-shipped untracked symlink renders its target's contents — the
// preview must refuse non-regular files under the repo root.
func TestGetUntrackedFileContent_SymlinkRefused(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("top secret\n"), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	res, err := GetUntrackedFileContent(root, "link.txt")
	if err != nil {
		t.Fatalf("GetUntrackedFileContent: %v", err)
	}
	if strings.Contains(res.Content, "top secret") {
		t.Fatal("symlink target content leaked into the preview")
	}
	if res.Error == "" && !res.Empty {
		t.Fatal("symlink preview should be an explicit skip, not a silent diff")
	}
}

// TestGetUntrackedFileContent_DanglingSymlink: a symlink with no target
// isn't regular — same skip, no panic.
func TestGetUntrackedFileContent_DanglingSymlink(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)
	if err := os.Symlink(filepath.Join(t.TempDir(), "nope"), filepath.Join(root, "dangling")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	res, err := GetUntrackedFileContent(root, "dangling")
	if err != nil {
		t.Fatalf("GetUntrackedFileContent: %v", err)
	}
	if res.Error == "" && !res.Empty {
		t.Fatal("dangling symlink preview should be an explicit skip")
	}
}

// TestGetUntrackedFileContent_EscapingPath: a porcelain path that escapes
// the repo root must not preview an outside file.
func TestGetUntrackedFileContent_EscapingPath(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)
	outside := filepath.Join(t.TempDir(), "escape.txt")
	if err := os.WriteFile(outside, []byte("escape hatch\n"), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	rel, err := filepath.Rel(root, outside)
	if err != nil || !strings.HasPrefix(rel, "..") {
		t.Skipf("cannot construct an escaping rel path: %v", err)
	}
	res, err := GetUntrackedFileContent(root, filepath.ToSlash(rel))
	if err != nil {
		t.Fatalf("GetUntrackedFileContent: %v", err)
	}
	if strings.Contains(res.Content, "escape hatch") {
		t.Fatal("path escaping the repo root leaked content")
	}
}
