package config

import (
	"os"
	"path/filepath"
	"strings"
)

// WorkspacesRootEnvVar relocates the workspace worktree root. It is the only
// knob for this: there is no config-file key for workspaces_root. At startup
// amux re-exports the resolved root under the same name so leaf packages
// (e.g. internal/git) receive the effective value through the environment.
const WorkspacesRootEnvVar = "AMUX_WORKSPACES_ROOT"

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

	workspacesRoot := filepath.Join(amuxHome, "workspaces")
	if override := strings.TrimSpace(os.Getenv(WorkspacesRootEnvVar)); override != "" {
		workspacesRoot = override
	}

	return &Paths{
		Home:           amuxHome,
		WorkspacesRoot: workspacesRoot,
		RegistryPath:   filepath.Join(amuxHome, "projects.json"),
		MetadataRoot:   filepath.Join(amuxHome, "workspaces-metadata"),
		ConfigPath:     filepath.Join(amuxHome, "config.json"),
	}, nil
}

// EnsureDirectories creates all required directories if they don't exist.
// MkdirAll applies the requested mode only to directories it creates, so the
// amux-owned ones are chmodded on every call — a pre-existing permissive
// ~/.amux gets tightened rather than left as-is.
func (p *Paths) EnsureDirectories() error {
	dirs := []string{
		p.Home,
		p.WorkspacesRoot,
		p.MetadataRoot,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}

	// WorkspacesRoot qualifies for the tighten only while it lives under
	// Home: AMUX_WORKSPACES_ROOT can relocate it to a path the user owns for
	// other purposes, and silently chmodding that would surprise them.
	owned := []string{p.Home, p.MetadataRoot}
	if strings.HasPrefix(p.WorkspacesRoot, p.Home+string(os.PathSeparator)) {
		owned = append(owned, p.WorkspacesRoot)
	}
	for _, dir := range owned {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}

	return nil
}

// ExpandHomePath expands a leading "~" (or "~/" / "~\") to the user's home
// directory. Other prefixes pass through unchanged — "~other" user lookups are
// deliberately not supported. This is the single home-expansion helper; the
// filepicker/validation/workspacesvc sites predate it and are converged
// separately.
func ExpandHomePath(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch {
	case path == "~":
		return home, nil
	case strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\"):
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
