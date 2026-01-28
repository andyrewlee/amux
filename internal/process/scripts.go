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

const configFilename = "workspaces.json"

// WorkspaceConfig holds per-project workspace configuration
type WorkspaceConfig struct {
	SetupWorkspace []string `json:"setup-workspace"`
	RunScript      string   `json:"run"`
	ArchiveScript  string   `json:"archive"`
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
	configPath := filepath.Join(repoPath, ".amux", configFilename)

	fileData, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return &WorkspaceConfig{}, nil
	}
	if err != nil {
		return nil, err
	}

	var config WorkspaceConfig
	if err := json.Unmarshal(fileData, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// RunSetup runs the setup scripts for a workspace
func (r *ScriptRunner) RunSetup(ws *data.Workspace) error {
	config, err := r.LoadConfig(ws.Repo)
	if err != nil {
		return err
	}

	env := r.envBuilder.BuildEnv(ws)

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
func (r *ScriptRunner) RunScript(ws *data.Workspace, scriptType ScriptType) (*exec.Cmd, error) {
	config, err := r.LoadConfig(ws.Repo)
	if err != nil {
		return nil, err
	}

	var cmdStr string
	switch scriptType {
	case ScriptRun:
		cmdStr = config.RunScript
		if cmdStr == "" {
			cmdStr = ws.Scripts.Run
		}
	case ScriptArchive:
		cmdStr = config.ArchiveScript
		if cmdStr == "" {
			cmdStr = ws.Scripts.Archive
		}
	}

	if cmdStr == "" {
		return nil, fmt.Errorf("no %s script configured", scriptType)
	}

	// Check for existing process in non-concurrent mode
	if ws.ScriptMode == "nonconcurrent" {
		_ = r.Stop(ws)
	}

	env := r.envBuilder.BuildEnv(ws)

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
