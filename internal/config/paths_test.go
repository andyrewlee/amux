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
