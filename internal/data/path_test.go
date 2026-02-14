package data

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizePath_ReResolvesAfterPathCreated(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(realRoot) error = %v", err)
	}

	linkRoot := filepath.Join(base, "link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	missingViaLink := filepath.Join(linkRoot, "new-workspace")

	first := NormalizePath(missingViaLink)
	if first != filepath.Clean(missingViaLink) {
		t.Fatalf("first normalization = %q, want %q", first, filepath.Clean(missingViaLink))
	}

	realPath := filepath.Join(realRoot, "new-workspace")
	if err := os.MkdirAll(realPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(realPath) error = %v", err)
	}

	second := NormalizePath(missingViaLink)
	resolvedReal, err := filepath.EvalSymlinks(realPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(realPath) error = %v", err)
	}
	want := filepath.Clean(resolvedReal)
	if second != want {
		t.Fatalf("second normalization = %q, want %q", second, want)
	}
}

func TestNormalizePath_ReResolvesAfterSymlinkRetarget(t *testing.T) {
	base := t.TempDir()

	realA := filepath.Join(base, "real-a")
	realB := filepath.Join(base, "real-b")
	workspaceA := filepath.Join(realA, "workspace")
	workspaceB := filepath.Join(realB, "workspace")
	if err := os.MkdirAll(workspaceA, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspaceA) error = %v", err)
	}
	if err := os.MkdirAll(workspaceB, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspaceB) error = %v", err)
	}

	linkRoot := filepath.Join(base, "link-root")
	if err := os.Symlink(realA, linkRoot); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	pathViaLink := filepath.Join(linkRoot, "workspace")
	first := NormalizePath(pathViaLink)
	resolvedA, err := filepath.EvalSymlinks(workspaceA)
	if err != nil {
		t.Fatalf("EvalSymlinks(workspaceA) error = %v", err)
	}
	wantFirst := filepath.Clean(resolvedA)
	if first != wantFirst {
		t.Fatalf("first normalization = %q, want %q", first, wantFirst)
	}

	if err := os.Remove(linkRoot); err != nil {
		t.Fatalf("Remove(linkRoot) error = %v", err)
	}
	if err := os.Symlink(realB, linkRoot); err != nil {
		t.Fatalf("Symlink(realB) error = %v", err)
	}

	second := NormalizePath(pathViaLink)
	resolvedB, err := filepath.EvalSymlinks(workspaceB)
	if err != nil {
		t.Fatalf("EvalSymlinks(workspaceB) error = %v", err)
	}
	wantSecond := filepath.Clean(resolvedB)
	if second != wantSecond {
		t.Fatalf("second normalization = %q, want %q", second, wantSecond)
	}
}
