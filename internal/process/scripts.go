package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// ScriptType identifies the type of script
type ScriptType string

const (
	ScriptSetup   ScriptType = "setup"
	ScriptRun     ScriptType = "run"
	ScriptArchive ScriptType = "archive"
	ScriptOnDone  ScriptType = "on-done"
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
	// OnDoneScript fires when an agent session in the workspace crosses the
	// working→done edge. Trust-gated like the others — it executes on a
	// lifecycle edge without any user keystroke.
	OnDoneScript string `json:"on-done"`
	// Env layers repo-supplied defaults beneath the workspace's own env map.
	// Trust-gated: env reaches every spawned process, so an unapproved repo
	// map must never leak in — it is shareable non-secret config only
	// (committed to the repo; secrets belong in the user-level project map).
	Env map[string]string `json:"env"`
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
	// runHost hosts `run` scripts in persistent sessions (tmux at the app
	// layer) when set; nil keeps the subprocess path. See run_session.go.
	runHost RunSessionHost
	// projectEnv resolves the user-level per-project env map for a repo path
	// (secrets/overrides that sit above repo `env` and beneath ws.Env). Nil
	// means no project layer. See script_env.go.
	projectEnv func(repoPath string) map[string]string
	// lastOutput records the bounded transcript of the most recent run of
	// each lifecycle script (setup/archive/on-done) per workspace — what the
	// "script output" viewer shows. See script_output.go.
	lastOutput map[string]ScriptOutput
	// exitListener notifies the app when a detached lifecycle script exits
	// non-zero (on-done — the only lifecycle hook without a synchronous
	// caller to report to). See script_output.go.
	exitListener func(ws *data.Workspace, scriptType ScriptType, runErr error)
	// runSessionsSeen records workspace ID forms under which run sessions
	// were ever found, so the hosted-status sweep can be skipped for
	// workspaces that have never had one. See run_session.go.
	runSessionsSeen map[string]struct{}
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
		lastOutput:       make(map[string]ScriptOutput),
		runSessionsSeen:  make(map[string]struct{}),
		killProcessGroup: KillProcessGroup,
		trust:            defaultScriptTrust(),
	}
}

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

	env, err := r.buildScriptEnv(ws)
	if err != nil {
		return err
	}

	// Run each setup command sequentially. Output across all commands lands
	// in one bounded tail so the recorded transcript (and a failure's error)
	// covers the whole setup, not just the command that died — stdout was
	// previously dropped entirely, which hid the failure's own diagnostics.
	tail := &tailWriter{max: scriptOutputTailBytes}
	for _, cmdStr := range config.SetupWorkspace {
		cmd := exec.Command("sh", "-c", cmdStr)
		cmd.Dir = ws.Root
		cmd.Env = env
		SetProcessGroup(cmd)

		// One writer for both streams: exec dedupes it into a single copy
		// goroutine, preserving real output order in the transcript.
		cmd.Stdout = tail
		cmd.Stderr = tail

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
			r.recordScriptOutput(ws, ScriptSetup, tail.String(), err)
			return fmt.Errorf("setup command failed: %s: %s: %w", cmdStr, tail.String(), err)
		}
	}
	r.recordScriptOutput(ws, ScriptSetup, tail.String(), nil)

	return nil
}
