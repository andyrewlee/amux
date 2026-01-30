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

			// Start from stored workspaces so metadata is authoritative.
			storedWorkspaces, err := a.workspaces.ListByRepo(path)
			if err != nil {
				logging.Warn("Failed to load stored workspaces for %s: %v", path, err)
			}
			needsLegacyImport, err := a.workspaces.HasLegacyWorkspaces(path)
			if err != nil {
				logging.Warn("Failed to check legacy workspaces for %s: %v", path, err)
			}
			if needsLegacyImport {
				discoveredWorkspaces, err := git.DiscoverWorkspaces(project)
				if err != nil {
					logging.Warn("Failed to discover workspaces for %s: %v", path, err)
				} else {
					for i := range discoveredWorkspaces {
						ws := &discoveredWorkspaces[i]
						if err := a.workspaces.UpsertFromDiscoveryPreserveArchived(ws); err != nil {
							logging.Warn("Failed to import workspace %s: %v", ws.Name, err)
						}
					}
					storedWorkspaces, err = a.workspaces.ListByRepo(path)
					if err != nil {
						logging.Warn("Failed to reload stored workspaces for %s: %v", path, err)
					}
				}
			}

			var workspaces []data.Workspace
			for _, ws := range storedWorkspaces {
				workspaces = append(workspaces, *ws)
			}

			// Stored workspaces not discovered on disk are already included (store-first).
			// These may be workspaces whose directories were deleted.

			// Add primary checkout as transient workspace if not present
			hasPrimary := false
			for _, ws := range workspaces {
				if ws.IsPrimaryCheckout() {
					hasPrimary = true
					break
				}
			}

			if !hasPrimary {
				branch, err := git.GetCurrentBranch(path)
				if err != nil {
					logging.Warn("Failed to get current branch for %s: %v", path, err)
					// Skip creating primary workspace if we can't get the branch -
					// the repo may be in a bad state or no longer a valid git repo
				} else {
					primaryWs := data.NewWorkspace(
						filepath.Base(path), // name
						branch,              // branch
						"",                  // base
						path,                // repo
						path,                // root (same as repo for primary)
					)
					// Load any persisted UI state (OpenTabs, etc.) for the primary checkout
					found, loadErr := a.workspaces.LoadMetadataFor(primaryWs)
					if loadErr != nil {
						logging.Warn("Failed to load metadata for primary checkout %s: %v", path, loadErr)
					} else if !found {
						// No stored metadata - save so UI state persists across restarts
						if err := a.workspaces.Save(primaryWs); err != nil {
							logging.Warn("Failed to save primary checkout %s: %v", path, err)
						}
					}
					workspaces = append([]data.Workspace{*primaryWs}, workspaces...)
				}
			}

			project.Workspaces = workspaces
			projects = append(projects, *project)
		}

		return messages.ProjectsLoaded{Projects: projects}
	}
}

// rescanWorkspaces discovers git worktrees and updates the workspace store.
func (a *App) rescanWorkspaces() tea.Cmd {
	return func() tea.Msg {
		paths, err := a.registry.Projects()
		if err != nil {
			return messages.Error{Err: err, Context: "rescanning workspaces"}
		}

		for _, path := range paths {
			if !git.IsGitRepository(path) {
				continue
			}

			project := data.NewProject(path)
			discoveredWorkspaces, err := git.DiscoverWorkspaces(project)
			if err != nil {
				logging.Warn("Failed to discover workspaces for %s: %v", path, err)
				continue
			}

			discoveredSet := make(map[string]bool, len(discoveredWorkspaces))
			for i := range discoveredWorkspaces {
				ws := &discoveredWorkspaces[i]
				discoveredSet[string(ws.ID())] = true
				if err := a.workspaces.UpsertFromDiscovery(ws); err != nil {
					logging.Warn("Failed to import workspace %s: %v", ws.Name, err)
				}
			}

			storedWorkspaces, err := a.workspaces.ListByRepoIncludingArchived(path)
			if err != nil {
				logging.Warn("Failed to load stored workspaces for %s: %v", path, err)
				continue
			}

			for _, ws := range storedWorkspaces {
				if ws == nil || ws.Root == "" {
					continue
				}
				if discoveredSet[string(ws.ID())] {
					continue
				}
				if !ws.Archived {
					ws.Archived = true
					ws.ArchivedAt = time.Now()
					if err := a.workspaces.Save(ws); err != nil {
						logging.Warn("Failed to archive workspace %s: %v", ws.Name, err)
					}
				}
			}
		}

		return messages.RefreshDashboard{}
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

		// Save unified workspace
		if err := a.workspaces.Save(ws); err != nil {
			_ = git.RemoveWorkspace(project.Path, workspacePath)
			_ = git.DeleteBranch(project.Path, branch)
			return messages.WorkspaceCreateFailed{
				Workspace: ws,
				Err:       err,
			}
		}

		// Return immediately for async setup
		return messages.WorkspaceCreated{Workspace: ws}
	}
}

// runSetupAsync runs setup scripts asynchronously and returns a WorkspaceSetupComplete message
func (a *App) runSetupAsync(ws *data.Workspace) tea.Cmd {
	return func() tea.Msg {
		if err := a.scripts.RunSetup(ws); err != nil {
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
		_ = a.workspaces.Delete(ws.ID())

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
