package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathsEnsureDirectories(t *testing.T) {
	tmp := t.TempDir()
	paths := &Paths{
		Home:           filepath.Join(tmp, "medusa"),
		WorkspacesRoot: filepath.Join(tmp, "medusa", "workspaces"),
		RegistryPath:   filepath.Join(tmp, "medusa", "projects.json"),
		MetadataRoot:   filepath.Join(tmp, "medusa", "workspaces-metadata"),
		ConfigPath:     filepath.Join(tmp, "medusa", "config.json"),
		ProfilesRoot:   filepath.Join(tmp, "medusa", "profiles"),
	}

	if err := paths.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}

	for _, dir := range []string{paths.Home, paths.WorkspacesRoot, paths.MetadataRoot, paths.ProfilesRoot} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected directory %s to exist: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", dir)
		}
	}
}
