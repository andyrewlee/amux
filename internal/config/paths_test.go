package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathsEnsureDirectories(t *testing.T) {
	tmp := t.TempDir()
	paths := &Paths{
		Home:           filepath.Join(tmp, "amux"),
		WorkspacesRoot: filepath.Join(tmp, "amux", "workspaces"),
		RegistryPath:   filepath.Join(tmp, "amux", "projects.json"),
		MetadataRoot:   filepath.Join(tmp, "amux", "workspaces-metadata"),
		ConfigPath:     filepath.Join(tmp, "amux", "config.json"),
	}

	if err := paths.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}

	for _, dir := range []string{paths.Home, paths.WorkspacesRoot, paths.MetadataRoot} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected directory %s to exist: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", dir)
		}
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			t.Fatalf("expected %s to be private, got mode %03o", dir, mode)
		}
	}
}

// TestPathsEnsureDirectoriesTightensExisting proves a pre-existing permissive
// amux dir is chmodded to 0700 — MkdirAll alone leaves it at its old mode.
func TestPathsEnsureDirectoriesTightensExisting(t *testing.T) {
	tmp := t.TempDir()
	paths := &Paths{
		Home:           filepath.Join(tmp, "amux"),
		WorkspacesRoot: filepath.Join(tmp, "amux", "workspaces"),
		RegistryPath:   filepath.Join(tmp, "amux", "projects.json"),
		MetadataRoot:   filepath.Join(tmp, "amux", "workspaces-metadata"),
		ConfigPath:     filepath.Join(tmp, "amux", "config.json"),
	}
	for _, dir := range []string{paths.Home, paths.WorkspacesRoot, paths.MetadataRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("seed %s: %v", dir, err)
		}
	}

	if err := paths.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}
	for _, dir := range []string{paths.Home, paths.WorkspacesRoot, paths.MetadataRoot} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("Stat(%s): %v", dir, err)
		}
		if mode := info.Mode().Perm(); mode != 0o700 {
			t.Fatalf("expected %s tightened to 0700, got %03o", dir, mode)
		}
	}
}

// TestPathsEnsureDirectoriesLeavesRelocatedWorkspacesRoot proves the chmod
// does not reach a WorkspacesRoot relocated outside Home by
// AMUX_WORKSPACES_ROOT — that path may be shared or owned for other purposes.
func TestPathsEnsureDirectoriesLeavesRelocatedWorkspacesRoot(t *testing.T) {
	tmp := t.TempDir()
	outside := filepath.Join(tmp, "shared-worktrees")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("seed outside root: %v", err)
	}
	paths := &Paths{
		Home:           filepath.Join(tmp, "amux"),
		WorkspacesRoot: outside,
		RegistryPath:   filepath.Join(tmp, "amux", "projects.json"),
		MetadataRoot:   filepath.Join(tmp, "amux", "workspaces-metadata"),
		ConfigPath:     filepath.Join(tmp, "amux", "config.json"),
	}

	if err := paths.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}
	info, err := os.Stat(outside)
	if err != nil {
		t.Fatalf("Stat(%s): %v", outside, err)
	}
	if mode := info.Mode().Perm(); mode != 0o755 {
		t.Fatalf("relocated WorkspacesRoot must keep its mode, got %03o", mode)
	}
}

func TestDefaultPathsWorkspacesRootEnvOverride(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-workspaces")
	t.Setenv(WorkspacesRootEnvVar, custom)
	paths, err := DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths() error = %v", err)
	}
	if paths.WorkspacesRoot != custom {
		t.Errorf("WorkspacesRoot = %q, want env override %q", paths.WorkspacesRoot, custom)
	}

	t.Setenv(WorkspacesRootEnvVar, "   ")
	paths, err = DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths() error = %v", err)
	}
	if want := filepath.Join(paths.Home, "workspaces"); paths.WorkspacesRoot != want {
		t.Errorf("blank env should fall back: WorkspacesRoot = %q, want %q", paths.WorkspacesRoot, want)
	}
}
