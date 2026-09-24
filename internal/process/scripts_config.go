package process

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LoadConfig loads the workspace configuration from the repo
func (r *ScriptRunner) LoadConfig(repoPath string) (*WorkspaceConfig, error) {
	config, _, err := r.loadConfigRaw(repoPath)
	return config, err
}

// loadConfigRaw loads the workspace configuration and also returns the raw file
// bytes, so the trust check can hash exactly what was parsed without a second
// disk read. A missing file yields an empty config and nil bytes (nothing to
// trust or run).
func (r *ScriptRunner) loadConfigRaw(repoPath string) (*WorkspaceConfig, []byte, error) {
	fileData, err := readWorkspaceConfigFile(repoPath)
	if os.IsNotExist(err) {
		return &WorkspaceConfig{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	var config WorkspaceConfig
	if err := json.Unmarshal(fileData, &config); err != nil {
		return nil, nil, err
	}
	return &config, fileData, nil
}

func readWorkspaceConfigFile(repoPath string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Join(repoPath, ".amux"))
	if err != nil {
		return nil, err
	}
	data, readErr := root.ReadFile(configFilename)
	closeErr := root.Close()
	if readErr != nil {
		if closeErr != nil {
			return nil, errors.Join(readErr, fmt.Errorf("close workspace config directory: %w", closeErr))
		}
		return nil, readErr
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close workspace config directory: %w", closeErr)
	}
	return data, nil
}

// RunSetup runs the setup scripts for a workspace
