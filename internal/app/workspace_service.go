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
	"github.com/andyrewlee/amux/internal/process"
)

var (
	createWorkspaceFn = git.CreateWorkspace
	removeWorkspaceFn = git.RemoveWorkspace
	deleteBranchFn    = git.DeleteBranch
)

type workspaceService struct {
	registry       ProjectRegistry
	store          WorkspaceStore
	scripts        *process.ScriptRunner
	workspacesRoot string
}

func newWorkspaceService(registry ProjectRegistry, store WorkspaceStore, scripts *process.ScriptRunner, workspacesRoot string) *workspaceService {
	return &workspaceService{
		registry:       registry,
		store:          store,
		scripts:        scripts,
		workspacesRoot: workspacesRoot,
	}
}

// LoadProjects loads all registered projects and their workspaces.
func (s *workspaceService) LoadProjects() tea.Cmd {
	return func() tea.Msg {
		if s == nil || s.registry == nil {
			return messages.ProjectsLoaded{}
		}
		paths, err := s.registry.Projects()
		if err != nil {
			return messages.Error{Err: err, Context: errorContext(errorServiceWorkspace, "loading projects")}
		}

		var projects []data.Project
		for _, path := range paths {
			if !git.IsGitRepository(path) {
				continue
			}

			project := data.NewProject(path)

			// Start from stored workspaces so metadata is authoritative.
			var storedWorkspaces []*data.Workspace
			if s.store != nil {
				storedWorkspaces, err = s.store.ListByRepo(path)
				if err != nil {
					logging.Warn("Failed to load stored workspaces for %s: %v", path, err)
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
					if s.store != nil {
						found, loadErr := s.store.LoadMetadataFor(primaryWs)
						if loadErr != nil {
							logging.Warn("Failed to load metadata for primary checkout %s: %v", path, loadErr)
						} else if !found {
							// No stored metadata - save so UI state persists across restarts
							if err := s.store.Save(primaryWs); err != nil {
								logging.Warn("Failed to save primary checkout %s: %v", path, err)
							}
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

// RescanWorkspaces discovers git worktrees and updates the workspace store.
func (s *workspaceService) RescanWorkspaces() tea.Cmd {
	return func() tea.Msg {
		if s == nil || s.registry == nil {
			return messages.RefreshDashboard{}
		}
		paths, err := s.registry.Projects()
		if err != nil {
			return messages.Error{Err: err, Context: errorContext(errorServiceWorkspace, "rescanning workspaces")}
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
				if s.store != nil {
					if err := s.store.UpsertFromDiscovery(ws); err != nil {
						logging.Warn("Failed to import workspace %s: %v", ws.Name, err)
					}
				}
			}

			var storedWorkspaces []*data.Workspace
			if s.store != nil {
				storedWorkspaces, err = s.store.ListByRepoIncludingArchived(path)
				if err != nil {
					logging.Warn("Failed to load stored workspaces for %s: %v", path, err)
					continue
				}
			}

			for _, ws := range storedWorkspaces {
				if ws == nil {
					continue
				}
				if discoveredSet[string(ws.ID())] {
					continue
				}
				if !ws.Archived {
					ws.Archived = true
					ws.ArchivedAt = time.Now()
					if s.store != nil {
						if err := s.store.Save(ws); err != nil {
							logging.Warn("Failed to archive workspace %s: %v", ws.Name, err)
						}
					}
				}
			}
		}

		return messages.RefreshDashboard{}
	}
}

// AddProject adds a new project to the registry.
func (s *workspaceService) AddProject(path string) tea.Cmd {
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
				Context: errorContext(errorServiceWorkspace, "adding project"),
			}
		}

		if s == nil || s.registry == nil {
			return messages.Error{Err: fmt.Errorf("registry unavailable"), Context: errorContext(errorServiceWorkspace, "adding project")}
		}

		// Add to registry
		if err := s.registry.AddProject(path); err != nil {
			logging.Error("Failed to add project to registry: %v", err)
			return messages.Error{Err: err, Context: errorContext(errorServiceWorkspace, "adding project")}
		}

		logging.Info("Project added successfully: %s", path)
		return messages.RefreshDashboard{}
	}
}

// CreateWorkspace creates a new workspace.
func (s *workspaceService) CreateWorkspace(project *data.Project, name, base string) tea.Cmd {
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
			s.workspacesRoot,
			project.Name,
			name,
		)

		branch := name
		ws = data.NewWorkspace(name, branch, base, project.Path, workspacePath)

		if err := createWorkspaceFn(project.Path, workspacePath, branch, base); err != nil {
			return messages.WorkspaceCreateFailed{
				Workspace: ws,
				Err:       err,
			}
		}

		// Wait for .git file to exist (race condition from workspace creation)
		gitPath := filepath.Join(workspacePath, ".git")
		if err := waitForGitPath(gitPath, gitPathWaitTimeout); err != nil {
			rollbackWorkspaceCreation(project.Path, workspacePath, branch)
			return messages.WorkspaceCreateFailed{
				Workspace: ws,
				Err:       err,
			}
		}

		// Save unified workspace
		if s.store != nil {
			if err := s.store.Save(ws); err != nil {
				rollbackWorkspaceCreation(project.Path, workspacePath, branch)
				return messages.WorkspaceCreateFailed{
					Workspace: ws,
					Err:       err,
				}
			}
		}

		// Return immediately for async setup
		return messages.WorkspaceCreated{Workspace: ws}
	}
}

// RunSetupAsync runs setup scripts asynchronously and returns a WorkspaceSetupComplete message.
func (s *workspaceService) RunSetupAsync(ws *data.Workspace) tea.Cmd {
	return func() tea.Msg {
		if s == nil || s.scripts == nil {
			return messages.WorkspaceSetupComplete{Workspace: ws}
		}
		if err := s.scripts.RunSetup(ws); err != nil {
			return messages.WorkspaceSetupComplete{Workspace: ws, Err: err}
		}
		return messages.WorkspaceSetupComplete{Workspace: ws}
	}
}

// DeleteWorkspace deletes a workspace.
func (s *workspaceService) DeleteWorkspace(project *data.Project, ws *data.Workspace) tea.Cmd {
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
		if s.store != nil {
			_ = s.store.Delete(ws.ID())
		}

		return messages.WorkspaceDeleted{
			Project:   project,
			Workspace: ws,
		}
	}
}

// RemoveProject removes a project from the registry (does not delete files).
func (s *workspaceService) RemoveProject(project *data.Project) tea.Cmd {
	if project == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("missing project"), Context: errorContext(errorServiceWorkspace, "removing project")}
		}
	}

	return func() tea.Msg {
		if s == nil || s.registry == nil {
			return messages.Error{Err: fmt.Errorf("registry unavailable"), Context: errorContext(errorServiceWorkspace, "removing project")}
		}
		if err := s.registry.RemoveProject(project.Path); err != nil {
			return messages.Error{Err: err, Context: errorContext(errorServiceWorkspace, "removing project")}
		}
		return messages.ProjectRemoved{Path: project.Path}
	}
}

func (s *workspaceService) Save(workspace *data.Workspace) error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Save(workspace)
}

func (s *workspaceService) StopAll() {
	if s == nil || s.scripts == nil {
		return
	}
	s.scripts.StopAll()
}

func waitForGitPath(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) && err != nil {
			return fmt.Errorf("failed to stat %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("missing git metadata at %s after %s", path, timeout)
		}
		time.Sleep(gitPathWaitInterval)
	}
}

func rollbackWorkspaceCreation(repoPath, workspacePath, branch string) {
	if err := removeWorkspaceFn(repoPath, workspacePath); err != nil {
		logging.Warn("Failed to roll back workspace %s: %v", workspacePath, err)
	}
	if err := deleteBranchFn(repoPath, branch); err != nil {
		logging.Warn("Failed to roll back branch %s: %v", branch, err)
	}
}
