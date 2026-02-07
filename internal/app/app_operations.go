package app

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// loadProjects loads all registered projects and their workspaces.
func (a *App) loadProjects() tea.Cmd {
	if a.workspaceService == nil {
		return nil
	}
	return a.workspaceService.LoadProjects()
}

// rescanWorkspaces discovers git worktrees and updates the workspace store.
func (a *App) rescanWorkspaces() tea.Cmd {
	if a.workspaceService == nil {
		return nil
	}
	return a.workspaceService.RescanWorkspaces()
}

// requestGitStatus requests git status for a workspace (always fetches fresh).
func (a *App) requestGitStatus(root string) tea.Cmd {
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
func (a *App) requestGitStatusCached(root string) tea.Cmd {
	if a.gitStatus != nil {
		if cached := a.gitStatus.GetCached(root); cached != nil {
			return func() tea.Msg {
				return messages.GitStatusResult{Root: root, Status: cached}
			}
		}
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
func (a *App) createWorkspace(project *data.Project, name, base string) tea.Cmd {
	if a.workspaceService == nil {
		return nil
	}
	return a.workspaceService.CreateWorkspace(project, name, base)
}

// runSetupAsync runs setup scripts asynchronously and returns a WorkspaceSetupComplete message.
func (a *App) runSetupAsync(ws *data.Workspace) tea.Cmd {
	if a.workspaceService == nil {
		return nil
	}
	return a.workspaceService.RunSetupAsync(ws)
}

// deleteWorkspace deletes a workspace.
func (a *App) deleteWorkspace(project *data.Project, ws *data.Workspace) tea.Cmd {
	if a.activeWorkspace != nil && ws != nil && a.activeWorkspace.Root == ws.Root {
		a.goHome()
	}
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
