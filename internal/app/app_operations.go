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
	return a.workspaceService.LoadProjects(a.lifecycle.projectsLoadToken)
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
	}
	return common.SafeBatch(cmds...)
}

// requestGitStatus requests git status for a workspace using fast mode (skips line stats).
func (a *App) requestGitStatus(root string) tea.Cmd {
	return func() tea.Msg {
		if a.gitStatus == nil {
			return messages.GitStatusResult{Root: root}
		}
		status, err := a.gitStatus.RefreshFast(root)
		if err == nil {
			a.gitStatus.UpdateCache(root, status)
		}
		return messages.GitStatusResult{Root: root, Status: status, Err: err}
	}
}

// requestGitStatusFull requests git status with full line stats (for sidebar display).
func (a *App) requestGitStatusFull(root string) tea.Cmd {
	return func() tea.Msg {
		if a.gitStatus == nil {
			return messages.GitStatusResult{Root: root}
		}
		status, err := a.gitStatus.Refresh(root)
		if err == nil {
			a.gitStatus.UpdateCache(root, status)
		}
		return messages.GitStatusResult{Root: root, Status: status, Err: err}
	}
}

// requestGitStatusCached requests git status using cache if available.
// On cache miss, it falls back to full mode when fallbackToFull is true,
// otherwise fast mode.
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
	return a.workspaceService.DeleteWorkspace(project, ws)
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
