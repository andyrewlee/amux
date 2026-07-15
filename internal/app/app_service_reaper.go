package app

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/process"
)

// serviceReapResult reports one reaper pass over orphaned service processes,
// plus the resource observations the pass gathers from the same snapshot: the
// per-workspace workload ledger and the system load.
type serviceReapResult struct {
	Killed []process.ProcessInfo
	// Stats maps workspace root -> live workload attributed to it.
	Stats map[string]process.WorkspaceStats
	// LoadPerCore is the 1-minute load average per CPU core (0 when the
	// platform cannot report it). It is sampled even when Err is set — the
	// overload banner must keep working precisely when the heavy ps snapshot
	// starts failing, which is what genuine overload looks like.
	LoadPerCore float64
	Err         error
}

// reapOrphanedServiceProcesses returns a Cmd that kills service process
// groups left behind by workspaces that no longer exist: their root is gone
// from disk or staged as .amux-prune-*. Groups tied to a live workspace are
// never touched (see process.FindWorkspaceOrphans) — reaping must never break
// the detach-and-reattach contract for sessions the user still owns. The
// pass also reconciles the durable service registry against live processes.
func (a *App) reapOrphanedServiceProcesses() tea.Cmd {
	if a.config == nil || a.config.Paths == nil || a.config.Paths.WorkspacesRoot == "" {
		return nil
	}
	workspacesRoot := a.config.Paths.WorkspacesRoot
	registry := a.serviceRegistry
	// Collect the known roots on the Update goroutine; the closure below runs
	// off it and must not touch a.projects.
	var roots []string
	a.eachWorkspace(func(ws *data.Workspace, _ *data.Project) {
		roots = append(roots, ws.Root)
	})
	return func() tea.Msg {
		// Load first: a cheap syscall that must not depend on ps succeeding.
		load, err := process.LoadPerCore()
		if err != nil {
			load = 0
		}
		snap, err := process.Snapshot()
		if err != nil {
			if errors.Is(err, errors.ErrUnsupported) {
				return nil // no process enumeration on this platform
			}
			return serviceReapResult{Err: err, LoadPerCore: load}
		}
		if registry != nil {
			registry.Reconcile(snap)
		}
		orphans := process.FindWorkspaceOrphans(snap, workspacesRoot, process.PathExists)
		killed := process.KillOrphanedGroups(orphans)
		return serviceReapResult{
			Killed:      killed,
			Stats:       process.StatsByWorkspace(snap, roots),
			LoadPerCore: load,
		}
	}
}

// Overload hysteresis thresholds, in 1-minute load average per core. Well
// above the >1 "busy" line so the banner only appears when the machine is
// genuinely drowning (the state where amux's own tmux/git timeouts start
// failing and everything looks broken), and clears with a gap so it does not
// flap.
const (
	overloadOnPerCore  = 4.0
	overloadOffPerCore = 2.5
)

// handleServiceReapResult logs a reaper pass, publishes the workload ledger
// to the dashboard, updates the system-overload banner state, and surfaces
// non-trivial cleanup as a toast so accumulated orphans stop being invisible.
func (a *App) handleServiceReapResult(msg serviceReapResult) tea.Cmd {
	// Overload state updates even on failed passes — see serviceReapResult.
	switch {
	case msg.LoadPerCore >= overloadOnPerCore && !a.systemOverloaded:
		a.systemOverloaded = true
		logging.Warn("system overload detected: load per core %.1f", msg.LoadPerCore)
	case msg.LoadPerCore > 0 && msg.LoadPerCore < overloadOffPerCore && a.systemOverloaded:
		a.systemOverloaded = false
		logging.Info("system overload cleared: load per core %.1f", msg.LoadPerCore)
	}
	a.systemLoadPerCore = msg.LoadPerCore
	if msg.Err != nil {
		logging.Warn("service reaper: %v", msg.Err)
		return nil
	}
	if a.dashboard != nil {
		a.dashboard.SetResourceStats(msg.Stats)
	}
	if len(msg.Killed) == 0 {
		return nil
	}
	logging.Info(
		"service reaper: killed %d orphaned service group(s): %s",
		len(msg.Killed), process.DescribeGroups(msg.Killed),
	)
	if a.toast == nil {
		return nil
	}
	return a.toast.ShowInfo(fmt.Sprintf("Cleaned up %d orphaned service process group(s)", len(msg.Killed)))
}
