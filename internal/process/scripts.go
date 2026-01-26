package process

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/safego"
)

// ScriptType identifies the type of script
type ScriptType string

const (
	ScriptSetup   ScriptType = "setup"
	ScriptRun     ScriptType = "run"
	ScriptArchive ScriptType = "archive"
)

const (
	configFilename       = "workspaces.json"
	legacyConfigFilename = "worktrees.json"
)

// WorkspaceConfig holds per-project workspace configuration
type WorkspaceConfig struct {
	SetupWorkspace []string `json:"setup-workspace"`
	RunScript      string   `json:"run"`
	ArchiveScript  string   `json:"archive"`
}

// LegacyWorkspaceConfig for backward compatibility with setup-worktree key
type LegacyWorkspaceConfig struct {
	SetupWorktree []string `json:"setup-worktree"`
	RunScript     string   `json:"run"`
	ArchiveScript string   `json:"archive"`
}

// ScriptRunner manages script execution for workspaces
type ScriptRunner struct {
	mu            sync.Mutex
	portAllocator *PortAllocator
	envBuilder    *EnvBuilder
	running       map[string]*exec.Cmd // workspace root -> running process
}

// NewScriptRunner creates a new script runner
func NewScriptRunner(portStart, portRange int) *ScriptRunner {
	ports := NewPortAllocator(portStart, portRange)
	return &ScriptRunner{
		portAllocator: ports,
		envBuilder:    NewEnvBuilder(ports),
		running:       make(map[string]*exec.Cmd),
	}
}

// LoadConfig loads the workspace configuration from the repo
func (r *ScriptRunner) LoadConfig(repoPath string) (*WorkspaceConfig, error) {
	configDir := filepath.Join(repoPath, ".amux")
	newPath := filepath.Join(configDir, configFilename)
	legacyPath := filepath.Join(configDir, legacyConfigFilename)

	// Try new file first
	if fileData, err := os.ReadFile(newPath); err == nil {
		var config WorkspaceConfig
		if err := json.Unmarshal(fileData, &config); err != nil {
			return nil, err
		}
		// Also check legacy key in new file for migration
		if len(config.SetupWorkspace) == 0 {
			var legacy LegacyWorkspaceConfig
			if err := json.Unmarshal(fileData, &legacy); err == nil && len(legacy.SetupWorktree) > 0 {
				config.SetupWorkspace = legacy.SetupWorktree
			}
		}
		return &config, nil
	} else if !os.IsNotExist(err) {
		return nil, err // Real error
	}

	// Try legacy file
	if data, err := os.ReadFile(legacyPath); err == nil {
		// Try new keys first in legacy file
		var config WorkspaceConfig
		if err := json.Unmarshal(data, &config); err == nil && len(config.SetupWorkspace) > 0 {
			return &config, nil
		}
		// Fall back to legacy keys
		var legacy LegacyWorkspaceConfig
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, err
		}
		return &WorkspaceConfig{
			SetupWorkspace: legacy.SetupWorktree,
			RunScript:      legacy.RunScript,
			ArchiveScript:  legacy.ArchiveScript,
		}, nil
	} else if !os.IsNotExist(err) {
		return nil, err // Real error
	}

	// Neither exists
	return &WorkspaceConfig{}, nil
}

// RunSetup runs the setup scripts for a workspace
func (r *ScriptRunner) RunSetup(ws *data.Workspace, meta *data.Metadata) error {
	config, err := r.LoadConfig(ws.Repo)
	if err != nil {
		return err
	}

	env := r.envBuilder.BuildEnv(ws, meta)

	// Run each setup command sequentially
	for _, cmdStr := range config.SetupWorkspace {
		cmd := exec.Command("sh", "-c", cmdStr)
		cmd.Dir = ws.Root
		cmd.Env = env

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("setup command failed: %s: %s", cmdStr, stderr.String())
		}
	}

	return nil
}

// RunScript runs a script for a workspace
func (r *ScriptRunner) RunScript(ws *data.Workspace, meta *data.Metadata, scriptType ScriptType) (*exec.Cmd, error) {
	config, err := r.LoadConfig(ws.Repo)
	if err != nil {
		return nil, err
	}

	var cmdStr string
	switch scriptType {
	case ScriptRun:
		cmdStr = config.RunScript
		if cmdStr == "" && meta != nil {
			cmdStr = meta.Scripts.Run
		}
	case ScriptArchive:
		cmdStr = config.ArchiveScript
		if cmdStr == "" && meta != nil {
			cmdStr = meta.Scripts.Archive
		}
	}

	if cmdStr == "" {
		return nil, fmt.Errorf("no %s script configured", scriptType)
	}

	// Check for existing process in non-concurrent mode
	if meta != nil && meta.ScriptMode == "nonconcurrent" {
		_ = r.Stop(ws)
	}

	env := r.envBuilder.BuildEnv(ws, meta)

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = ws.Root
	cmd.Env = env
	SetProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.running[ws.Root] = cmd
	r.mu.Unlock()

	// Monitor in background
	safego.Go("process.script_wait", func() {
		_ = cmd.Wait()
		r.mu.Lock()
		delete(r.running, ws.Root)
		r.mu.Unlock()
	})

	return cmd, nil
}

// Stop stops the running script for a workspace
func (r *ScriptRunner) Stop(ws *data.Workspace) error {
	r.mu.Lock()
	cmd, ok := r.running[ws.Root]
	r.mu.Unlock()

	if !ok {
		return nil
	}

	if cmd.Process != nil {
		return KillProcessGroup(cmd.Process.Pid, KillOptions{})
	}

	return nil
}

// IsRunning checks if a script is running for a workspace
func (r *ScriptRunner) IsRunning(ws *data.Workspace) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[ws.Root]
	return ok
}

// StopAll stops all running scripts
func (r *ScriptRunner) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, cmd := range r.running {
		if cmd.Process != nil {
			_ = KillProcessGroup(cmd.Process.Pid, KillOptions{})
		}
	}
	r.running = make(map[string]*exec.Cmd)
}
