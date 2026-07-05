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

// ErrScriptsNotTrusted is returned (wrapped) when a repo's .amux/workspaces.json
// supplies commands but the user has not approved the current content of that
// file. It is the sentinel callers test with errors.Is to distinguish a trust
// skip from a genuine setup failure.
var ErrScriptsNotTrusted = errors.New("project scripts not trusted")

// ErrScriptsChangedSincePrompt is returned when a user approves script content
// after the repo config changed from the content that originally triggered the prompt.
var ErrScriptsChangedSincePrompt = errors.New("project scripts changed since trust prompt")

// ScriptsNotTrustedError carries the hash of the repo config content that was
// blocked, so the UI can bind a later approval to the exact reviewed content.
type ScriptsNotTrustedError struct {
	Repo       string
	Command    string
	ConfigHash string
}

func (e *ScriptsNotTrustedError) Error() string {
	return fmt.Sprintf("%s (%q): %v", e.Repo, e.Command, ErrScriptsNotTrusted)
}

func (e *ScriptsNotTrustedError) Unwrap() error {
	return ErrScriptsNotTrusted
}

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
	var releaseRoot string
	r.mu.Lock()
	current := r.running[key]
	delete(r.running, key)
	if pending, ok := r.pendingRelease[key]; ok && pending.running == current {
		releaseRoot = pending.root
		delete(r.pendingRelease, key)
	}
	r.mu.Unlock()
	if releaseRoot != "" && r.portAllocator != nil {
		r.portAllocator.ReleasePort(releaseRoot)
	}
}

func (r *ScriptRunner) finishRunningEntry(key string, running *runningScript) {
	var releaseRoot string
	r.mu.Lock()
	if current, ok := r.running[key]; ok && current == running {
		delete(r.running, key)
		if pending, ok := r.pendingRelease[key]; ok && pending.running == running {
			releaseRoot = pending.root
			delete(r.pendingRelease, key)
		}
	}
	r.mu.Unlock()
	if releaseRoot != "" && r.portAllocator != nil {
		r.portAllocator.ReleasePort(releaseRoot)
	}
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
	pendingRelease   map[string]pendingPortRelease
	killProcessGroup func(pid int, opts KillOptions) error
	trust            *ScriptTrust // per-user approval registry for repo-supplied scripts
}

type runningScript struct {
	cmd  *exec.Cmd
	done chan struct{}
}

type pendingPortRelease struct {
	root    string
	running *runningScript
}

func (r *ScriptRunner) setRunningEntry(key string, running *runningScript) {
	r.mu.Lock()
	if _, exists := r.running[key]; exists {
		delete(r.pendingRelease, key)
	}
	r.running[key] = running
	r.mu.Unlock()
}

// NewScriptRunner creates a new script runner
func NewScriptRunner(portStart, portRange int) *ScriptRunner {
	ports := NewPortAllocator(portStart, portRange)
	return &ScriptRunner{
		portAllocator:    ports,
		envBuilder:       NewEnvBuilder(ports),
		running:          make(map[string]*runningScript),
		pendingRelease:   make(map[string]pendingPortRelease),
		killProcessGroup: KillProcessGroup,
		trust:            defaultScriptTrust(),
	}
}

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
func (r *ScriptRunner) RunSetup(ws *data.Workspace) error {
	if err := validateScriptWorkspace(ws); err != nil {
		return err
	}
	config, raw, err := r.loadConfigRaw(ws.Repo)
	if err != nil {
		return err
	}

	// Gate repo-supplied commands behind recorded per-repo consent. Until the
	// user trusts the current content of .amux/workspaces.json, execute nothing
	// and return the sentinel (fail-closed).
	if len(config.SetupWorkspace) > 0 && !r.trust.IsTrusted(ws.Repo, raw) {
		return &ScriptsNotTrustedError{
			Repo:       ws.Repo,
			Command:    config.SetupWorkspace[0],
			ConfigHash: hashConfig(raw),
		}
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

		if err := cmd.Start(); err != nil {
			return err
		}
		running := &runningScript{
			cmd:  cmd,
			done: make(chan struct{}),
		}
		key := scriptWorkspaceKey(ws)
		r.setRunningEntry(key, running)

		err := cmd.Wait()
		close(running.done)
		r.finishRunningEntry(key, running)
		if err != nil {
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

	config, raw, err := r.loadConfigRaw(ws.Repo)
	if err != nil {
		return nil, err
	}

	// fromRepoConfig is true only when the command came from the repo's
	// .amux/workspaces.json (config.RunScript/config.ArchiveScript), false when
	// it fell back to ws.Scripts.* (user-entered in the amux UI). Only the
	// repo-supplied case is gated behind trust.
	var cmdStr string
	var fromRepoConfig bool
	switch scriptType {
	case ScriptRun:
		if config.RunScript != "" {
			cmdStr, fromRepoConfig = config.RunScript, true
		} else {
			cmdStr = ws.Scripts.Run
		}
	case ScriptArchive:
		if config.ArchiveScript != "" {
			cmdStr, fromRepoConfig = config.ArchiveScript, true
		} else {
			cmdStr = ws.Scripts.Archive
		}
	}

	if cmdStr == "" {
		return nil, fmt.Errorf("no %s script configured", scriptType)
	}

	// Gate only repo-supplied commands; user-entered ws.Scripts.* always run.
	if fromRepoConfig && !r.trust.IsTrusted(ws.Repo, raw) {
		return nil, &ScriptsNotTrustedError{
			Repo:       ws.Repo,
			Command:    cmdStr,
			ConfigHash: hashConfig(raw),
		}
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
	r.setRunningEntry(key, running)

	// Monitor in background
	safego.Go("process.script_wait", func() {
		defer close(running.done)
		if err := cmd.Wait(); err != nil {
			slog.Debug("script process exited with error", "error", err)
		}
		r.finishRunningEntry(key, running)
	})

	return cmd, nil
}

// TrustRepoScripts records the current content of repoPath's
// .amux/workspaces.json as approved, so subsequent RunSetup/RunScript calls
// execute its repo-supplied commands. Approval is content-bound: any later edit
// to the file re-gates execution until the user trusts it again. A repo with no
// config file is a no-op (nothing to trust).
func (r *ScriptRunner) TrustRepoScripts(repoPath string) error {
	_, raw, err := r.loadConfigRaw(repoPath)
	if err != nil {
		return err
	}
	if raw == nil {
		return nil
	}
	return r.trust.Trust(repoPath, raw)
}

// TrustRepoScriptsIfHash records trust only if the repo config still matches the
// content hash that originally triggered the user approval prompt.
func (r *ScriptRunner) TrustRepoScriptsIfHash(repoPath, expectedHash string) error {
	_, raw, err := r.loadConfigRaw(repoPath)
	if err != nil {
		return err
	}
	if raw == nil {
		return nil
	}
	if expectedHash != "" && hashConfig(raw) != expectedHash {
		return ErrScriptsChangedSincePrompt
	}
	return r.trust.Trust(repoPath, raw)
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
		if err != nil {
			if isBenignStopError(err) {
				r.clearRunningEntry(key)
				return nil
			}
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

// PortAllocated reports the port base allocated for the workspace, and whether
// one is currently held. It mirrors PortAllocator.GetPort so callers (and the
// delete path's tests) can observe release without reaching into the allocator.
func (r *ScriptRunner) PortAllocated(ws *data.Workspace) (int, bool) {
	if validateScriptWorkspace(ws) != nil || r.portAllocator == nil {
		return 0, false
	}
	return r.portAllocator.GetPort(ws.Root)
}

// ReleaseWorkspace releases the workspace's port allocation once no script is
// running for it, so a deleted workspace's port-range entry does not leak in the
// allocator's map for the lifetime of the process. It is a no-op while a script
// is still running so a release can never strand a live script's port; the
// caller (workspace delete) tears scripts down first. The allocator is keyed by
// the raw ws.Root (see EnvBuilder.PortRange), so release uses ws.Root directly.
func (r *ScriptRunner) ReleaseWorkspace(ws *data.Workspace) {
	if validateScriptWorkspace(ws) != nil {
		return
	}
	key := scriptWorkspaceKey(ws)
	r.mu.Lock()
	running, isRunning := r.running[key]
	if isRunning {
		r.pendingRelease[key] = pendingPortRelease{root: ws.Root, running: running}
	}
	r.mu.Unlock()
	if isRunning {
		return
	}
	if r.portAllocator != nil {
		r.portAllocator.ReleasePort(ws.Root)
	}
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
