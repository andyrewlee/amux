package process

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

// scriptStopTimeout is how long Stop waits for the background cmd.Wait monitor
// to observe process exit before escalating to a direct SIGKILL.
// Kept as a var so tests can shorten it.
var scriptStopTimeout = 5 * time.Second

func scriptWorkspaceKey(ws *data.Workspace) string {
	return data.NormalizePath(ws.Root)
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

func isBenignStopError(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return true
	}
	if isTypedProcessGoneError(err) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "process already finished") ||
		strings.Contains(msg, "no such process")
}

func (r *ScriptRunner) clearRunningEntry(key string) {
	r.mu.Lock()
	delete(r.running, key)
	r.mu.Unlock()
}

// WorkspaceConfig holds per-project workspace configuration
type WorkspaceConfig struct {
	SetupWorkspace []string `json:"setup-workspace"`
	RunScript      string   `json:"run"`
	ArchiveScript  string   `json:"archive"`
}

// ScriptRunner manages script execution for workspaces
type ScriptRunner struct {
	mu               sync.Mutex
	portAllocator    *PortAllocator
	envBuilder       *EnvBuilder
	running          map[string]*runningScript // workspace root -> running process
	killProcessGroup func(pid int, opts KillOptions) error
}

type runningScript struct {
	cmd  *exec.Cmd
	done chan struct{}
}

// NewScriptRunner creates a new script runner
func NewScriptRunner(portStart, portRange int) *ScriptRunner {
	ports := NewPortAllocator(portStart, portRange)
	return &ScriptRunner{
		portAllocator:    ports,
		envBuilder:       NewEnvBuilder(ports),
		running:          make(map[string]*runningScript),
		killProcessGroup: KillProcessGroup,
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
		if err := r.Stop(ws); !isBenignStopError(err) {
			return nil, err
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

	running := &runningScript{
		cmd:  cmd,
		done: make(chan struct{}),
	}
	key := scriptWorkspaceKey(ws)
	r.mu.Lock()
	r.running[key] = running
	r.mu.Unlock()

	// Monitor in background
	safego.Go("process.script_wait", func() {
		defer close(running.done)
		if err := cmd.Wait(); err != nil {
			slog.Debug("script process exited with error", "error", err)
		}
		r.mu.Lock()
		if current, ok := r.running[key]; ok && current == running {
			delete(r.running, key)
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

	if running.cmd != nil && running.cmd.Process != nil {
		pid := running.cmd.Process.Pid
		err := r.killProcessGroup(pid, KillOptions{})
		if isBenignStopError(err) {
			r.clearRunningEntry(key)
			return nil
		}
		if err != nil {
			return err
		}
		if running.done == nil {
			r.clearRunningEntry(key)
			return nil
		}
		// Wait briefly for the background cmd.Wait monitor to observe exit,
		// then escalate to SIGKILL if needed.
		select {
		case <-running.done:
			r.clearRunningEntry(key)
		case <-time.After(scriptStopTimeout):
			_ = ForceKillProcess(pid)
			r.clearRunningEntry(key)
		}
	}

	return nil
}

// IsRunning checks if a script is running for a workspace
func (r *ScriptRunner) IsRunning(ws *data.Workspace) bool {
	if validateScriptWorkspace(ws) != nil {
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
			if err := KillProcessGroup(entry.cmd.Process.Pid, KillOptions{}); err != nil {
				slog.Debug("best-effort process group kill failed", "pid", entry.cmd.Process.Pid, "error", err)
			}
		}
	}
}
