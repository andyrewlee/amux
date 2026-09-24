package workspacesvc

import (
	"sync"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
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

type Service struct {
	registry           ProjectRegistry
	store              WorkspaceStore
	scripts            *process.ScriptRunner
	workspacesRoot     string
	gitOps             GitOperations
	gitPathWaitTimeout time.Duration
	// mutationInFlight reports whether a workspace is currently mid-mutation. It is
	// wired to the App's guard in app_init; nil when the service is constructed
	// directly (e.g. in tests) and then treated as "never in flight". The probe
	// takes the workspace (not a bare ID) so the app can consult every identity
	// form — IDs drift with path existence mid-mutation.
	mutationInFlight func(ws *data.Workspace) bool
	// mutationInFlightGuard runs a store mutation only when the workspace is not
	// mid-mutation, keeping the check atomic with App delete-state updates.
	mutationInFlightGuard func(ws *data.Workspace, fn func()) bool
	// killWorkspaceSessions synchronously tears down a workspace's tmux sessions.
	// Wired in app_init; nil in directly-constructed services (a no-op). Called
	// only after worktree removal succeeds, so failed deletes leave live sessions
	// intact.
	killWorkspaceSessions func(wsID string) error
	// killWorkspaceSessionNames tears down exact persisted session names. This
	// complements workspace-ID cleanup for sessions created before an ID
	// normalization migration.
	killWorkspaceSessionNames func(sessionNames []string) error
	// repoGitLocks serializes git worktree/branch mutations per repository (keyed
	// by normalized project path) so concurrent create/delete of workspaces in the
	// same repo do not contend on .git locks (index.lock / packed-refs).
	repoGitLocks sync.Map
}

// lockRepoGit acquires the per-repo git mutation lock and returns the unlock
// closure. Callers must hold it only around git CLI mutations, not over the
// flock-serialized metadata store.
func (s *Service) lockRepoGit(repoPath string) func() {
	actual, _ := s.repoGitLocks.LoadOrStore(data.NormalizePath(repoPath), &sync.Mutex{})
	mu, _ := actual.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// isMutationInFlight reports whether the workspace is mid-mutation. It is nil-safe so
// a service built without the predicate (tests) treats every workspace as not in
// flight.
func (s *Service) isMutationInFlight(ws *data.Workspace) bool {
	return s != nil && s.mutationInFlight != nil && s.mutationInFlight(ws)
}

func (s *Service) runUnlessMutationInFlight(ws *data.Workspace, fn func()) bool {
	if s == nil {
		return false
	}
	if s.mutationInFlightGuard != nil {
		return s.mutationInFlightGuard(ws, fn)
	}
	if s.isMutationInFlight(ws) {
		return false
	}
	if fn != nil {
		fn()
	}
	return true
}

func New(registry ProjectRegistry, store WorkspaceStore, scripts *process.ScriptRunner, workspacesRoot string) *Service {
	s := &Service{
		registry:           registry,
		store:              store,
		scripts:            scripts,
		workspacesRoot:     workspacesRoot,
		gitOps:             defaultGitOps{},
		gitPathWaitTimeout: 3 * time.Second,
	}
	// Optional capabilities are asserted at the call site, but a missing one
	// must never be silent — log once at construction so a test stub
	// downgrading behavior is visible in output. Both have identical-semantics
	// fallbacks, so this is Debug not Warn.
	if store != nil {
		if _, ok := store.(recordSetLister); !ok {
			logging.Debug("workspace store lacks ListAll; loads fall back to per-repo reads")
		}
		if _, ok := store.(workspaceRepoMetadataDeleter); !ok {
			logging.Debug("workspace store lacks DeleteByRepo; project removal falls back to per-record deletes")
		}
	}
	return s
}

func (s *Service) resolvedDefaultAssistant() string {
	if s != nil && s.store != nil {
		return s.store.ResolvedDefaultAssistant()
	}
	return data.DefaultAssistant
}
