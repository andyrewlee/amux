package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathsEnsureDirectories(t *testing.T) {
	tmp := t.TempDir()
	paths := &Paths{
		Home:             filepath.Join(tmp, "amux"),
		WorkspacesRoot:   filepath.Join(tmp, "amux", "workspaces"),
		RegistryPath:     filepath.Join(tmp, "amux", "projects.json"),
		MetadataRoot:     filepath.Join(tmp, "amux", "workspaces-metadata"),
		ConfigPath:       filepath.Join(tmp, "amux", "config.json"),
		CacheRoot:        filepath.Join(tmp, "amux", "cache"),
		LinearConfigPath: filepath.Join(tmp, "amux", "linear.json"),
		GitHubConfigPath: filepath.Join(tmp, "amux", "github.json"),
	}

	if err := paths.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}

	for _, dir := range []string{paths.Home, paths.WorkspacesRoot, paths.MetadataRoot, paths.CacheRoot} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected directory %s to exist: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", dir)
		}
	}
}
