package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
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
	// EPERM on kill(-pgid) means the group exists but is not ours to signal —
	// the tracked PID was recycled into a foreign group between the running
	// entry's snapshot and the kill. The entry is stale either way; mirror
	// the EPERM tolerance KillProcessGroup already applies to its own
	// SIGKILL leg.
	if errors.Is(err, syscall.EPERM) {
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
	mu             sync.Mutex
	portAllocator  *PortAllocator
	envBuilder     *EnvBuilder
	running        map[string]*runningScript // workspace root -> running process
	pendingRelease map[string]pendingPortRelease
	// stopped latches once StopAll swaps the running map — setRunningEntry
	// observes it under the same r.mu that inserts, so a concurrently
	// admitted run cannot escape the shutdown sweep.
	stopped bool
	// runAdmissionMu stripes the nonconcurrent Stop+spawn pair across a
	// fixed slot set — per-key mutual exclusion without a map that would
	// grow for the process lifetime. Two racing RunScript calls for one
	// nonconcurrent workspace must serialize, else both survive Stop and
	// mint two sessions/processes (L1).
	runAdmissionMu   [64]sync.Mutex
	killProcessGroup func(pid int, opts KillOptions) error
	trust            *ScriptTrust // per-user approval registry for repo-supplied scripts
	// runHost hosts `run` scripts in persistent sessions (tmux at the app
	// layer) when set; nil keeps the subprocess path. See run_session.go.
	runHost RunSessionHost
	// projectEnv resolves the user-level per-project env map for a repo path
	// (secrets/overrides that sit above repo `env` and beneath ws.Env). Nil
	// means no project layer. See script_env.go.
	projectEnv func(repoPath string) map[string]string
	// projectScripts resolves the user-level per-project script defaults for
	// a repo path — the layer beneath ws.Scripts (precedence: repo →
	// workspace → project). Nil means no project layer. User-authored like
	// ws.Scripts, so never trust-gated. See project_scripts.go /
	// script_lifecycle.go.
	projectScripts func(repoPath string) data.ScriptsConfig
	// lastOutput records the bounded transcript of the most recent run of
	// each lifecycle script (setup/archive/on-done) per workspace — what the
	// "script output" viewer shows. See script_output.go.
	lastOutput map[string]ScriptOutput
	// liveSetup holds the in-flight setup tail per workspace key — the
	// same tailWriter the process's copier feeds, exposed to the output
	// viewer so a running setup streams instead of rendering like a hang.
	// Entries exist only between admission and finish, and the setup slot
	// guarantees at most one per key. See script_output.go.
	liveSetup map[string]*tailWriter
	// exitListener notifies the app when a detached lifecycle script exits
	// non-zero (on-done — the only lifecycle hook without a synchronous
	// caller to report to). See script_output.go.
	exitListener func(ws *data.Workspace, scriptType ScriptType, runErr error)
	// runSessionsSeen records workspace ID forms under which run sessions
	// were ever found, so the hosted-status sweep can be skipped for
	// workspaces that have never had one. See run_session.go.
	runSessionsSeen map[string]struct{}
	// runSessionsSwept records ID forms whose one-shot post-restart probe
	// already ran — the seen-set is process-local, so a restarted amux gets
	// exactly one discovery sweep per workspace, not zero and not every
	// poll. See run_session.go.
	runSessionsSwept map[string]struct{}
	// lifecycle tracks local lifecycle work — the workspace's single
	// admitted setup sequence and its detached on-done hooks — plus the
	// teardown gate service removal holds across stop→archive→remove.
	// Setup does not occupy the `running` map: that map is run-scripts only,
	// so a re-run request can no longer overwrite the tracked slot, and a
	// stale run-script monitor can never clear a live setup entry.
	// See lifecycle_coordinator.go.
	lifecycle *lifecycleCoordinator
	// transcriptRoot is the workspaces-metadata dir under which lifecycle
	// transcripts persist (write-through on record, fallback on read).
	// Empty disables persistence. transcriptMu serializes the file writes —
	// see persistScriptOutputs for why the map is re-gathered under it.
	// See script_output_store.go.
	transcriptRoot string
	transcriptMu   sync.Mutex
	// transcriptLoadHook is a test seam that runs between the disk read and
	// the memory fold in LastScriptOutputs — the window where a concurrent
	// record used to be clobbered by stale hydration.
	transcriptLoadHook func()
	// postStartHook is a test seam that runs in RunScript/runScriptHosted
	// between the spawn (cmd.Start / Ensure) and the post-start admission
	// re-check — the window where a teardown can slip past the early gate
	// check. Tests flip the gate inside it to prove the loser kills+reaps.
	postStartHook func(key string)
}

type runningScript struct {
	cmd  *exec.Cmd
	done chan struct{}
}

type pendingPortRelease struct {
	root    string
	running *runningScript
}

// setRunningEntry installs the run slot under the same lock that observes
// the stopped flag: a StopAll swap that already ran leaves stopped set, and
// an insert racing it must not land in the post-shutdown map — the escaping
// process would survive shutdown. Returns false on refusal; the caller
// kills and reaps the just-started child.
//
// On slot overwrite a parked release pinned to the displaced entry is
// re-armed as sweepable (running=nil) instead of dropped: the release still
// fires at the next "nothing alive" observation, and the displaced
// process's own finish path would never find it once the slot belongs to a
// newer entry (finishRunningEntry requires the parked-for process to be the
// current slot owner).
func (r *ScriptRunner) setRunningEntry(key string, running *runningScript) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return false
	}
	if displaced, exists := r.running[key]; exists && displaced != running {
		if pending, ok := r.pendingRelease[key]; ok && pending.running == displaced {
			pending.running = nil
			r.pendingRelease[key] = pending
		}
	}
	r.running[key] = running
	return true
}

// lockRunAdmission holds the workspace's striped admission mutex across a
// nonconcurrent Stop+spawn — see runAdmissionMu.
func (r *ScriptRunner) lockRunAdmission(key string) func() {
	var h uint32
	for i := 0; i < len(key); i++ {
		h = h*31 + uint32(key[i])
	}
	mu := &r.runAdmissionMu[h%uint32(len(r.runAdmissionMu))]
	mu.Lock()
	return mu.Unlock
}

// NewScriptRunner creates a new script runner
func NewScriptRunner(portStart, portRange int) *ScriptRunner {
	ports := NewPortAllocator(portStart, portRange)
	r := &ScriptRunner{
		portAllocator:    ports,
		envBuilder:       NewEnvBuilder(ports),
		running:          make(map[string]*runningScript),
		pendingRelease:   make(map[string]pendingPortRelease),
		lastOutput:       make(map[string]ScriptOutput),
		runSessionsSeen:  make(map[string]struct{}),
		runSessionsSwept: make(map[string]struct{}),
		killProcessGroup: KillProcessGroup,
		trust:            defaultScriptTrust(),
		lifecycle:        newLifecycleCoordinator(),
	}
	// Route coordinator kills through the runner's injectable seam so a test
	// that swaps r.killProcessGroup also observes teardown drains.
	r.lifecycle.killGroup = func(pid int, opts KillOptions) error {
		return r.killProcessGroup(pid, opts)
	}
	return r
}

// SetPortReservationStore installs the shared durable port-reservation
// registry on the runner's allocator. The app wires it once at startup,
// before any environment provider or spawn is exposed — after it, every
// setup/run/archive/on-done spawn, every agent/viewer, and every sidebar
// session draws its AMUX_PORT range from the same cross-instance registry,
// keyed by the workspace's persisted metadata ID.
func (r *ScriptRunner) SetPortReservationStore(store *data.PortReservationStore) {
	r.portAllocator.SetDurableStore(store)
}

// RunSetup runs the workspace's setup commands to completion: the repo's
// setup-workspace list when present (trust-gated like every repo script), else
// the workspace's own Scripts.Setup field — the same repo-first resolution the
// other lifecycle scripts use. It is called on workspace create/restore and by
// the user-triggered re-run key, so it must be safe to run more than once —
// which is on the script's own idempotency, as with any re-run provisioning.
func (r *ScriptRunner) RunSetup(ws *data.Workspace) error {
	if err := validateScriptWorkspace(ws); err != nil {
		return err
	}
	config, raw, err := r.loadConfigRaw(ws.Repo)
	if err != nil {
		return err
	}

	// Resolution order matches resolveScriptCommand: the repo's
	// setup-workspace list wins; the workspace's own Scripts.Setup (typed
	// into the scripts editor) fills in, then the user-level project default
	// (project-scripts.json — same precedence tail). Both non-repo layers
	// are user-authored and run without the trust gate.
	commands := config.SetupWorkspace
	fromRepo := len(commands) > 0
	if !fromRepo && ws.Scripts.Setup != "" {
		commands = []string{ws.Scripts.Setup}
	}
	if len(commands) == 0 {
		if setup := r.projectScriptFor(ws.Repo, ScriptSetup); setup != "" {
			commands = []string{setup}
		}
	}

	// Gate repo-supplied commands behind recorded per-repo consent. Until the
	// user trusts the current content of .amux/workspaces.json, execute nothing
	// and return the sentinel (fail-closed).
	if fromRepo && !r.trust.IsTrusted(ws.Repo, raw) {
		return &ScriptsNotTrustedError{
			Repo:       ws.Repo,
			Command:    commands[0],
			ConfigHash: hashConfig(raw),
		}
	}

	env, err := r.buildScriptEnv(ws)
	if err != nil {
		return err
	}

	// Neither source defines a setup — nothing to run. The sentinel lets
	// callers distinguish that from a failure; env was still built above so
	// the workspace's port allocation behaves exactly as before.
	if len(commands) == 0 {
		return fmt.Errorf("%s: %w", ScriptSetup, ErrNoScriptConfigured)
	}

	// Admission: one setup sequence per workspace. A second request reports
	// ErrSetupBusy (informational — the first run is authoritative) instead
	// of overwriting the tracked slot; a held teardown gate rejects with
	// ErrWorkspaceTeardown so nothing new starts while removal is pending.
	key := scriptWorkspaceKey(ws)
	ticket, err := r.lifecycle.admitSetup(key)
	if err != nil {
		return err
	}
	defer r.lifecycle.finishSetup(key, ticket)

	// Run each setup command sequentially. Output across all commands lands
	// in one bounded tail so the recorded transcript (and a failure's error)
	// covers the whole setup, not just the command that died — stdout was
	// previously dropped entirely, which hid the failure's own diagnostics.
	//
	// The same tail is published for the live-output viewer: the copier
	// goroutine's Writes land under the tail's own mutex, so readers see a
	// consistent snapshot at any moment. Only the admitted sequence
	// registers — an ErrSetupBusy loser never reaches here, so the slot
	// cannot be overwritten by a second request.
	tail := &tailWriter{max: scriptOutputTailBytes}
	r.mu.Lock()
	if r.liveSetup == nil {
		r.liveSetup = map[string]*tailWriter{}
	}
	r.liveSetup[key] = tail
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.liveSetup, key)
		r.mu.Unlock()
	}()
	for _, cmdStr := range commands {
		// The ticket's context spans the whole sequence: teardown cancels it
		// once, which aborts the in-flight command AND every queued command —
		// a stale sequence must never start its next step after teardown.
		if err := ticket.ctx.Err(); err != nil {
			r.recordScriptOutput(ws, ScriptSetup, tail.String(), err)
			return fmt.Errorf("setup canceled: %w", err)
		}
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
		// Register the started process atomically with a generation check:
		// a teardown landing between admission and Start invalidates the
		// ticket, and the just-spawned child is killed and reaped here so it
		// never outlives the teardown gate.
		if !r.lifecycle.registerCmd(key, ticket, cmd) {
			r.reapAfterKill(cmd)
			err := ticket.ctx.Err()
			if err == nil {
				err = ErrWorkspaceTeardown
			}
			r.recordScriptOutput(ws, ScriptSetup, tail.String(), err)
			return fmt.Errorf("setup canceled: %w", err)
		}

		err := cmd.Wait()
		r.lifecycle.clearCmd(key, ticket, cmd)
		if err != nil {
			r.recordScriptOutput(ws, ScriptSetup, tail.String(), err)
			if ticket.ctx.Err() != nil {
				return fmt.Errorf("setup canceled: %w", ticket.ctx.Err())
			}
			return fmt.Errorf("setup command failed: %s: %s: %w", cmdStr, tail.String(), err)
		}
	}
	r.recordScriptOutput(ws, ScriptSetup, tail.String(), nil)

	return nil
}
