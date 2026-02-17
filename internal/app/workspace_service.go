package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/validation"
)

var (
	createWorkspaceFn = git.CreateWorkspace
	removeWorkspaceFn = git.RemoveWorkspace
	deleteBranchFn    = git.DeleteBranch
)

// AddProject adds a new project to the registry.
func (s *workspaceService) AddProject(path string) tea.Cmd {
	return func() tea.Msg {
		logging.Info("Adding project: %s", path)

		// Expand path
		if strings.HasPrefix(path, "~") {
			home, err := os.UserHomeDir()
			if err == nil {
				switch {
				case path == "~":
					path = home
				case strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\"):
					path = filepath.Join(home, path[2:])
				}
				logging.Debug("Expanded project path to: %s", path)
			}
		}

		// Validate path exists and has .git
		if err := validation.ValidateProjectPath(path); err != nil {
			logging.Warn("Path is not a git repository: %s", path)
			return messages.Error{
				Err:     err,
				Context: errorContext(errorServiceWorkspace, "adding project"),
			}
		}

		// Verify it's a real git repo (not just a .git directory)
		if !git.IsGitRepository(path) {
			logging.Warn("Path failed git repository validation: %s", path)
			return messages.Error{
				Err: &validation.ValidationError{
					Field:   "path",
					Message: "path is not a git repository",
				},
				Context: errorContext(errorServiceWorkspace, "adding project"),
			}
		}

		if s == nil || s.registry == nil {
			return messages.Error{Err: errors.New("registry unavailable"), Context: errorContext(errorServiceWorkspace, "adding project")}
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
func (s *workspaceService) CreateWorkspace(project *data.Project, name, base string, assistant ...string) tea.Cmd {
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

		if project == nil {
			return messages.WorkspaceCreateFailed{
				Err: errors.New("missing project or workspace name"),
			}
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return messages.WorkspaceCreateFailed{
				Err: errors.New("missing project or workspace name"),
			}
		}
		base = resolveBase(project.Path, base)
		ws = s.pendingWorkspace(project, name, base)
		if ws == nil {
			return messages.WorkspaceCreateFailed{
				Err: errors.New("missing project or workspace name"),
			}
		}
		name = ws.Name
		base = ws.Base

		if err := validation.ValidateWorkspaceName(name); err != nil {
			return messages.WorkspaceCreateFailed{
				Workspace: ws,
				Err:       err,
			}
		}
		if err := validation.ValidateBaseRef(base); err != nil {
			return messages.WorkspaceCreateFailed{
				Workspace: ws,
				Err:       err,
			}
		}

		workspacePath := ws.Root
		branch := name
		selectedAssistant := strings.TrimSpace(ws.Assistant)
		if len(assistant) > 0 {
			selectedAssistant = strings.TrimSpace(assistant[0])
		}
		if selectedAssistant == "" {
			return messages.WorkspaceCreateFailed{
				Workspace: ws,
				Err:       errors.New("assistant is required"),
			}
		}
		if err := validation.ValidateAssistant(selectedAssistant); err != nil {
			return messages.WorkspaceCreateFailed{
				Workspace: ws,
				Err:       err,
			}
		}
		ws.Assistant = selectedAssistant

		if !isManagedWorkspacePathForProject(s.workspacesRoot, project, workspacePath) {
			return messages.WorkspaceCreateFailed{
				Workspace: ws,
				Err:       fmt.Errorf("workspace path %s is outside managed project root", workspacePath),
			}
		}

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
				Err:       errors.New("missing project or workspace"),
			}
		}
	}

	return func() tea.Msg {
		if ws.IsPrimaryCheckout() {
			return messages.WorkspaceDeleteFailed{
				Project:   project,
				Workspace: ws,
				Err:       errors.New("cannot delete primary checkout"),
			}
		}

		projectPath := data.NormalizePath(project.Path)
		if projectPath == "" {
			return messages.WorkspaceDeleteFailed{
				Project:   project,
				Workspace: ws,
				Err:       errors.New("project path is empty"),
			}
		}

		workspaceRepo := data.NormalizePath(ws.Repo)
		if workspaceRepo == "" {
			return messages.WorkspaceDeleteFailed{
				Project:   project,
				Workspace: ws,
				Err:       errors.New("workspace repo is empty"),
			}
		}

		legacyRepoScopeMatch := false
		if projectPath != workspaceRepo {
			if !filepath.IsAbs(ws.Repo) && isLegacyManagedWorkspaceDeletePath(s.workspacesRoot, project, ws) {
				legacyRepoScopeMatch = true
			} else if isLegacyManagedWorkspaceDeletePath(s.workspacesRoot, project, ws) {
				legacyRepoScopeMatch = true
			} else {
				return messages.WorkspaceDeleteFailed{
					Project:   project,
					Workspace: ws,
					Err:       fmt.Errorf("workspace repo %s does not match project path %s", ws.Repo, project.Path),
				}
			}
		}

		if !isManagedWorkspacePathForProject(s.workspacesRoot, project, ws.Root) {
			if !legacyRepoScopeMatch && !isLegacyManagedWorkspaceDeletePath(s.workspacesRoot, project, ws) {
				return messages.WorkspaceDeleteFailed{
					Project:   project,
					Workspace: ws,
					Err:       fmt.Errorf("workspace root %s is outside managed project root", ws.Root),
				}
			}
		}

		if err := removeWorkspaceFn(projectPath, ws.Root); err != nil {
			if cleanupErr := cleanupStaleWorkspacePath(ws.Root); cleanupErr != nil {
				return messages.WorkspaceDeleteFailed{
					Project:   project,
					Workspace: ws,
					Err:       errors.Join(err, cleanupErr),
				}
			}
			logging.Warn("git remove failed for %s but stale cleanup succeeded: %v", ws.Root, err)
		}

		if err := deleteBranchFn(projectPath, ws.Branch); err != nil {
			logging.Warn("failed to delete branch %s: %v", ws.Branch, err)
		}
		if s.store != nil {
			if err := s.store.Delete(ws.ID()); err != nil {
				logging.Warn("failed to delete workspace store entry %s: %v", ws.ID(), err)
			}
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
			return messages.Error{Err: errors.New("missing project"), Context: errorContext(errorServiceWorkspace, "removing project")}
		}
	}

	return func() tea.Msg {
		if s == nil || s.registry == nil {
			return messages.Error{Err: errors.New("registry unavailable"), Context: errorContext(errorServiceWorkspace, "removing project")}
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
