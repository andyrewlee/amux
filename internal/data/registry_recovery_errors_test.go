package data

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistry_LoadCorruptPrimaryIncludesBackupReadError(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	if err := os.WriteFile(registryPath, []byte("{broken"), 0o644); err != nil {
		t.Fatalf("write primary: %v", err)
	}

	r := NewRegistry(registryPath)
	_, err := r.Load()
	if err == nil {
		t.Fatalf("expected load error for corrupt primary")
	}
	msg := err.Error()
	if !strings.Contains(msg, "parse "+registryPath) {
		t.Fatalf("expected primary parse context in error, got: %v", err)
	}
	if !strings.Contains(msg, "read backup") {
		t.Fatalf("expected backup read context in error, got: %v", err)
	}
}

func TestRegistry_LoadCorruptPrimaryIncludesBackupParseError(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	backupPath := registryPath + ".bak"
	if err := os.WriteFile(registryPath, []byte("{broken"), 0o644); err != nil {
		t.Fatalf("write primary: %v", err)
	}
	if err := os.WriteFile(backupPath, []byte("{also-broken"), 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	r := NewRegistry(registryPath)
	_, err := r.Load()
	if err == nil {
		t.Fatalf("expected load error for corrupt primary+backup")
	}
	msg := err.Error()
	if !strings.Contains(msg, "parse "+registryPath) {
		t.Fatalf("expected primary parse context in error, got: %v", err)
	}
	if !strings.Contains(msg, "parse backup "+backupPath) {
		t.Fatalf("expected backup parse context in error, got: %v", err)
	}
}
