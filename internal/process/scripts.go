package process

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/andyrewlee/amux/internal/config"
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

// ScriptRunner manages script execution for workspaces
type ScriptRunner struct {
	mu            sync.Mutex
	portAllocator *PortAllocator
	envBuilder    *EnvBuilder
	running       map[string]*runningProcess // workspace root -> running process
}

type runningProcess struct {
	cmd        *exec.Cmd
	scriptType ScriptType
	startedAt  time.Time
}

// NewScriptRunner creates a new script runner
func NewScriptRunner(portStart, portRange int) *ScriptRunner {
	ports := NewPortAllocator(portStart, portRange)
	return &ScriptRunner{
		portAllocator: ports,
		envBuilder:    NewEnvBuilder(ports),
		running:       make(map[string]*runningProcess),
	}
}

// LoadConfig loads the project configuration from the repo.
func (r *ScriptRunner) LoadConfig(repoPath string) (*config.ProjectConfig, error) {
	return config.LoadProjectConfig(repoPath)
}

// RunSetup runs the setup scripts for a workspace
func (r *ScriptRunner) RunSetup(ws *data.Workspace) error {
	config, err := r.LoadConfig(ws.Repo)
	if err != nil {
		return err
	}

	env := r.envBuilder.BuildEnv(ws)

	// Run each setup command sequentially
	for _, cmdStr := range config.SetupScripts {
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
	return r.runScriptInternal(ws, scriptType, nil, nil)
}

// RunScriptWithOutput runs a script and returns stdout/stderr pipes for streaming output.
func (r *ScriptRunner) RunScriptWithOutput(ws *data.Workspace, scriptType ScriptType) (*exec.Cmd, io.ReadCloser, io.ReadCloser, error) {
	var stdout io.ReadCloser
	var stderr io.ReadCloser
	cmd, err := r.runScriptInternal(ws, scriptType, func(c *exec.Cmd) error {
		var err error
		stdout, err = c.StdoutPipe()
		if err != nil {
			return err
		}
		stderr, err = c.StderrPipe()
		if err != nil {
			return err
		}
		return nil
	}, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	return cmd, stdout, stderr, nil
}

// RunScriptWithOutputAndCallback runs a script and invokes onExit when it finishes.
func (r *ScriptRunner) RunScriptWithOutputAndCallback(ws *data.Workspace, scriptType ScriptType, onExit func(error)) (*exec.Cmd, io.ReadCloser, io.ReadCloser, error) {
	var stdout io.ReadCloser
	var stderr io.ReadCloser
	cmd, err := r.runScriptInternal(ws, scriptType, func(c *exec.Cmd) error {
		var err error
		stdout, err = c.StdoutPipe()
		if err != nil {
			return err
		}
		stderr, err = c.StderrPipe()
		if err != nil {
			return err
		}
		return nil
	}, onExit)
	if err != nil {
		return nil, nil, nil, err
	}
	return cmd, stdout, stderr, nil
}

func (r *ScriptRunner) runScriptInternal(ws *data.Workspace, scriptType ScriptType, beforeStart func(*exec.Cmd) error, onExit func(error)) (*exec.Cmd, error) {
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

	if beforeStart != nil {
		if err := beforeStart(cmd); err != nil {
			return nil, err
		}
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.running[ws.Root] = &runningProcess{cmd: cmd, scriptType: scriptType, startedAt: time.Now()}
	r.mu.Unlock()

	// Monitor in background
	safego.Go("process.script_wait", func() {
		err := cmd.Wait()
		r.mu.Lock()
		delete(r.running, ws.Root)
		r.mu.Unlock()
		if onExit != nil {
			onExit(err)
		}
	})

	return cmd, nil
}

// Stop stops the running script for a workspace
func (r *ScriptRunner) Stop(ws *data.Workspace) error {
	r.mu.Lock()
	proc, ok := r.running[ws.Root]
	r.mu.Unlock()

	if !ok {
		return nil
	}

	if proc.cmd != nil && proc.cmd.Process != nil {
		return KillProcessGroup(proc.cmd.Process.Pid, KillOptions{})
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

// RunningProcesses returns a snapshot of running scripts.
func (r *ScriptRunner) RunningProcesses() []RunningProcessInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RunningProcessInfo, 0, len(r.running))
	for root, proc := range r.running {
		info := RunningProcessInfo{
			WorkspaceRoot: root,
			ScriptType:    proc.scriptType,
			StartedAt:     proc.startedAt,
		}
		if proc.cmd != nil && proc.cmd.Process != nil {
			info.PID = proc.cmd.Process.Pid
		}
		out = append(out, info)
	}
	return out
}

// RunningProcessInfo describes a running script process.
type RunningProcessInfo struct {
	WorkspaceRoot string
	ScriptType    ScriptType
	PID           int
	StartedAt     time.Time
}

// StopAll stops all running scripts
func (r *ScriptRunner) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, proc := range r.running {
		if proc.cmd != nil && proc.cmd.Process != nil {
			_ = KillProcessGroup(proc.cmd.Process.Pid, KillOptions{})
		}
	}
	r.running = make(map[string]*runningProcess)
}
