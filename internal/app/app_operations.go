package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// loadProjects loads all registered projects and their workspaces
func (a *App) loadProjects() tea.Cmd {
	return func() tea.Msg {
		paths, err := a.registry.Projects()
		if err != nil {
			return messages.Error{Err: err, Context: "loading projects"}
		}

		var projects []data.Project
		for _, path := range paths {
			if !git.IsGitRepository(path) {
				continue
			}

			project := data.NewProject(path)
			workspaces, err := git.DiscoverWorkspaces(project)
			if err != nil {
				continue
			}
			for i := range workspaces {
				ws := &workspaces[i]
				meta, err := a.metadata.Load(ws)
				if err != nil {
					logging.Warn("Failed to load metadata for %s: %v", ws.Root, err)
					continue
				}
				if meta.Base != "" {
					ws.Base = meta.Base
				}
				if meta.Created != "" {
					if createdAt, err := time.Parse(time.RFC3339, meta.Created); err == nil {
						ws.Created = createdAt
					} else if createdAt, err := time.Parse(time.RFC3339Nano, meta.Created); err == nil {
						ws.Created = createdAt
					} else {
						logging.Warn("Failed to parse workspace created time for %s: %v", ws.Root, err)
					}
				}
			}
			project.Workspaces = workspaces
			projects = append(projects, *project)
		}

		return messages.ProjectsLoaded{Projects: projects}
	}
}

// requestGitStatus requests git status for a workspace (always fetches fresh)
func (a *App) requestGitStatus(root string) tea.Cmd {
	return func() tea.Msg {
		status, err := git.GetStatus(root)
		// Update cache directly (no async refresh needed, we just fetched)
		if a.statusManager != nil && err == nil {
			a.statusManager.UpdateCache(root, status)
		}
		return messages.GitStatusResult{
			Root:   root,
			Status: status,
			Err:    err,
		}
	}
}

// requestGitStatusCached requests git status using cache if available
func (a *App) requestGitStatusCached(root string) tea.Cmd {
	// Check cache first
	if a.statusManager != nil {
		if cached := a.statusManager.GetCached(root); cached != nil {
			return func() tea.Msg {
				return messages.GitStatusResult{
					Root:   root,
					Status: cached,
					Err:    nil,
				}
			}
		}
	}
	// Cache miss, fetch fresh
	return a.requestGitStatus(root)
}

// addProject adds a new project to the registry
func (a *App) addProject(path string) tea.Cmd {
	return func() tea.Msg {
		logging.Info("Adding project: %s", path)

		// Expand path
		if len(path) > 0 && path[0] == '~' {
			home, err := os.UserHomeDir()
			if err == nil {
				path = filepath.Join(home, path[1:])
				logging.Debug("Expanded path to: %s", path)
			}
		}

		// Verify it's a git repo
		if !git.IsGitRepository(path) {
			logging.Warn("Path is not a git repository: %s", path)
			return messages.Error{
				Err:     fmt.Errorf("not a git repository: %s", path),
				Context: "adding project",
			}
		}

		// Add to registry
		if err := a.registry.AddProject(path); err != nil {
			logging.Error("Failed to add project to registry: %v", err)
			return messages.Error{Err: err, Context: "adding project"}
		}

		logging.Info("Project added successfully: %s", path)
		return messages.RefreshDashboard{}
	}
}

// createWorkspace creates a new workspace
func (a *App) createWorkspace(project *data.Project, name, base string) tea.Cmd {
	return func() (msg tea.Msg) {
		var ws *data.Workspace
		defer func() {
			if r := recover(); r != nil {
				logging.Error("panic in createWorkspace: %v", r)
				msg = messages.WorkspaceCreateFailed{
					Workspace: ws,
					Err:       fmt.Errorf("create workspace panicked: %v", r),
				}
			}
		}()

		if project == nil || name == "" {
			return messages.WorkspaceCreateFailed{
				Err: fmt.Errorf("missing project or workspace name"),
			}
		}

		workspacePath := filepath.Join(
			a.config.Paths.WorkspacesRoot,
			project.Name,
			name,
		)

		branch := name
		ws = data.NewWorkspace(name, branch, base, project.Path, workspacePath)

		if err := git.CreateWorkspace(project.Path, workspacePath, branch, base); err != nil {
			return messages.WorkspaceCreateFailed{
				Workspace: ws,
				Err:       err,
			}
		}

		// Wait for .git file to exist (race condition from workspace creation)
		gitPath := filepath.Join(workspacePath, ".git")
		for i := 0; i < 10; i++ {
			if _, err := os.Stat(gitPath); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		meta := &data.Metadata{
			Name:       name,
			Branch:     branch,
			Repo:       project.Path,
			Base:       base,
			Created:    time.Now().Format(time.RFC3339),
			Assistant:  "claude",
			Runtime:    "local",
			ScriptMode: "nonconcurrent",
			Env:        make(map[string]string),
		}

		if err := a.metadata.Save(ws, meta); err != nil {
			_ = git.RemoveWorkspace(project.Path, workspacePath)
			_ = git.DeleteBranch(project.Path, branch)
			return messages.WorkspaceCreateFailed{
				Workspace: ws,
				Err:       err,
			}
		}

		// Return immediately with metadata for async setup
		return messages.WorkspaceCreated{Workspace: ws, Meta: meta}
	}
}

// runSetupAsync runs setup scripts asynchronously and returns a WorkspaceSetupComplete message
func (a *App) runSetupAsync(ws *data.Workspace, meta *data.Metadata) tea.Cmd {
	return func() tea.Msg {
		if err := a.scripts.RunSetup(ws, meta); err != nil {
			return messages.WorkspaceSetupComplete{Workspace: ws, Err: err}
		}
		return messages.WorkspaceSetupComplete{Workspace: ws}
	}
}

// deleteWorkspace deletes a workspace
func (a *App) deleteWorkspace(project *data.Project, ws *data.Workspace) tea.Cmd {
	// Defensive nil checks
	if project == nil || ws == nil {
		return func() tea.Msg {
			return messages.WorkspaceDeleteFailed{
				Project:   project,
				Workspace: ws,
				Err:       fmt.Errorf("missing project or workspace"),
			}
		}
	}

	// Clear UI components if deleting the active workspace
	if a.activeWorkspace != nil && a.activeWorkspace.Root == ws.Root {
		a.goHome()
	}

	return func() tea.Msg {
		if ws.IsPrimaryCheckout() {
			return messages.WorkspaceDeleteFailed{
				Project:   project,
				Workspace: ws,
				Err:       fmt.Errorf("cannot delete primary checkout"),
			}
		}

		if err := git.RemoveWorkspace(project.Path, ws.Root); err != nil {
			return messages.WorkspaceDeleteFailed{
				Project:   project,
				Workspace: ws,
				Err:       err,
			}
		}

		_ = git.DeleteBranch(project.Path, ws.Branch)
		_ = a.metadata.Delete(ws)

		return messages.WorkspaceDeleted{
			Project:   project,
			Workspace: ws,
		}
	}
}

// removeProject removes a project from the registry (does not delete files).
func (a *App) removeProject(project *data.Project) tea.Cmd {
	if project == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("missing project"), Context: "removing project"}
		}
	}

	if a.activeWorkspace != nil && a.activeWorkspace.Repo == project.Path {
		a.goHome()
	}

	return func() tea.Msg {
		if err := a.registry.RemoveProject(project.Path); err != nil {
			return messages.Error{Err: err, Context: "removing project"}
		}
		return messages.ProjectRemoved{Path: project.Path}
	}
}
