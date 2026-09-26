package process

import (
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/safego"
)

// This file holds the two ways amux runs a workspace's own scripts, and the
// command resolution they share. RunSetup lives in scripts.go with the runner
// itself because it is part of workspace creation; these are the post-creation
// lifecycle hooks: `run` (started on demand, long-lived) and `archive` (run to
// completion at teardown).

// ErrNoScriptConfigured is returned (wrapped) when neither the repo's
// .amux/workspaces.json nor the workspace's own Scripts field defines a command
// for the requested script type. It is a sentinel rather than a bare error so
// callers can treat "nothing to run" as benign — the archive-on-delete path
// skips silently, while a user-triggered run reports it — instead of surfacing
// it as a failure.
var ErrNoScriptConfigured = errors.New("no script configured")

// resolveScriptCommand picks the command to run for scriptType and applies the
// trust gate, returning the resolved shell command string. It is the shared
// front half of RunScript (async, long-lived `run`) and RunArchive (synchronous,
// bounded `archive`) so both resolve the command and enforce trust identically.
//
// Resolution order is repo config first, then the workspace's own Scripts field.
// Only the repo-supplied command is gated behind trust: ws.Scripts.* is the
// user's own input, typed into the amux UI, and always runs.
//
// It returns ErrNoScriptConfigured when neither source defines a command, and a
// *ScriptsNotTrustedError when a repo-supplied command is not yet approved.
func (r *ScriptRunner) resolveScriptCommand(ws *data.Workspace, scriptType ScriptType) (string, error) {
	if err := validateScriptWorkspace(ws); err != nil {
		return "", err
	}

	config, raw, err := r.loadConfigRaw(ws.Repo)
	if err != nil {
		return "", err
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
	case ScriptOnDone:
		if config.OnDoneScript != "" {
			cmdStr, fromRepoConfig = config.OnDoneScript, true
		} else {
			cmdStr = ws.Scripts.OnDone
		}
	}

	if cmdStr == "" {
		return "", fmt.Errorf("%s: %w", scriptType, ErrNoScriptConfigured)
	}

	// Gate only repo-supplied commands; user-entered ws.Scripts.* always run.
	if fromRepoConfig && !r.trust.IsTrusted(ws.Repo, raw) {
		return "", &ScriptsNotTrustedError{
			Repo:       ws.Repo,
			Command:    cmdStr,
			ConfigHash: hashConfig(raw),
		}
	}

	return cmdStr, nil
}

// RunScript starts a script for a workspace and returns as soon as it is
// spawned; the process is tracked so Stop/IsRunning can manage it. Use it for
// the long-lived `run` script (a dev server), not for commands the caller needs
// to see finish — RunArchive covers that case.
func (r *ScriptRunner) RunScript(ws *data.Workspace, scriptType ScriptType) (*exec.Cmd, error) {
	cmdStr, err := r.resolveScriptCommand(ws, scriptType)
	if err != nil {
		return nil, err
	}

	// A held teardown gate rejects new starts: a run launched while removal is
	// pending would survive the teardown's hosted-session kill entirely.
	if err := r.lifecycle.checkAdmission(scriptWorkspaceKey(ws)); err != nil {
		return nil, err
	}

	// Check for existing process in non-concurrent mode
	if ws.ScriptMode == "nonconcurrent" {
		if err := r.Stop(ws); !isBenignStopError(err) {
			return nil, err
		}
	}

	// Hosted path: the run rides a detached tmux session (scrollback,
	// remain-on-exit forensics) instead of a naked subprocess. Returns a nil
	// *exec.Cmd — callers must not touch it when RunHosted() is true.
	if r.runHost != nil {
		return nil, r.runScriptHosted(ws, cmdStr)
	}

	env, err := r.buildScriptEnv(ws)
	if err != nil {
		return nil, err
	}

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

// archiveTimeout bounds the archive script. Archive commands are documented as
// wrap-up work (the README's example tars the workspace up), so they are
// expected to finish rather than run forever — but they can touch the whole
// tree, so the budget is generous. It is a var so tests can shorten it.
var archiveTimeout = 2 * time.Minute

// RunArchive runs a workspace's `archive` script to completion and returns its
// outcome. Unlike RunScript it is synchronous: the archive script is a workspace
// teardown hook that runs while the worktree still exists, so the caller must
// know it finished before removing that directory.
//
// The command is killed if it exceeds archiveTimeout, and its process group is
// torn down with it so a shell that forked children does not outlive the call.
// Stderr is captured and folded into the returned error, which is what the UI
// surfaces. A workspace with no archive script returns ErrNoScriptConfigured,
// which callers treat as "nothing to do".
//
// RunArchive always returns: a process that survives both the kill and the
// escalation is abandoned rather than waited on, because the caller is a
// workspace delete and a delete must never be able to hang forever.
//
// While a workspace's teardown gate is held, only the owning TeardownGuard may
// invoke archive — TeardownGuard.RunArchive calls runArchive with that guard.
// Outside teardown, RunArchive admits freely.
func (r *ScriptRunner) RunArchive(ws *data.Workspace) error {
	return r.runArchive(ws, nil)
}

func (r *ScriptRunner) runArchive(ws *data.Workspace, guard *TeardownGuard) error {
	if err := r.lifecycle.checkArchiveAdmission(scriptWorkspaceKey(ws), guard); err != nil {
		return err
	}
	cmdStr, err := r.resolveScriptCommand(ws, ScriptArchive)
	if err != nil {
		return err
	}

	env, err := r.buildScriptEnv(ws)
	if err != nil {
		return err
	}

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = ws.Root
	cmd.Env = env
	SetProcessGroup(cmd)

	// Bounded combined tail: stdout was previously dropped entirely and
	// stderr only reached the error string. The recorded transcript is what
	// the "script output" viewer shows; the error folds in the same tail.
	tail := &tailWriter{max: scriptOutputTailBytes}
	cmd.Stdout = tail
	cmd.Stderr = tail

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting archive script: %s: %w", cmdStr, err)
	}

	// Wait in the background so the timeout can win the race: cmd.Wait cannot be
	// interrupted, so the timer kills the process group and Wait then returns.
	waitErr := make(chan error, 1)
	safego.Go("process.archive_wait", func() {
		waitErr <- cmd.Wait()
	})

	timer := time.NewTimer(archiveTimeout)
	defer timer.Stop()

	// reaped records whether cmd.Wait actually returned. It gates reading
	// stderr: exec fills that buffer from a copier goroutine that only Wait
	// joins, so reading it after abandoning the wait would be a data race.
	var runErr error
	reaped := true

	select {
	case runErr = <-waitErr:
	case <-timer.C:
		runErr = fmt.Errorf("archive script timed out after %s", archiveTimeout)
		reaped = r.reapAfterTimeout(cmd, waitErr)
	}

	// The tail is only safe to read once cmd.Wait joined its copier — see
	// the reaped flag's race note above.
	tailText := ""
	if reaped {
		tailText = tail.String()
	}
	r.recordScriptOutput(ws, ScriptArchive, tailText, runErr)

	if runErr != nil {
		if tailText != "" {
			return fmt.Errorf("archive script failed: %s: %s: %w", cmdStr, tailText, runErr)
		}
		return fmt.Errorf("archive script failed: %s: %w", cmdStr, runErr)
	}
	return nil
}

// RunOnDone spawns the workspace's `on-done` hook — a fire-and-forget command
// invoked by the activity loop when one of the workspace's agent sessions
// crosses the working→done edge.
//
// Unlike RunArchive it never blocks the caller (the edge fires inside the
// scan pipeline). It is tracked on the lifecycle coordinator's own on-done set
// — deliberately out of the run-script slot that Stop/RunActive/concurrent-
// mode manage — so workspace teardown can drain it before the directory goes
// away. It shares the resolve+trust front half with every other script type:
// a repo-supplied on-done is gated identically to run/archive (it executes on
// a lifecycle edge with no user keystroke), while ws.Scripts.OnDone is user
// input and always runs.
//
// env is BuildEnv plus AMUX_SESSION naming the session that finished. Errors
// resolve as: ErrNoScriptConfigured → nil (most workspaces have no hook);
// untrusted or spawn failures → returned for the caller to surface. The
// spawned process's own exit is logged, never propagated — a hook crash must
// not perturb the scan loop.
func (r *ScriptRunner) RunOnDone(ws *data.Workspace, sessionName string) error {
	cmdStr, err := r.resolveScriptCommand(ws, ScriptOnDone)
	if err != nil {
		if errors.Is(err, ErrNoScriptConfigured) {
			return nil
		}
		return err
	}

	env, err := r.buildScriptEnv(ws)
	if err != nil {
		return err
	}

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = ws.Root
	cmd.Env = append(env, "AMUX_SESSION="+sessionName)
	SetProcessGroup(cmd)

	// Bounded combined tail: the hook is fire-and-forget, so the transcript
	// and the exit listener are the only ways a failing hook is visible —
	// a non-zero exit must reach the UI, not just a Debug log.
	tail := &tailWriter{max: scriptOutputTailBytes}
	cmd.Stdout = tail
	cmd.Stderr = tail

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting on-done hook: %s: %w", cmdStr, err)
	}
	// Admission after Start, atomically: a teardown landing between resolve
	// and spawn rejects the registration, and the just-started child is
	// killed and reaped inline so it cannot outlive the removal.
	key := scriptWorkspaceKey(ws)
	proc, ok := r.lifecycle.admitOnDone(key, cmd)
	if !ok {
		_ = KillProcessGroup(cmd.Process.Pid, KillOptions{})
		_ = cmd.Wait()
		return ErrWorkspaceTeardown
	}
	safego.Go("process.on_done_wait", func() {
		defer close(proc.done)
		err := cmd.Wait()
		r.recordScriptOutput(ws, ScriptOnDone, tail.String(), err)
		if err != nil {
			slog.Debug("on-done hook exited non-zero", "command", cmdStr, "error", err)
			r.notifyScriptExit(ws, ScriptOnDone, err)
		}
		r.lifecycle.finishOnDone(key, proc)
	})
	return nil
}

// reapAfterTimeout kills the timed-out script's process group and waits a
// bounded while for it to be reaped, escalating to a direct SIGKILL the way
// Stop does. It reports whether cmd.Wait returned.
//
// Giving up is deliberate. A process wedged in uninterruptible sleep cannot be
// killed at all, and waiting on it would hang the workspace delete that called
// us. The wait goroutine's channel is buffered, so abandoning it leaks nothing
// permanently — it completes on its own whenever the process finally exits.
func (r *ScriptRunner) reapAfterTimeout(cmd *exec.Cmd, waitErr <-chan error) bool {
	r.killScriptProcessGroup(cmd)

	select {
	case <-waitErr:
		return true
	case <-time.After(scriptStopTimeout):
	}

	if cmd.Process != nil {
		if err := ForceKillProcess(cmd.Process.Pid); err != nil && !isBenignStopError(err) {
			slog.Debug("force-killing unreaped archive script", "error", err)
		}
	}
	select {
	case <-waitErr:
		return true
	case <-time.After(scriptStopTimeout):
		slog.Warn("archive script could not be reaped; abandoning it so the delete can proceed")
		return false
	}
}

// killScriptProcessGroup tears down cmd's whole process group via the injected
// killer, so tests can observe the kill without signaling real processes.
func (r *ScriptRunner) killScriptProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	kill := r.killProcessGroup
	if kill == nil {
		kill = KillProcessGroup
	}
	if err := kill(cmd.Process.Pid, KillOptions{}); err != nil && !isBenignStopError(err) {
		slog.Debug("killing timed-out archive script", "error", err)
	}
}
