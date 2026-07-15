package app

import (
	"sync"
	"time"

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
	registry           ProjectRegistry
	store              WorkspaceStore
	scripts            *process.ScriptRunner
	workspacesRoot     string
	gitOps             GitOperations
	gitPathWaitTimeout time.Duration
	// deleteInFlight reports whether a workspace is currently mid-delete. It is
	// wired to the App's guard in app_init; nil when the service is constructed
	// directly (e.g. in tests) and then treated as "never in flight".
	deleteInFlight func(wsID string) bool
	// deleteInFlightGuard runs a store mutation only when the workspace is not
	// mid-delete, keeping the check atomic with App delete-state updates.
	deleteInFlightGuard func(wsID string, fn func()) bool
	// killWorkspaceSessions synchronously tears down a workspace's tmux sessions.
	// Wired in app_init; nil in directly-constructed services (a no-op). Called
	// before worktree removal so no session process can hold or repopulate the
	// worktree while it is being deleted.
	killWorkspaceSessions func(wsID string)
	// teardownProcesses kills orphaned service process groups still referencing
	// the workspace root and fails when any survive, making the delete
	// transactional: stop services → kill sessions → kill survivors → verify →
	// only then remove the worktree. Wired to process.TeardownWorkspaceProcesses
	// in app_init; nil in directly-constructed services (a no-op).
	teardownProcesses func(root string) (process.TeardownResult, error)
	// repoGitLocks serializes git worktree/branch mutations per repository (keyed
	// by normalized project path) so concurrent create/delete of workspaces in the
	// same repo do not contend on .git locks (index.lock / packed-refs).
	repoGitLocks sync.Map
}

// lockRepoGit acquires the per-repo git mutation lock and returns the unlock
// closure. Callers must hold it only around git CLI mutations, not over the
// flock-serialized metadata store.
func (s *workspaceService) lockRepoGit(repoPath string) func() {
	actual, _ := s.repoGitLocks.LoadOrStore(data.NormalizePath(repoPath), &sync.Mutex{})
	mu, _ := actual.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// isDeleteInFlight reports whether the workspace is mid-delete. It is nil-safe so
// a service built without the predicate (tests) treats every workspace as not in
// flight.
func (s *workspaceService) isDeleteInFlight(wsID string) bool {
	return s != nil && s.deleteInFlight != nil && s.deleteInFlight(wsID)
}

func (s *workspaceService) runUnlessDeleteInFlight(wsID string, fn func()) bool {
	if s == nil {
		return false
	}
	if s.deleteInFlightGuard != nil {
		return s.deleteInFlightGuard(wsID, fn)
	}
	if s.isDeleteInFlight(wsID) {
		return false
	}
	if fn != nil {
		fn()
	}
	return true
}

func newWorkspaceService(registry ProjectRegistry, store WorkspaceStore, scripts *process.ScriptRunner, workspacesRoot string) *workspaceService {
	return &workspaceService{
		registry:           registry,
		store:              store,
		scripts:            scripts,
		workspacesRoot:     workspacesRoot,
		gitOps:             defaultGitOps{},
		gitPathWaitTimeout: 3 * time.Second,
	}
}

func (s *workspaceService) resolvedDefaultAssistant() string {
	if s != nil && s.store != nil {
		return s.store.ResolvedDefaultAssistant()
	}
	return data.DefaultAssistant
}
