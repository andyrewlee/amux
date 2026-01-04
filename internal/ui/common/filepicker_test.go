package common

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilePickerEnterSelectsCurrentDirectory(t *testing.T) {
	tempDir := t.TempDir()
	fp := NewFilePicker("test", tempDir, true)

	fp.cursor = 0
	_, cmd := fp.handleEnter()
	if cmd == nil {
		t.Fatalf("expected selection command")
	}
	msg := cmd()
	result, ok := msg.(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", msg)
	}
	if !result.Confirmed {
		t.Fatalf("expected confirmed result")
	}
	if result.Value != tempDir {
		t.Fatalf("expected %q, got %q", tempDir, result.Value)
	}
}

func TestFilePickerEnterOpensDirectory(t *testing.T) {
	tempDir := t.TempDir()
	subdir := filepath.Join(tempDir, "child")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fp := NewFilePicker("test", tempDir, true)
	fp.cursor = 1
	_, cmd := fp.handleEnter()
	if cmd != nil {
		t.Fatalf("expected no command when opening directory")
	}
	if fp.currentPath != subdir {
		t.Fatalf("expected current path %q, got %q", subdir, fp.currentPath)
	}
}

func TestFilePickerEnterSelectsFileWhenAllowed(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "note.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	fp := NewFilePicker("test", tempDir, false)
	fp.cursor = 1
	_, cmd := fp.handleEnter()
	if cmd == nil {
		t.Fatalf("expected selection command for file")
	}
	msg := cmd()
	result, ok := msg.(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", msg)
	}
	if !result.Confirmed {
		t.Fatalf("expected confirmed result")
	}
	if result.Value != filePath {
		t.Fatalf("expected %q, got %q", filePath, result.Value)
	}
}
