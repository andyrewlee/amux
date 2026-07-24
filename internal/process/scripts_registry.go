package process

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// This file holds the ScriptRunner's durable-registry integration and the
// stop paths that use it: recording started scripts, clearing records, and
// stopping services recorded by an earlier amux process. Split out of
// scripts.go for the repo's 500-line file cap.

// AttachServiceRegistry enables durable process-group tracking for scripts
// started by this runner.
func (r *ScriptRunner) AttachServiceRegistry(reg *ServiceRegistry) {
	r.mu.Lock()
	r.registry = reg
	r.mu.Unlock()
}

func (r *ScriptRunner) serviceRegistry() *ServiceRegistry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.registry
}

// clearRegistryEntry drops the durable record for an entry this runner just
// stopped tracking. The PID guard keeps a stale clear from erasing a newer
// service's record; an entry with no resolvable PID is left for Reconcile to
// collect rather than cleared blind.
func clearRegistryEntry(reg *ServiceRegistry, key string, entry *runningScript) {
	if reg == nil || entry == nil || entry.cmd == nil || entry.cmd.Process == nil {
		return
	}
	if err := reg.Clear(key, entry.cmd.Process.Pid); err != nil {
		slog.Debug("service registry clear failed", "error", err)
	}
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
		return r.stopFromRegistry(key)
	}

	if running.cmd != nil && running.cmd.Process != nil {
		pid := running.cmd.Process.Pid
		err := r.killProcessGroup(pid, KillOptions{})
		if err != nil {
			if isBenignStopError(err) {
				r.clearRunningEntry(key, running)
				return nil
			}
			return err
		}
		if running.done == nil {
			r.clearRunningEntry(key, running)
			return nil
		}
		// Wait briefly for the background cmd.Wait monitor to observe exit,
		// then escalate to SIGKILL if needed.
		select {
		case <-running.done:
			r.clearRunningEntry(key, running)
		case <-time.After(scriptStopTimeout):
			_ = ForceKillProcess(pid)
			r.clearRunningEntry(key, running)
		}
	}

	return nil
}

// stopFromRegistry stops a service this process never started: a durable
// record left by an earlier amux instance. The record is trusted only when
// the live process still matches its recorded identity (same PID, group, and
// command line), so a recycled PID can never be killed by mistake. A dead or
// mismatched record is dropped instead.
func (r *ScriptRunner) stopFromRegistry(key string) error {
	reg := r.serviceRegistry()
	if reg == nil {
		return nil
	}
	rec, ok := reg.Get(key)
	if !ok {
		return nil
	}
	snap, err := Snapshot()
	if err != nil {
		if errors.Is(err, errors.ErrUnsupported) {
			// No enumeration on this platform: leave the record alone (never
			// kill unverified, never clear a possibly-live service's record).
			return nil
		}
		return fmt.Errorf("stop recorded service: %w", err)
	}
	if !rec.Matches(snap) {
		if err := reg.Clear(key, rec.PID); err != nil {
			slog.Debug("service registry clear failed", "error", err)
		}
		return nil
	}
	if err := r.killProcessGroup(rec.PID, KillOptions{}); err != nil && !isBenignStopError(err) {
		return err
	}
	if err := reg.Clear(key, rec.PID); err != nil {
		slog.Debug("service registry clear failed", "error", err)
	}
	return nil
}
