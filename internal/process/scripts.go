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
)

// ScriptType identifies the type of script
type ScriptType string

const (
	ScriptSetup   ScriptType = "setup"
	ScriptRun     ScriptType = "run"
	ScriptArchive ScriptType = "archive"
)

// WorktreeConfig holds per-project worktree configuration
type WorktreeConfig struct {
	SetupWorktree []string `json:"setup-worktree"`
	RunScript     string   `json:"run"`
	ArchiveScript string   `json:"archive"`
}

// ScriptRunner manages script execution for worktrees
type ScriptRunner struct {
	mu            sync.Mutex
	portAllocator *PortAllocator
	envBuilder    *EnvBuilder
	running       map[string]*exec.Cmd // worktree root -> running process
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

// LoadConfig loads the worktree configuration from the repo
func (r *ScriptRunner) LoadConfig(repoPath string) (*WorktreeConfig, error) {
	configPath := filepath.Join(repoPath, ".amux", "worktrees.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &WorktreeConfig{}, nil
		}
		return nil, err
	}

	var config WorktreeConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// RunSetup runs the setup scripts for a worktree
func (r *ScriptRunner) RunSetup(wt *data.Worktree, meta *data.Metadata) error {
	config, err := r.LoadConfig(wt.Repo)
	if err != nil {
		return err
	}

	env := r.envBuilder.BuildEnv(wt, meta)

	// Run each setup command sequentially
	for _, cmdStr := range config.SetupWorktree {
		cmd := exec.Command("sh", "-c", cmdStr)
		cmd.Dir = wt.Root
		cmd.Env = env

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("setup command failed: %s: %s", cmdStr, stderr.String())
		}
	}

	return nil
}

// RunScript runs a script for a worktree
func (r *ScriptRunner) RunScript(wt *data.Worktree, meta *data.Metadata, scriptType ScriptType) (*exec.Cmd, error) {
	config, err := r.LoadConfig(wt.Repo)
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
		r.Stop(wt)
	}

	env := r.envBuilder.BuildEnv(wt, meta)

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = wt.Root
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.running[wt.Root] = cmd
	r.mu.Unlock()

	// Monitor in background
	go func() {
		cmd.Wait()
		r.mu.Lock()
		delete(r.running, wt.Root)
		r.mu.Unlock()
	}()

	return cmd, nil
}

// Stop stops the running script for a worktree
func (r *ScriptRunner) Stop(wt *data.Worktree) error {
	r.mu.Lock()
	cmd, ok := r.running[wt.Root]
	r.mu.Unlock()

	if !ok {
		return nil
	}

	if cmd.Process != nil {
		return cmd.Process.Kill()
	}

	return nil
}

// IsRunning checks if a script is running for a worktree
func (r *ScriptRunner) IsRunning(wt *data.Worktree) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[wt.Root]
	return ok
}

// StopAll stops all running scripts
func (r *ScriptRunner) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, cmd := range r.running {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}
	r.running = make(map[string]*exec.Cmd)
}
