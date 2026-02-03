package config

import (
	"os"
	"path/filepath"
)

// Paths holds all the file system paths used by the application
type Paths struct {
	Home                  string // ~/.amux
	WorkspacesRoot        string // ~/.amux/workspaces
	RegistryPath          string // ~/.amux/projects.json
	MetadataRoot          string // ~/.amux/workspaces-metadata
	ConfigPath            string // ~/.amux/config.json
	ProfilesRoot          string // ~/.amux/profiles
	SharedProfileRoot     string // ~/.amux/profiles/shared
	GlobalPermissionsPath string // ~/.amux/global_permissions.json
}

// DefaultPaths returns the default paths configuration
func DefaultPaths() (*Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	amuxHome := filepath.Join(home, ".amux")

	profilesRoot := filepath.Join(amuxHome, "profiles")
	return &Paths{
		Home:                  amuxHome,
		WorkspacesRoot:        filepath.Join(amuxHome, "workspaces"),
		RegistryPath:          filepath.Join(amuxHome, "projects.json"),
		MetadataRoot:          filepath.Join(amuxHome, "workspaces-metadata"),
		ConfigPath:            filepath.Join(amuxHome, "config.json"),
		ProfilesRoot:          profilesRoot,
		SharedProfileRoot:     filepath.Join(profilesRoot, "shared"),
		GlobalPermissionsPath: filepath.Join(amuxHome, "global_permissions.json"),
	}, nil
}

// EnsureDirectories creates all required directories if they don't exist
func (p *Paths) EnsureDirectories() error {
	dirs := []string{
		p.Home,
		p.WorkspacesRoot,
		p.MetadataRoot,
		p.ProfilesRoot,
		filepath.Join(p.SharedProfileRoot, "skills"),
		filepath.Join(p.SharedProfileRoot, "plugins"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return nil
}
