package process

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

var killProcessGroupFn = KillProcessGroup

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
	running       map[string]*runningScript // workspace root -> running process
}

type runningScript struct {
	cmd *exec.Cmd
}

func scriptWorkspaceKey(ws *data.Workspace) string {
	key := data.NormalizePath(strings.TrimSpace(ws.Root))
	if key == "" {
		key = strings.TrimSpace(ws.Root)
	}
	return key
}

// NewScriptRunner creates a new script runner
func NewScriptRunner(portStart, portRange int) *ScriptRunner {
	ports := NewPortAllocator(portStart, portRange)
	return &ScriptRunner{
		portAllocator: ports,
		envBuilder:    NewEnvBuilder(ports),
		running:       make(map[string]*runningScript),
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
	if err := validateScriptWorkspace(ws); err != nil {
		return err
	}
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
		SetProcessGroup(cmd)

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("setup command failed: %s: %s: %w", cmdStr, stderr.String(), err)
		}
	}

	return nil
}

// RunScript runs a script for a workspace
func (r *ScriptRunner) RunScript(ws *data.Workspace, scriptType ScriptType) (*exec.Cmd, error) {
	if err := validateScriptWorkspace(ws); err != nil {
		return nil, err
	}
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
		if err := r.Stop(ws); err != nil {
			return nil, fmt.Errorf("stopping existing script: %w", err)
		}
	}

	env := r.envBuilder.BuildEnv(ws)

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = ws.Root
	cmd.Env = env
	SetProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	running := &runningScript{cmd: cmd}
	root := scriptWorkspaceKey(ws)
	r.mu.Lock()
	r.running[root] = running
	r.mu.Unlock()

	// Monitor in background
	safego.Go("process.script_wait", func() {
		_ = cmd.Wait()
		r.mu.Lock()
		if current, ok := r.running[root]; ok && current == running {
			delete(r.running, root)
		}
		r.mu.Unlock()
	})

	return cmd, nil
}

// Stop stops the running script for a workspace
func (r *ScriptRunner) Stop(ws *data.Workspace) error {
	if err := validateScriptWorkspace(ws); err != nil {
		return err
	}
	key := scriptWorkspaceKey(ws)
	r.mu.Lock()
	running, ok := r.running[key]
	r.mu.Unlock()

	if !ok {
		return nil
	}

	if running.cmd == nil || running.cmd.Process == nil {
		r.clearRunningEntry(key, running)
		return nil
	}

	if err := killProcessGroupFn(running.cmd.Process.Pid, KillOptions{}); err != nil {
		if isBenignStopError(err) {
			r.clearRunningEntry(key, running)
			return nil
		}
		return err
	}
	return nil
}

// IsRunning checks if a script is running for a workspace
func (r *ScriptRunner) IsRunning(ws *data.Workspace) bool {
	if err := validateScriptWorkspace(ws); err != nil {
		return false
	}
	key := scriptWorkspaceKey(ws)
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[key]
	return ok
}

// StopAll stops all running scripts
func (r *ScriptRunner) StopAll() {
	r.mu.Lock()
	running := make([]*runningScript, 0, len(r.running))
	for _, entry := range r.running {
		running = append(running, entry)
	}
	r.running = make(map[string]*runningScript)
	r.mu.Unlock()

	for _, entry := range running {
		if entry.cmd != nil && entry.cmd.Process != nil {
			_ = KillProcessGroup(entry.cmd.Process.Pid, KillOptions{})
		}
	}
}

func (r *ScriptRunner) clearRunningEntry(key string, expected *runningScript) {
	r.mu.Lock()
	if current, ok := r.running[key]; ok && current == expected {
		delete(r.running, key)
	}
	r.mu.Unlock()
}

func isBenignStopError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrProcessDone) {
		return true
	}
	if isTypedProcessGoneError(err) {
		return true
	}
	// Fallback for wrapped platform-specific process errors surfaced as strings.
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "process already finished") ||
		strings.Contains(lower, "already exited") ||
		strings.Contains(lower, "no such process")
}

func validateScriptWorkspace(ws *data.Workspace) error {
	if ws == nil {
		return errors.New("workspace is required")
	}
	if strings.TrimSpace(ws.Repo) == "" {
		return errors.New("workspace repo is required")
	}
	if strings.TrimSpace(ws.Root) == "" {
		return errors.New("workspace root is required")
	}
	return nil
}
