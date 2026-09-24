package app

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// loadProjects loads all registered projects and their workspaces.
func (a *App) loadProjects() tea.Cmd {
	if a.workspaceService == nil {
		return nil
	}
	a.lifecycle.projectsLoadToken++
	return a.workspaceService.LoadProjects(int(a.lifecycle.projectsLoadToken))
}

// rescanWorkspaces discovers git worktrees and updates the workspace store.
func (a *App) rescanWorkspaces() tea.Cmd {
	if a.workspaceService == nil {
		return nil
	}
	return a.workspaceService.RescanWorkspaces()
}

// commitWorkspaceAsync stages and commits every change in ws.Root with message,
// off the UI goroutine, reporting the outcome as messages.WorkspaceCommitted.
// The commit runs on ws's own branch through the hardened git.CommitAll; it
// never merges, pushes, or checks out the base branch. commitAllFn is a seam so
// tests can assert the wiring without touching a real repo.
func (a *App) commitWorkspaceAsync(ws *data.Workspace, message string) tea.Cmd {
	if ws == nil {
		return nil
	}
	commit := a.commitAllFn
	if commit == nil {
		commit = git.CommitAll
	}
	ctx := a.ctx
	root := ws.Root
	return func() tea.Msg {
		return messages.WorkspaceCommitted{Workspace: ws, Err: commit(ctx, root, message)}
	}
}

// handleWorkspaceCommitted reports a commit-all outcome: on failure via
// ReportError; on success a toast plus a full git-status refresh so the sidebar
// diff/status view reflects the now-clean tree.
func (a *App) handleWorkspaceCommitted(msg messages.WorkspaceCommitted) tea.Cmd {
	if msg.Err != nil {
		return common.ReportError("committing workspace changes", msg.Err, "Commit failed: "+msg.Err.Error())
	}
	var cmds []tea.Cmd
	cmds = append(cmds, a.toast.ShowSuccess("Committed changes"))
	if msg.Workspace != nil {
		cmds = append(cmds, a.requestGitStatusFull(msg.Workspace.Root))
		// A commit moves HEAD, which changes how far ahead of base it is; refresh
		// the sidebar's ahead/behind badge so it doesn't show a stale count.
		if a.sidebar != nil {
			cmds = append(cmds, a.sidebar.RefreshAheadBehind())
		}
	}
	return common.SafeBatch(cmds...)
}

// enqueueGitStatus coalesces status refreshes per root: while a refresh is in
// flight the request records a pending flag (and whether any waiter wanted
// full stats) instead of forking another git status. The result handler
// drains the pending flag into a single follow-up — the same
// in-flight/rescan shape scanTmuxActivityNow uses.
func (a *App) enqueueGitStatus(root string, full bool) tea.Cmd {
	if root == "" {
		return nil
	}
	a.initGitStatusDedup()
	if a.gitStatusInFlight[root] {
		a.gitStatusPending[root] = true
		if full {
			a.gitStatusPendingFull[root] = true
		}
		return nil
	}
	a.gitStatusInFlight[root] = true
	return a.gitStatusCmd(root, full)
}

func (a *App) initGitStatusDedup() {
	if a.gitStatusInFlight == nil {
		a.gitStatusInFlight = make(map[string]bool)
		a.gitStatusPending = make(map[string]bool)
		a.gitStatusPendingFull = make(map[string]bool)
	}
}

func (a *App) gitStatusCmd(root string, full bool) tea.Cmd {
	return func() tea.Msg {
		if a.gitStatus == nil {
			return messages.GitStatusResult{Root: root, Tracked: true}
		}
		var status *git.StatusResult
		var err error
		if full {
			status, err = a.gitStatus.Refresh(root)
		} else {
			status, err = a.gitStatus.RefreshFast(root)
		}
		if err == nil {
			a.gitStatus.UpdateCache(root, status)
		}
		return messages.GitStatusResult{Root: root, Status: status, Err: err, Tracked: true}
	}
}

// gitStatusFollowUp runs on the Update goroutine when a tracked result lands:
// it clears the root's in-flight mark and, if requests coalesced meanwhile,
// emits exactly one follow-up refresh — full if any waiter asked for it.
func (a *App) gitStatusFollowUp(root string) tea.Cmd {
	a.initGitStatusDedup()
	delete(a.gitStatusInFlight, root)
	if !a.gitStatusPending[root] {
		return nil
	}
	delete(a.gitStatusPending, root)
	full := a.gitStatusPendingFull[root]
	delete(a.gitStatusPendingFull, root)
	return a.enqueueGitStatus(root, full)
}

// requestGitStatus requests git status for a workspace using fast mode (skips line stats).
func (a *App) requestGitStatus(root string) tea.Cmd {
	return a.enqueueGitStatus(root, false)
}

// requestGitStatusBatch refreshes fast-mode status for every root in one
// sequential Cmd and returns a single GitStatusBatchResult — the reload
// path's alternative to one goroutine + one message per workspace. Roots
// already being refreshed coalesce into pending follow-ups instead of
// joining this batch.
func (a *App) requestGitStatusBatch(roots []string) tea.Cmd {
	if len(roots) == 0 {
		return nil
	}
	a.initGitStatusDedup()
	run := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == "" {
			continue
		}
		if a.gitStatusInFlight[root] {
			a.gitStatusPending[root] = true
			continue
		}
		a.gitStatusInFlight[root] = true
		run = append(run, root)
	}
	if len(run) == 0 {
		return nil
	}
	return func() tea.Msg {
		batch := messages.GitStatusBatchResult{Results: make([]messages.GitStatusResult, 0, len(run))}
		for _, root := range run {
			if a.gitStatus == nil {
				batch.Results = append(batch.Results, messages.GitStatusResult{Root: root})
				continue
			}
			status, err := a.gitStatus.RefreshFast(root)
			if err == nil {
				a.gitStatus.UpdateCache(root, status)
			}
			batch.Results = append(batch.Results, messages.GitStatusResult{Root: root, Status: status, Err: err})
		}
		return batch
	}
}

// requestGitStatusFull requests git status with full line stats (for sidebar display).
func (a *App) requestGitStatusFull(root string) tea.Cmd {
	return a.enqueueGitStatus(root, true)
}

// requestGitStatusCached requests git status using cache if available.
// On cache miss, it falls back to full mode when fallbackToFull is true,
// otherwise fast mode. Cache hits intentionally bypass the dedup maps — they
// fork no subprocess, and their result must not clear a real refresh's
// in-flight mark.
func (a *App) requestGitStatusCached(root string, fallbackToFull bool) tea.Cmd {
	if a.gitStatus != nil {
		if cached := a.gitStatus.GetCached(root); cached != nil {
			return func() tea.Msg {
				return messages.GitStatusResult{Root: root, Status: cached}
			}
		}
	}
	if fallbackToFull {
		return a.requestGitStatusFull(root)
	}
	return a.requestGitStatus(root)
}

// addProject adds a new project to the registry.
func (a *App) addProject(path string) tea.Cmd {
	if a.workspaceService == nil {
		return nil
	}
	return a.workspaceService.AddProject(path)
}

// createWorkspace creates a new workspace.
func (a *App) createWorkspace(project *data.Project, name, base, assistant string) tea.Cmd {
	if a.workspaceService == nil {
		return nil
	}
	return a.workspaceService.CreateWorkspace(project, name, base, assistant)
}

// runSetupAsync runs setup scripts asynchronously and returns a WorkspaceSetupComplete message.
func (a *App) runSetupAsync(ws *data.Workspace) tea.Cmd {
	if a.workspaceService == nil {
		return nil
	}
	return a.workspaceService.RunSetupAsync(ws)
}

// trustRepoScriptsAndRunSetupAsync trusts the reviewed repo script config and retries setup.
func (a *App) trustRepoScriptsAndRunSetupAsync(ws *data.Workspace, expectedHash string) tea.Cmd {
	if a.workspaceService == nil {
		return nil
	}
	return a.workspaceService.TrustRepoScriptsAndRunSetupAsync(ws, expectedHash)
}

// deleteWorkspace deletes a workspace. The user is NOT navigated home here: that
// happens only once the delete is confirmed (handleWorkspaceDeleted), so a
// rejected or failed delete does not bounce the user out of a workspace it left
// intact.
func (a *App) deleteWorkspace(project *data.Project, ws *data.Workspace) tea.Cmd {
	if a.workspaceService == nil {
		return nil
	}
	return wrapLifecycleCmd(a.workspaceService.DeleteWorkspace(project, ws), lifecycleOpDelete, project, ws)
}

// shelveWorkspace shelves a workspace — remove the worktree, keep branch and
// metadata for a later restore.
func (a *App) shelveWorkspace(project *data.Project, ws *data.Workspace) tea.Cmd {
	if a.workspaceService == nil {
		return nil
	}
	return wrapLifecycleCmd(a.workspaceService.ShelveWorkspace(project, ws), lifecycleOpShelve, project, ws)
}

// restoreWorkspace recreates a shelved workspace's worktree from its branch.
func (a *App) restoreWorkspace(project *data.Project, ws *data.Workspace) tea.Cmd {
	if a.workspaceService == nil {
		return nil
	}
	return wrapLifecycleCmd(a.workspaceService.RestoreWorkspace(project, ws), lifecycleOpRestore, project, ws)
}

// removeProject removes a project from the registry (does not delete files).
func (a *App) removeProject(project *data.Project) tea.Cmd {
	if project == nil {
		return func() tea.Msg {
			return messages.Error{Err: errors.New("missing project"), Context: errorContext(errorServiceWorkspace, "removing project")}
		}
	}
	if a.activeWorkspace != nil && a.activeWorkspace.Repo == project.Path {
		a.goHome()
	}
	if a.workspaceService == nil {
		return nil
	}
	return a.workspaceService.RemoveProject(project)
}

// goHome is the explicit "no active workspace" state transition: it clears the
// active workspace and resets every pane that renders workspace-scoped state.
// It runs only from message handlers (workspace deleted, project removed,
// selection rebind), never from view code.
func (a *App) goHome() {
	if a.fileWatcher != nil && a.activeWorkspace != nil {
		a.fileWatcher.Unwatch(a.activeWorkspace.Root)
	}
	a.showWelcome = true
	a.activeWorkspace = nil
	if a.center != nil {
		a.center.SetWorkspace(nil)
	}
	if a.sidebar != nil {
		a.sidebar.SetWorkspace(nil)
		a.sidebar.SetGitStatus(nil)
	}
	if a.sidebarTerminal != nil {
		_ = a.sidebarTerminal.SetWorkspace(nil)
	}
	if a.dashboard != nil {
		a.dashboard.ClearActiveRoot()
	}
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
}
