package config

import (
	"os"
	"path/filepath"
)

// Paths holds all the file system paths used by the application
type Paths struct {
	Home                  string // ~/.medusa
	WorkspacesRoot        string // ~/.medusa/workspaces
	GroupsWorkspacesRoot  string // ~/.medusa/workspaces/groups
	RegistryPath          string // ~/.medusa/projects.json
	MetadataRoot          string // ~/.medusa/workspaces-metadata
	ConfigPath            string // ~/.medusa/config.json
	ProfilesRoot          string // ~/.medusa/profiles
	SharedProfileRoot     string // ~/.medusa/profiles/shared
	GlobalPermissionsPath string // ~/.medusa/global_permissions.json
	SandboxRulesPath      string // ~/.medusa/sandbox_rules.json
}

// DefaultPaths returns the default paths configuration
func DefaultPaths() (*Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	medusaHome := filepath.Join(home, ".medusa")

	profilesRoot := filepath.Join(medusaHome, "profiles")
	workspacesRoot := filepath.Join(medusaHome, "workspaces")
	return &Paths{
		Home:                  medusaHome,
		WorkspacesRoot:        workspacesRoot,
		GroupsWorkspacesRoot:  filepath.Join(workspacesRoot, "groups"),
		RegistryPath:          filepath.Join(medusaHome, "projects.json"),
		MetadataRoot:          filepath.Join(medusaHome, "workspaces-metadata"),
		ConfigPath:            filepath.Join(medusaHome, "config.json"),
		ProfilesRoot:          profilesRoot,
		SharedProfileRoot:     filepath.Join(profilesRoot, "shared"),
		GlobalPermissionsPath: filepath.Join(medusaHome, "global_permissions.json"),
		SandboxRulesPath:      filepath.Join(medusaHome, "sandbox_rules.json"),
	}, nil
}

// EnsureDirectories creates all required directories if they don't exist
func (p *Paths) EnsureDirectories() error {
	dirs := []string{
		p.Home,
		p.WorkspacesRoot,
		p.GroupsWorkspacesRoot,
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
