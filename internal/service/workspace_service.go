package service

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/git"
	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/process"
)

// WorkspaceService manages workspace creation, deletion, and metadata.
type WorkspaceService struct {
	registry   *data.Registry
	workspaces *data.WorkspaceStore
	config     *config.Config
	scripts    *process.ScriptRunner
	eventBus   *EventBus
}

// NewWorkspaceService creates a workspace service.
func NewWorkspaceService(registry *data.Registry, workspaces *data.WorkspaceStore, cfg *config.Config, scripts *process.ScriptRunner, bus *EventBus) *WorkspaceService {
	return &WorkspaceService{
		registry:   registry,
		workspaces: workspaces,
		config:     cfg,
		scripts:    scripts,
		eventBus:   bus,
	}
}

// GetWorkspace returns a workspace by its ID.
func (s *WorkspaceService) GetWorkspace(wsID data.WorkspaceID) (*data.Workspace, error) {
	ws, err := s.workspaces.Load(wsID)
	if err != nil {
		return nil, fmt.Errorf("loading workspace %s: %w", wsID, err)
	}
	return ws, nil
}

// CreateWorkspace creates a new git worktree workspace for the given project.
func (s *WorkspaceService) CreateWorkspace(opts CreateWorkspaceOpts) (*data.Workspace, error) {
	if opts.ProjectPath == "" || opts.Name == "" {
		return nil, fmt.Errorf("missing project path or workspace name")
	}

	if !git.IsGitRepository(opts.ProjectPath) {
		return nil, fmt.Errorf("not a git repository: %s", opts.ProjectPath)
	}

	// Resolve base branch
	var base string
	var err error
	switch opts.BranchMode {
	case "local":
		base, err = git.GetCheckedOutBase(opts.ProjectPath)
	case "custom":
		_ = git.FetchIfStale(opts.ProjectPath)
		base, err = git.ResolveCustomBranch(opts.ProjectPath, opts.CustomBranch)
	default: // "remote" or empty
		base, err = git.GetFreshRemoteBase(opts.ProjectPath)
	}
	if err != nil {
		base = "HEAD"
	}

	workspacePath := filepath.Join(
		s.config.Paths.WorkspacesRoot,
		filepath.Base(opts.ProjectPath),
		opts.Name,
	)

	branch := opts.Name
	ws := data.NewWorkspace(opts.Name, branch, base, opts.ProjectPath, workspacePath)
	ws.AllowEdits = opts.AllowEdits
	ws.Isolated = opts.Isolated
	ws.SkipPermissions = opts.SkipPermissions

	if err := git.CreateWorkspace(opts.ProjectPath, workspacePath, branch, base); err != nil {
		return nil, fmt.Errorf("creating git worktree: %w", err)
	}

	// Wait for .git file
	gitPath := filepath.Join(workspacePath, ".git")
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(gitPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Copy .env files from project root
	copyEnvFilesService(opts.ProjectPath, workspacePath)

	// Save workspace metadata
	if err := s.workspaces.Save(ws); err != nil {
		_ = git.RemoveWorkspace(opts.ProjectPath, workspacePath)
		_ = git.DeleteBranch(opts.ProjectPath, branch)
		return nil, fmt.Errorf("saving workspace: %w", err)
	}

	// Run setup scripts if configured
	if s.scripts != nil {
		if err := s.scripts.RunSetup(ws); err != nil {
			logging.Warn("Setup script failed for %s: %v", ws.Name, err)
		}
	}

	s.eventBus.Publish(NewEvent(EventWorkspaceCreated, ws))
	return ws, nil
}

// DeleteWorkspace removes a git worktree workspace.
func (s *WorkspaceService) DeleteWorkspace(wsID data.WorkspaceID) error {
	ws, err := s.workspaces.Load(wsID)
	if err != nil {
		return fmt.Errorf("loading workspace %s: %w", wsID, err)
	}

	if ws.IsPrimaryCheckout() {
		return fmt.Errorf("cannot delete primary checkout")
	}

	if err := git.RemoveWorkspace(ws.Repo, ws.Root); err != nil {
		return fmt.Errorf("removing worktree: %w", err)
	}

	if err := git.DeleteBranch(ws.Repo, ws.Branch); err != nil {
		logging.Warn("Failed to delete branch %q: %v", ws.Branch, err)
	}

	_ = s.workspaces.Delete(wsID)

	s.eventBus.Publish(NewEvent(EventWorkspaceDeleted, map[string]string{
		"workspace_id": string(wsID),
		"name":         ws.Name,
	}))
	return nil
}

// RenameWorkspace renames a workspace (branch + directory).
func (s *WorkspaceService) RenameWorkspace(wsID data.WorkspaceID, newName string) error {
	ws, err := s.workspaces.Load(wsID)
	if err != nil {
		return fmt.Errorf("loading workspace: %w", err)
	}

	newBranch := newName
	if git.BranchExists(ws.Repo, newBranch) {
		return fmt.Errorf("branch '%s' already exists", newBranch)
	}

	newRoot := filepath.Join(filepath.Dir(ws.Root), newName)
	if _, err := os.Stat(newRoot); err == nil {
		return fmt.Errorf("directory '%s' already exists", filepath.Base(newRoot))
	}

	if err := git.RenameBranch(ws.Repo, ws.Branch, newBranch); err != nil {
		return fmt.Errorf("renaming branch: %w", err)
	}

	if err := git.MoveWorkspace(ws.Repo, ws.Root, newRoot); err != nil {
		// Rollback branch rename on failure
		_ = git.RenameBranch(ws.Repo, newBranch, ws.Branch)
		return fmt.Errorf("moving workspace: %w", err)
	}

	// Delete old metadata, save new
	_ = s.workspaces.Delete(wsID)

	ws.Name = newName
	ws.Branch = newBranch
	ws.Root = newRoot
	if err := s.workspaces.Save(ws); err != nil {
		return fmt.Errorf("saving renamed workspace: %w", err)
	}

	s.eventBus.Publish(NewEvent(EventWorkspaceRenamed, map[string]string{
		"old_id":   string(wsID),
		"new_name": newName,
	}))
	return nil
}

// ArchiveWorkspace marks a workspace as archived.
func (s *WorkspaceService) ArchiveWorkspace(wsID data.WorkspaceID) error {
	ws, err := s.workspaces.Load(wsID)
	if err != nil {
		return fmt.Errorf("loading workspace: %w", err)
	}

	ws.Archived = true
	ws.ArchivedAt = time.Now()
	if err := s.workspaces.Save(ws); err != nil {
		return fmt.Errorf("archiving workspace: %w", err)
	}

	s.eventBus.Publish(NewEvent(EventWorkspaceArchived, map[string]string{
		"workspace_id": string(wsID),
	}))
	return nil
}

// FetchRemoteBase fetches the remote and returns the latest base branch ref.
func (s *WorkspaceService) FetchRemoteBase(projectPath string) (string, error) {
	return git.GetFreshRemoteBase(projectPath)
}

// copyEnvFilesService copies .env* files from source to dest (one level deep).
func copyEnvFilesService(src, dst string) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) < 4 || name[:4] != ".env" {
			continue
		}
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)
		if _, err := os.Stat(dstPath); err == nil {
			continue // Don't overwrite existing
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			continue
		}
		_ = os.WriteFile(dstPath, data, 0644)
	}
}
