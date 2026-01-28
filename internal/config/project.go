package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// ProjectConfig defines per-repo configuration.
type ProjectConfig struct {
	Tracker         string   `json:"tracker"`
	BranchPrefix    string   `json:"branchPrefix"`
	SetupScripts    []string `json:"setupScripts"`
	RunScript       string   `json:"runScript"`
	ArchiveScript   string   `json:"archiveScript,omitempty"`
	LinearTeamKey   string   `json:"linearTeamKey,omitempty"`
	LinearProjectID string   `json:"linearProjectId,omitempty"`
}

// DefaultProjectConfig returns defaults.
func DefaultProjectConfig() *ProjectConfig {
	return &ProjectConfig{
		Tracker:       "none",
		BranchPrefix:  "",
		SetupScripts:  []string{},
		RunScript:     "",
		ArchiveScript: "",
	}
}

// LoadProjectConfig loads .amux/project.json (preferred) or falls back to .amux/worktrees.json.
func LoadProjectConfig(repoPath string) (*ProjectConfig, error) {
	projectPath := filepath.Join(repoPath, ".amux", "project.json")
	if data, err := os.ReadFile(projectPath); err == nil {
		cfg := DefaultProjectConfig()
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
		if cfg.Tracker == "" {
			cfg.Tracker = "none"
		}
		return cfg, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// Fallback to legacy worktrees.json.
	legacyPath := filepath.Join(repoPath, ".amux", "worktrees.json")
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultProjectConfig(), nil
		}
		return nil, err
	}
	var legacy struct {
		SetupWorktree []string `json:"setup-worktree"`
		RunScript     string   `json:"run"`
		ArchiveScript string   `json:"archive"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, err
	}
	return &ProjectConfig{
		Tracker:       "none",
		BranchPrefix:  "",
		SetupScripts:  legacy.SetupWorktree,
		RunScript:     legacy.RunScript,
		ArchiveScript: legacy.ArchiveScript,
	}, nil
}
