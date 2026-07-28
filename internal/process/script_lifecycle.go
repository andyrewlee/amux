package process

import (
	"bytes"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/safego"
)

// This file holds the two ways amux runs a workspace's own scripts, and the
// command resolution they share. RunSetup lives in scripts.go with the runner
// itself because it is part of workspace creation; these are the post-creation
// lifecycle hooks: `run` (started on demand, long-lived) and `archive` (run to
// completion at teardown).

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
// It takes over the workspace's single tracking slot for its duration, so the
// caller must have stopped any run script first (the delete path does).
func (r *ScriptRunner) RunArchive(ws *data.Workspace) error {
	cmdStr, err := r.resolveScriptCommand(ws, ScriptArchive)
	if err != nil {
		return err
	}

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = ws.Root
	cmd.Env = r.envBuilder.BuildEnv(ws)
	SetProcessGroup(cmd)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting archive script: %s: %w", cmdStr, err)
	}

	running := &runningScript{
		cmd:  cmd,
		done: make(chan struct{}),
	}
	key := scriptWorkspaceKey(ws)
	r.setRunningEntry(key, running)

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

	close(running.done)
	r.finishRunningEntry(key, running)

	if runErr != nil {
		if msg := stderrForError(&stderr, reaped); msg != "" {
			return fmt.Errorf("archive script failed: %s: %s: %w", cmdStr, msg, runErr)
		}
		return fmt.Errorf("archive script failed: %s: %w", cmdStr, runErr)
	}
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

// stderrForError renders the captured stderr for an error message, but only
// when the process was reaped — see the race note on RunArchive's reaped flag.
func stderrForError(buf *bytes.Buffer, reaped bool) string {
	if !reaped {
		return ""
	}
	return strings.TrimSpace(buf.String())
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
