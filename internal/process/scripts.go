package process

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

const (
	defaultSetupTimeout   = 2 * time.Minute
	defaultRunTimeout     = 0
	defaultArchiveTimeout = 2 * time.Minute
)

// WorkspaceConfig holds per-project workspace configuration
type WorkspaceConfig struct {
	SetupWorkspace        []string `json:"setup-workspace"`
	RunScript             string   `json:"run"`
	ArchiveScript         string   `json:"archive"`
	SetupTimeoutSeconds   int      `json:"setup-timeout-seconds"`
	RunTimeoutSeconds     int      `json:"run-timeout-seconds"`
	ArchiveTimeoutSeconds int      `json:"archive-timeout-seconds"`
}

// ScriptRunner manages script execution for workspaces
type ScriptRunner struct {
	mu            sync.Mutex
	portAllocator *PortAllocator
	envBuilder    *EnvBuilder
	running       map[string]*runningScript // workspace root -> running process
}

type runningScript struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	ctx    context.Context
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
	config, err := r.LoadConfig(ws.Repo)
	if err != nil {
		return err
	}

	env := r.envBuilder.BuildEnv(ws)
	timeout := setupTimeout(config)

	// Run each setup command sequentially
	for _, cmdStr := range config.SetupWorkspace {
		ctx, cancel := contextWithTimeout(timeout)
		cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
		cmd.Dir = ws.Root
		cmd.Env = env

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			cancel()
			if errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("setup command timed out after %s: %s: %w", timeout, cmdStr, context.DeadlineExceeded)
			}
			return fmt.Errorf("setup command failed: %s: %s: %w", cmdStr, stderr.String(), err)
		}
		cancel()
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

	timeout := scriptTimeout(config, scriptType)
	ctx, cancel := contextWithTimeout(timeout)
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = ws.Root
	cmd.Env = env
	SetProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	running := &runningScript{cmd: cmd, cancel: cancel, ctx: ctx}
	root := ws.Root
	r.mu.Lock()
	r.running[root] = running
	r.mu.Unlock()

	// Monitor in background
	safego.Go("process.script_wait", func() {
		_ = cmd.Wait()
		if ctx.Err() == context.DeadlineExceeded {
			if cmd.Process != nil {
				_ = KillProcessGroup(cmd.Process.Pid, KillOptions{})
			}
		}
		cancel()
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
	r.mu.Lock()
	running, ok := r.running[ws.Root]
	r.mu.Unlock()

	if !ok {
		return nil
	}

	if running.cancel != nil {
		running.cancel()
	}
	if running.cmd != nil && running.cmd.Process != nil {
		return KillProcessGroup(running.cmd.Process.Pid, KillOptions{})
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
	running := make([]*runningScript, 0, len(r.running))
	for _, entry := range r.running {
		running = append(running, entry)
	}
	r.running = make(map[string]*runningScript)
	r.mu.Unlock()

	for _, entry := range running {
		if entry.cancel != nil {
			entry.cancel()
		}
		if entry.cmd != nil && entry.cmd.Process != nil {
			_ = KillProcessGroup(entry.cmd.Process.Pid, KillOptions{})
		}
	}
}

func setupTimeout(config *WorkspaceConfig) time.Duration {
	if config == nil {
		return defaultSetupTimeout
	}
	if config.SetupTimeoutSeconds == 0 {
		return defaultSetupTimeout
	}
	if config.SetupTimeoutSeconds < 0 {
		return 0
	}
	return time.Duration(config.SetupTimeoutSeconds) * time.Second
}

func scriptTimeout(config *WorkspaceConfig, scriptType ScriptType) time.Duration {
	if config == nil {
		switch scriptType {
		case ScriptArchive:
			return defaultArchiveTimeout
		case ScriptRun:
			return defaultRunTimeout
		default:
			return defaultSetupTimeout
		}
	}
	switch scriptType {
	case ScriptArchive:
		if config.ArchiveTimeoutSeconds == 0 {
			return defaultArchiveTimeout
		}
		if config.ArchiveTimeoutSeconds < 0 {
			return 0
		}
		return time.Duration(config.ArchiveTimeoutSeconds) * time.Second
	case ScriptRun:
		if config.RunTimeoutSeconds == 0 {
			return defaultRunTimeout
		}
		if config.RunTimeoutSeconds < 0 {
			return 0
		}
		return time.Duration(config.RunTimeoutSeconds) * time.Second
	default:
		return setupTimeout(config)
	}
}

func contextWithTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), timeout)
}
