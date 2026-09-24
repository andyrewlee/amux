package process

import (
	"log/slog"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// Stop stops the running script for a workspace
func (r *ScriptRunner) Stop(ws *data.Workspace) error {
	if err := validateScriptWorkspace(ws); err != nil {
		return err
	}

	if r.RunHosted() {
		// Kill every run session the workspace owns — a dead (remain-on-exit)
		// session gets cleaned too, so "stop" also clears stale output. The
		// dual-form lookup covers sessions tagged under either workspace-ID
		// form (ws.ID() drifts across worktree create/remove).
		names, err := r.findRunSessions(ws)
		if err != nil {
			return err
		}
		for _, name := range names {
			if err := r.runHost.Kill(name); err != nil {
				return err
			}
		}
		return nil
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
	if r.RunHosted() {
		_, alive, _ := r.runSessionsHosted(ws)
		if !alive {
			// The last session died without a wait-monitor to notice (there is
			// none under the host) — drain a parked port release here, where
			// the app's periodic indicator sync is guaranteed to look.
			r.sweepPendingRelease(scriptWorkspaceKey(ws))
		}
		return alive
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
	if r.RunHosted() {
		if r.IsRunning(ws) {
			// Park the release with a nil running marker; the hosted IsRunning
			// sweep drops it the next time it observes every session dead.
			r.mu.Lock()
			r.pendingRelease[key] = pendingPortRelease{root: ws.Root}
			r.mu.Unlock()
			return
		}
		if r.portAllocator != nil {
			r.portAllocator.ReleasePort(ws.Root)
		}
		return
	}
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

// sweepPendingRelease drains a hosted-mode parked port release: the
// subprocess path gates release on the runningScript pointer finishing; under
// the host there is no wait monitor, so any "nothing alive" observation (the
// indicator sync's poll) is the safe point to let the port go.
func (r *ScriptRunner) sweepPendingRelease(key string) {
	r.mu.Lock()
	pending, ok := r.pendingRelease[key]
	if ok && pending.running == nil {
		delete(r.pendingRelease, key)
	}
	r.mu.Unlock()
	if ok && pending.running == nil && r.portAllocator != nil {
		r.portAllocator.ReleasePort(pending.root)
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
