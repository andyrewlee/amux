package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/validation"
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

		if err := s.createWorkspaceLocked(project.Path, workspacePath, branch, base); err != nil {
			return messages.WorkspaceCreateFailed{
				Workspace: ws,
				Err:       err,
			}
		}

		// Wait for .git file to exist (race condition from workspace creation)
		gitPath := filepath.Join(workspacePath, ".git")
		if err := waitForGitPath(gitPath, s.gitPathWaitTimeout); err != nil {
			s.rollbackWorkspaceCreation(project, project.Path, workspacePath, branch)
			return messages.WorkspaceCreateFailed{
				Workspace: ws,
				Err:       err,
			}
		}

		// Save unified workspace
		if s.store != nil {
			if err := s.store.Save(ws); err != nil {
				s.rollbackWorkspaceCreation(project, project.Path, workspacePath, branch)
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

func (s *workspaceService) createWorkspaceLocked(repoPath, workspacePath, branch, base string) error {
	unlock := s.lockRepoGit(repoPath)
	defer unlock()

	return s.gitOps.CreateWorkspace(repoPath, workspacePath, branch, base)
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
		wsID := ""
		wsRoot := ""
		projectPath := ""
		if ws != nil {
			wsID = string(ws.ID())
			wsRoot = ws.Root
		}
		if project != nil {
			projectPath = project.Path
		}
		logging.Warn(
			"workspace delete failed workspace_id=%s stage=validate_nil workspace_root=%s project_path=%s error=%v",
			wsID,
			wsRoot,
			projectPath,
			errors.New("missing project or workspace"),
		)
		return func() tea.Msg {
			return messages.WorkspaceDeleteFailed{
				Project:   project,
				Workspace: ws,
				Err:       errors.New("missing project or workspace"),
			}
		}
	}

	return func() tea.Msg {
		wsID := string(ws.ID())
		logging.Info(
			"workspace delete start workspace_id=%s workspace_name=%s workspace_root=%s project_path=%s branch=%s",
			wsID,
			ws.Name,
			ws.Root,
			project.Path,
			ws.Branch,
		)
		fail := func(stage string, err error) tea.Msg {
			logging.Warn(
				"workspace delete failed workspace_id=%s stage=%s workspace_name=%s workspace_root=%s project_path=%s error=%v",
				wsID,
				stage,
				ws.Name,
				ws.Root,
				project.Path,
				err,
			)
			return messages.WorkspaceDeleteFailed{
				Project:   project,
				Workspace: ws,
				Err:       err,
			}
		}

		if ws.IsPrimaryCheckout() {
			return fail("validate_primary_checkout", errors.New("cannot delete primary checkout"))
		}

		projectPath := data.NormalizePath(project.Path)
		if projectPath == "" {
			return fail("validate_project_path", errors.New("project path is empty"))
		}

		workspaceRepo := data.NormalizePath(ws.Repo)
		if workspaceRepo == "" {
			return fail("validate_workspace_repo", errors.New("workspace repo is empty"))
		}

		if projectPath != workspaceRepo {
			return fail(
				"validate_repo_match",
				fmt.Errorf("workspace repo %s does not match project path %s", ws.Repo, project.Path),
			)
		}

		if !isManagedWorkspacePathForProject(s.workspacesRoot, project, ws.Root) {
			return fail("validate_managed_root", fmt.Errorf("workspace root %s is outside managed project root", ws.Root))
		}

		// Validation passed, so this delete will proceed. Write a durable tombstone
		// FIRST so that if the process quits/crashes between here and the metadata
		// removal, startup recovery can finish the delete rather than surfacing a
		// dir-less ghost workspace. store.Delete removes the whole metadata dir,
		// clearing the tombstone on success.
		s.markDeleteTombstone(ws.ID())

		warning, failMsg := s.removeWorktreeAndBranchLocked(project, ws, projectPath, wsID, fail)
		if failMsg != nil {
			return failMsg
		}
		if s.store != nil {
			if err := s.store.Delete(ws.ID()); err != nil {
				// Worktree and branch are already gone; because loading is store-
				// first and shouldSurfaceWorkspace never stats the root, a surviving
				// metadata dir resurfaces the just-deleted workspace on the next load,
				// pointing at a missing worktree. Archive the surviving metadata as a
				// durable fallback, then return the deleted message so UI cleanup still
				// runs, with the metadata error attached for reporting.
				logging.Warn("workspace delete metadata cleanup failed workspace_id=%s error=%v", wsID, err)
				if archiveErr := s.archiveDeletedWorkspaceMetadata(ws); archiveErr != nil {
					return fail("remove_metadata", errors.Join(err, archiveErr))
				}
				return messages.WorkspaceDeleted{
					Project:   project,
					Workspace: ws,
					Err:       err,
					Warning:   warning,
				}
			}
		}
		logging.Info(
			"workspace delete succeeded workspace_id=%s workspace_name=%s workspace_root=%s project_path=%s",
			wsID,
			ws.Name,
			ws.Root,
			project.Path,
		)

		return messages.WorkspaceDeleted{
			Project:   project,
			Workspace: ws,
			Warning:   warning,
		}
	}
}

func (s *workspaceService) killWorkspaceSessionsForDelete(wsID string) {
	if s != nil && s.killWorkspaceSessions != nil {
		s.killWorkspaceSessions(wsID)
	}
}

func workspacePathGone(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

func (s *workspaceService) archiveDeletedWorkspaceMetadata(ws *data.Workspace) error {
	if s == nil || s.store == nil || ws == nil {
		return nil
	}
	archived := *ws
	archived.Archived = true
	archived.ArchivedAt = time.Now()
	if err := s.store.Save(&archived); err != nil {
		return fmt.Errorf("archive deleted workspace metadata: %w", err)
	}
	return nil
}

// removeWorktreeAndBranchLocked runs the git mutations (worktree remove + branch
// delete) under the per-repo lock so concurrent same-repo deletes do not contend
// on .git locks. It returns a non-fatal branch-delete warning and a failure
// message that is non-nil only when the worktree removal hard-failed; the caller
// then returns it and skips metadata removal.
func (s *workspaceService) removeWorktreeAndBranchLocked(
	project *data.Project, ws *data.Workspace, projectPath, wsID string,
	fail func(stage string, err error) tea.Msg,
) (warning string, failMsg tea.Msg) {
	unlock := s.lockRepoGit(projectPath)
	defer unlock()

	if err := s.gitOps.RemoveWorkspace(projectPath, ws.Root); err != nil {
		if failMsg := s.handleStaleRemoveError(project, ws, wsID, err, fail); failMsg != nil {
			return "", failMsg
		}
	}

	// The worktree removal has succeeded or an owned stale path was cleaned up.
	// Tear down any remaining workspace tmux sessions before metadata deletion,
	// but never kill sessions for a delete that failed and left the workspace.
	s.killWorkspaceSessionsForDelete(wsID)

	if err := s.gitOps.DeleteBranch(projectPath, ws.Branch); err != nil {
		logging.Warn("workspace delete branch cleanup failed workspace_id=%s branch=%s error=%v", wsID, ws.Branch, err)
		warning = fmt.Sprintf("workspace deleted but branch %s was left behind: %v", ws.Branch, err)
	}
	return warning, nil
}

// handleStaleRemoveError resolves a RemoveWorkspace error. It returns a non-nil
// failure message when the removal genuinely failed, or nil when an unregistered
// worktree path was reconciled by a managed stale-path cleanup.
func (s *workspaceService) handleStaleRemoveError(
	project *data.Project, ws *data.Workspace, wsID string, err error,
	fail func(stage string, err error) tea.Msg,
) tea.Msg {
	if !git.IsUnregisteredWorkspacePathError(err) {
		if workspacePathGone(ws.Root) {
			s.killWorkspaceSessionsForDelete(wsID)
		}
		return fail("remove_worktree", err)
	}
	if _, statErr := os.Stat(ws.Root); statErr != nil {
		if os.IsNotExist(statErr) {
			s.killWorkspaceSessionsForDelete(wsID)
			return fail("remove_worktree", err)
		}
		return fail("remove_worktree", errors.Join(err, statErr))
	}
	if !isManagedWorkspaceChildPathForProject(s.workspacesRoot, project, ws.Root) {
		return fail("remove_worktree", err)
	}
	if cleanupErr := cleanupStaleWorkspacePath(ws.Root); cleanupErr != nil {
		return fail("remove_worktree", errors.Join(err, cleanupErr))
	}
	logging.Warn("workspace delete stale cleanup workspace_id=%s workspace_root=%s remove_error=%v", wsID, ws.Root, err)
	return nil
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
