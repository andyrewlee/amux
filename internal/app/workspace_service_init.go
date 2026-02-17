package app

import (
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/process"
)

// GitOperations abstracts git workspace operations for testability.
type GitOperations interface {
	CreateWorkspace(repoPath, workspacePath, branch, base string) error
	RemoveWorkspace(repoPath, workspacePath string) error
	DeleteBranch(repoPath, branch string) error
	DiscoverWorkspaces(project *data.Project) ([]data.Workspace, error)
}

type defaultGitOps struct{}

func (defaultGitOps) CreateWorkspace(repoPath, workspacePath, branch, base string) error {
	return git.CreateWorkspace(repoPath, workspacePath, branch, base)
}

func (defaultGitOps) RemoveWorkspace(repoPath, workspacePath string) error {
	return git.RemoveWorkspace(repoPath, workspacePath)
}

func (defaultGitOps) DeleteBranch(repoPath, branch string) error {
	return git.DeleteBranch(repoPath, branch)
}

func (defaultGitOps) DiscoverWorkspaces(project *data.Project) ([]data.Workspace, error) {
	return git.DiscoverWorkspaces(project)
}

type workspaceService struct {
	registry       ProjectRegistry
	store          WorkspaceStore
	scripts        *process.ScriptRunner
	workspacesRoot string
	gitOps         GitOperations
}

func newWorkspaceService(registry ProjectRegistry, store WorkspaceStore, scripts *process.ScriptRunner, workspacesRoot string) *workspaceService {
	return &workspaceService{
		registry:       registry,
		store:          store,
		scripts:        scripts,
		workspacesRoot: workspacesRoot,
		gitOps:         defaultGitOps{},
	}
}

func (s *workspaceService) resolvedDefaultAssistant() string {
	if s != nil && s.store != nil {
		return s.store.ResolvedDefaultAssistant()
	}
	return data.DefaultAssistant
}
