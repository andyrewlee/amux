package app

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// handleDeleteWorkspace handles the DeleteWorkspace message.
func (a *App) handleDeleteWorkspace(msg messages.DeleteWorkspace) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Project == nil || msg.Workspace == nil {
		logging.Warn("DeleteWorkspace received with nil project or workspace")
		return nil
	}
	msg.Workspace = snapshotWorkspaceForSave(msg.Workspace)
	if !a.markWorkspaceDeleteInFlight(msg.Workspace, true) {
		logging.Warn("DeleteWorkspace rejected while workspace %s is in another lifecycle phase", msg.Workspace.ID())
		return nil
	}
	a.lifecycle.clearCreatedProjectLoadBarrier(string(msg.Workspace.ID()), msg.Workspace.Root)
	// Disarm any auto-launch still pending for this workspace. Workspace IDs are
	// derived from project+name, so a delete-then-recreate at the same name
	// reuses the ID (see the recreate note in handleWorkspaceDeleted below) and a
	// stale arming would otherwise fire against the new workspace.
	a.clearPendingAgentLaunchFor(msg.Workspace)
	// Do NOT kill the workspace's tmux sessions here. All real delete validation
	// (primary-checkout guard, repo/path checks, worktree removal) runs later in
	// the async DeleteWorkspace cmd; killing up-front means a rejected or failed
	// delete still destroys live agent sessions and scrollback. The kill now runs
	// only on the confirmed-success path in handleWorkspaceDeleted.
	if cmd := a.dashboard.SetWorkspaceDeleting(msg.Workspace.Root, true); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, a.deleteWorkspace(msg.Project, msg.Workspace))
	return cmds
}

// handleRenameWorkspace handles the RenameWorkspace message: a Tier-1 label
// rename. It updates only the workspace's display Name via the store — the git
// branch, worktree, and workspace ID() are untouched — then reloads projects so
// the dashboard and sidebar labels refresh.
func (a *App) handleRenameWorkspace(msg messages.RenameWorkspace) []tea.Cmd {
	if msg.Workspace == nil {
		logging.Warn("RenameWorkspace received with nil workspace")
		return nil
	}
	if a.workspaceService == nil || a.workspaceService.store == nil {
		return nil
	}
	if err := a.workspaceService.store.Rename(msg.Workspace.ID(), msg.NewName); err != nil {
		if cmd := common.ReportError(errorContext(errorServiceWorkspace, "renaming workspace"), err, ""); cmd != nil {
			return []tea.Cmd{cmd}
		}
		return nil
	}
	// Reflect the new label immediately on the in-memory active workspace so the
	// header updates without waiting for the async reload.
	if a.activeWorkspace != nil && a.activeWorkspace.Root == msg.Workspace.Root {
		a.activeWorkspace.Name = msg.NewName
	}
	var cmds []tea.Cmd
	if cmd := a.toast.ShowSuccess("Renamed workspace to " + msg.NewName); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, a.loadProjects())
	return cmds
}

// handleWorkspaceCreatedWithWarning handles the WorkspaceCreatedWithWarning message.
func (a *App) handleWorkspaceCreatedWithWarning(msg messages.WorkspaceCreatedWithWarning) []tea.Cmd {
	var cmds []tea.Cmd
	a.err = fmt.Errorf("workspace created with warning: %s", msg.Warning)
	if msg.Workspace != nil {
		a.lifecycle.clearCreating(string(msg.Workspace.ID()))
		if cmd := a.dashboard.SetWorkspaceCreating(msg.Workspace, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	cmds = append(cmds, a.loadProjectsAfterCreate(msg.Workspace))
	return cmds
}

func (a *App) loadProjectsAfterCreate(ws *data.Workspace) tea.Cmd {
	cmd := a.loadProjects()
	if cmd != nil && ws != nil {
		a.lifecycle.markCreatedUntilProjectsLoad(
			string(ws.ID()),
			ws.Root,
			a.lifecycle.projectsLoadToken,
		)
	}
	return cmd
}

// handleWorkspaceCreated handles the WorkspaceCreated message.
func (a *App) handleWorkspaceCreated(msg messages.WorkspaceCreated) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Workspace != nil {
		a.lifecycle.clearCreating(string(msg.Workspace.ID()))
		if cmd := a.dashboard.SetWorkspaceCreating(msg.Workspace, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, a.runSetupAsync(msg.Workspace))
		// Open the new workspace immediately and arm the assistant the user
		// already picked in the create flow, so creation ends in a live agent
		// tab instead of a dashboard row they must click into and re-pick.
		if cmd := a.activateCreatedWorkspace(msg.Workspace); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	cmds = append(cmds, a.loadProjectsAfterCreate(msg.Workspace))
	return cmds
}

// activateCreatedWorkspace returns a command activating a freshly created
// workspace. The assistant recorded on the workspace is armed for launch and
// fires from handleWorkspaceActivated, after the activation has switched the
// center pane over, so the agent tab is created against the right workspace.
func (a *App) activateCreatedWorkspace(ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return nil
	}
	// Activating with an unresolvable project would leave activeWorkspace set
	// and activeProject nil, which disables the workspace-scoped commands. Stay
	// put instead and let the projects reload settle selection.
	project := a.findProjectByPath(ws.Repo)
	if project == nil {
		logging.Warn("created workspace %s has no loaded project for %s; skipping activation", ws.ID(), ws.Repo)
		return nil
	}
	if assistant := strings.TrimSpace(ws.Assistant); a.canAutoLaunchAssistant(assistant) {
		if a.pendingLaunchAssistants == nil {
			a.pendingLaunchAssistants = make(map[string]string)
		}
		a.pendingLaunchAssistants[string(ws.ID())] = assistant
	}
	// The projects reload carrying this workspace is still in flight, so the
	// dashboard holds the selection until its row appears.
	if a.dashboard != nil {
		a.dashboard.SelectWorkspace(string(ws.ID()))
	}
	return func() tea.Msg {
		return messages.WorkspaceActivated{Project: project, Workspace: ws}
	}
}

// canAutoLaunchAssistant reports whether the assistant recorded on a new
// workspace can be started for the user. Agent tabs need tmux, so a host
// without it is left alone: the explicit new-agent paths report that with an
// install hint rather than failing silently from an auto-launch. An unfinished
// tmux check is treated as available — it settles during startup, long before
// a workspace can be created, and assuming otherwise would skip the launch.
func (a *App) canAutoLaunchAssistant(assistant string) bool {
	if assistant == "" || !a.isKnownAssistant(assistant) {
		return false
	}
	return a.tmuxAvailable || !a.tmuxCheckDone
}

// hasPendingAgentLaunch reports whether ws is a workspace created moments ago
// whose assistant is about to be launched, which activation uses to skip the
// tmux agent discovery it would otherwise queue in the same batch as the
// launch. Both run concurrently, so a delayed scan could list the session the
// launch just created and, finding no tab registered for it yet, adopt it into
// a second tab bound to the same session. A workspace created this instant owns
// no sessions this process did not just start, so the scan has nothing to find.
//
// This covers only that same-batch scan. The periodic sync tick
// (handleTmuxSyncTick) runs the same discovery against the active workspace and
// can still land inside the window between session creation and tab
// registration — a pre-existing race shared with the manual new-agent flow,
// since the arming is already consumed by the time that window opens. Closing
// it needs the center to dedupe against in-flight session names.
func (a *App) hasPendingAgentLaunch(ws *data.Workspace) bool {
	if ws == nil || len(a.pendingLaunchAssistants) == 0 {
		return false
	}
	_, ok := a.pendingLaunchAssistants[string(ws.ID())]
	return ok
}

// clearPendingAgentLaunchFor disarms a pending auto-launch when it belongs to
// ws. Armings for other workspaces are left alone.
func (a *App) clearPendingAgentLaunchFor(ws *data.Workspace) {
	if ws == nil {
		return
	}
	delete(a.pendingLaunchAssistants, string(ws.ID()))
}

// launchPendingAgent starts the assistant armed by activateCreatedWorkspace
// when ws is the workspace it was armed for. The arming is single-shot: it is
// cleared whether or not a launch command is produced.
func (a *App) launchPendingAgent(ws *data.Workspace) tea.Cmd {
	if !a.hasPendingAgentLaunch(ws) {
		return nil
	}
	assistant := a.pendingLaunchAssistants[string(ws.ID())]
	delete(a.pendingLaunchAssistants, string(ws.ID()))
	if assistant == "" || a.center == nil {
		return nil
	}
	return a.handleLaunchAgent(messages.LaunchAgent{Assistant: assistant, Workspace: ws})
}

// handleWorkspaceSetupComplete handles the WorkspaceSetupComplete message.
func (a *App) handleWorkspaceSetupComplete(msg messages.WorkspaceSetupComplete) tea.Cmd {
	if msg.Err != nil {
		// Distinguish a trust skip (the repo's .amux/workspaces.json scripts were
		// deliberately not run because the repo isn't trusted yet) from a genuine
		// setup failure, so the user knows nothing executed and why.
		if errors.Is(msg.Err, process.ErrScriptsNotTrusted) {
			var trustErr *process.ScriptsNotTrustedError
			var configHash string
			if errors.As(msg.Err, &trustErr) {
				configHash = trustErr.ConfigHash
			}
			toastCmd := a.toast.ShowWarning(fmt.Sprintf(
				"Skipped .amux/workspaces.json scripts for %s: repo not trusted yet (scripts run only after you trust this repo)",
				msg.Workspace.Name))
			dialogCmd := func() tea.Msg {
				return messages.ShowTrustScriptsDialog{Workspace: msg.Workspace, ConfigHash: configHash}
			}
			return common.SafeBatch(toastCmd, dialogCmd)
		}
		return common.ReportError(errorContext(errorServiceWorkspace, "running setup"), msg.Err, fmt.Sprintf("Setup failed for %s: %v", msg.Workspace.Name, msg.Err))
	}
	return nil
}

// handleWorkspaceCreateFailed handles the WorkspaceCreateFailed message.
func (a *App) handleWorkspaceCreateFailed(msg messages.WorkspaceCreateFailed) tea.Cmd {
	var cmds []tea.Cmd
	if msg.Workspace != nil {
		a.lifecycle.clearCreating(string(msg.Workspace.ID()))
		if cmd := a.dashboard.SetWorkspaceCreating(msg.Workspace, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if errCmd := common.ReportError(errorContext(errorServiceWorkspace, "creating workspace"), msg.Err, ""); errCmd != nil {
		cmds = append(cmds, errCmd)
	}
	return common.SafeBatch(cmds...)
}

// handleWorkspaceDeleted handles the WorkspaceDeleted message.
func (a *App) handleWorkspaceDeleted(msg messages.WorkspaceDeleted) []tea.Cmd {
	var cmds []tea.Cmd
	var postDeleteLoad tea.Cmd
	if msg.Warning != "" {
		cmds = append(cmds, a.toast.ShowWarning(msg.Warning))
	}
	if msg.Workspace != nil {
		postDeleteLoad = a.loadProjects()
		a.lifecycle.markDeletedUntilProjectsLoad(string(msg.Workspace.ID()), msg.Workspace.Root, a.lifecycle.projectsLoadToken)
		a.markWorkspaceDeleteInFlight(msg.Workspace, false)
		// Drop the deleted workspace from the active set now rather than waiting
		// for the async loadProjects -> scan reconcile, so a killed-but-not-yet-
		// reaped agent session cannot keep it shown as active by tag alone.
		delete(a.tmuxActivity.activeWorkspaceIDs, string(msg.Workspace.ID()))
		a.syncActiveWorkspacesToDashboard()
		// Navigate home only now that the delete is confirmed (moved off the
		// up-front deleteWorkspace path so a failed delete leaves the user put).
		if a.activeWorkspace != nil && a.activeWorkspace.Root == msg.Workspace.Root {
			a.goHome()
		}
		delete(a.lifecycle.dirty, string(msg.Workspace.ID()))
		// No trailing tmux cleanup here: the validated delete path already tore
		// down this workspace's sessions before removing the worktree. Re-running
		// it after the delete-in-flight flag is cleared would, on a delete-then-
		// recreate at the same project+name (same wsID, same session names), match
		// and kill the brand-new agent session by tag.
		if cmd := a.dashboard.SetWorkspaceDeleting(msg.Workspace.Root, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if a.gitStatus != nil {
			a.gitStatus.Invalidate(msg.Workspace.Root)
		}
		// Release the deleted workspace's file watch: the worktree is gone, so
		// keeping the OS watch descriptor only leaks it. Unwatch is idempotent.
		if a.fileWatcher != nil {
			a.fileWatcher.Unwatch(msg.Workspace.Root)
		}
		// Release the deleted workspace's port allocation. The worktree and its
		// scripts are already torn down on this confirmed-delete path; the runner
		// no-ops the release if a script were somehow still running, so a live
		// script's port can't be stranded. Without this the allocator's map keeps
		// one entry per workspace that ever ran a script.
		if a.workspaceService != nil {
			a.workspaceService.ReleaseWorkspacePort(msg.Workspace)
		}
		a.removeWorkspaceFromLoadedProjects(msg.Workspace)
		if a.dashboard != nil {
			a.dashboard.SetProjects(a.projects)
		}
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		newTerminal, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newTerminal
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if msg.Err != nil {
		a.removeWorkspaceFromLoadedProjects(msg.Workspace)
		if a.dashboard != nil {
			a.dashboard.SetProjects(a.projects)
		}
		if errCmd := common.ReportError(errorContext(errorServiceWorkspace, "removing workspace metadata"), msg.Err, ""); errCmd != nil {
			cmds = append(cmds, errCmd)
		}
		if postDeleteLoad != nil {
			cmds = append(cmds, postDeleteLoad)
		}
		return cmds
	}
	if postDeleteLoad == nil {
		postDeleteLoad = a.loadProjects()
	}
	cmds = append(cmds, postDeleteLoad)
	return cmds
}

func (a *App) removeWorkspaceFromLoadedProjects(ws *data.Workspace) {
	if ws == nil {
		return
	}
	wsID := string(ws.ID())
	for i := range a.projects {
		workspaces := a.projects[i].Workspaces
		filtered := make([]data.Workspace, 0, len(workspaces))
		for j := range workspaces {
			candidate := &workspaces[j]
			if string(candidate.ID()) == wsID || candidate.Root == ws.Root {
				continue
			}
			filtered = append(filtered, workspaces[j])
		}
		a.projects[i].Workspaces = filtered
	}
}

// handleWorkspaceDeleteFailed handles the WorkspaceDeleteFailed message.
func (a *App) handleWorkspaceDeleteFailed(msg messages.WorkspaceDeleteFailed) tea.Cmd {
	var cmds []tea.Cmd
	if msg.Workspace != nil {
		// Ordering is intentional: clear delete-in-flight first so the
		// persistence requeue below is not suppressed.
		a.markWorkspaceDeleteInFlight(msg.Workspace, false)
		// Clear the delete tombstone only when the worktree is still present (the
		// delete failed before removing it, so the workspace stays usable). If the
		// worktree is already gone — e.g. metadata removal failed after the worktree
		// was deleted — leave the tombstone so startup recovery finishes the delete
		// rather than resurfacing a dir-less ghost.
		if a.workspaceService != nil && dirExists(msg.Workspace.Root) {
			a.workspaceService.clearWorkspaceDeleteTombstones(msg.Workspace)
		}
		if cmd := a.dashboard.SetWorkspaceDeleting(msg.Workspace.Root, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if a.tmuxAvailable {
			if cmd := a.scanTmuxActivityNow(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if cmd := a.persistWorkspaceTabs(string(msg.Workspace.ID())); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := a.loadProjects(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if errCmd := common.ReportError(errorContext(errorServiceWorkspace, "removing workspace"), msg.Err, ""); errCmd != nil {
		cmds = append(cmds, errCmd)
	}
	return common.SafeBatch(cmds...)
}
