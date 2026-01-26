package config

import (
	"os"
	"path/filepath"
)

// Paths holds all the file system paths used by the application
type Paths struct {
	Home           string // ~/.amux
	WorkspacesRoot string // ~/.amux/workspaces
	RegistryPath   string // ~/.amux/projects.json
	MetadataRoot   string // ~/.amux/workspaces-metadata
	ConfigPath     string // ~/.amux/config.json
}

// DefaultPaths returns the default paths configuration
func DefaultPaths() (*Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	amuxHome := filepath.Join(home, ".amux")

	return &Paths{
		Home:           amuxHome,
		WorkspacesRoot: filepath.Join(amuxHome, "workspaces"),
		RegistryPath:   filepath.Join(amuxHome, "projects.json"),
		MetadataRoot:   filepath.Join(amuxHome, "workspaces-metadata"),
		ConfigPath:     filepath.Join(amuxHome, "config.json"),
	}, nil
}

// EnsureDirectories creates all required directories if they don't exist
func (p *Paths) EnsureDirectories() error {
	dirs := []string{
		p.Home,
		p.WorkspacesRoot,
		p.MetadataRoot,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return nil
}
