package data

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCanonicalLookupPath_KeepsRelativeSymlinkPathRelative(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require elevated privileges on windows")
	}
	base := t.TempDir()
	realRepo := filepath.Join(base, "real", "repo")
	if err := os.MkdirAll(realRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(realRepo) error = %v", err)
	}
	if err := os.Symlink("real/repo", filepath.Join(base, "repo-link")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("Chdir(base) error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousWD) })

	got := canonicalLookupPath("./repo-link")
	want := "repo-link"
	if got != want {
		t.Fatalf("canonicalLookupPath(relative symlink) = %q, want %q", got, want)
	}
	if filepath.IsAbs(got) {
		t.Fatalf("canonicalLookupPath(relative symlink) should stay relative, got %q", got)
	}
}

func TestCanonicalLookupPath_ResolvesAbsoluteSymlinkPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require elevated privileges on windows")
	}
	base := t.TempDir()
	realRepo := filepath.Join(base, "real", "repo")
	if err := os.MkdirAll(realRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(realRepo) error = %v", err)
	}
	linkPath := filepath.Join(base, "repo-link")
	if err := os.Symlink(realRepo, linkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	got := canonicalLookupPath(linkPath)
	want := NormalizePath(realRepo)
	if got != want {
		t.Fatalf("canonicalLookupPath(absolute symlink) = %q, want %q", got, want)
	}
}
