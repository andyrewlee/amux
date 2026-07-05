//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteDumpFrameKeepsPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frame.txt")
	content := "\x1b[31msecret frame\x1b[0m"

	if err := writeDumpFrame(path, content); err != nil {
		t.Fatalf("writeDumpFrame() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != content {
		t.Fatalf("content = %q, want %q", got, content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Fatalf("expected no group/other permissions, got %03o", mode)
	}
}
