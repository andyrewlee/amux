package workspacesvc

import (
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// ProjectRegistry is the minimal interface used by the service for project
// tracking.
type ProjectRegistry interface {
	Projects() ([]string, error)
	AddProject(path string) error
	RemoveProject(path string) error
}

// WorkspaceStore is the minimal interface used by the service for workspace
// metadata.
//
// Tombstone (MarkDeleting/IsDeleting/ClearDeleting) and PruneStale are declared
// here — not recovered by runtime asserts — because their absence silently
// disables crash recovery and metadata reconciliation: a fake lacking them
// would pass tests while production semantics differ. The batch/optimization
// capabilities (recordSetLister, workspaceRepoMetadataDeleter) stay asserted
// at the call site; they have identical-semantics fallbacks, and
// New logs when they're absent.
type WorkspaceStore interface {
	ListByRepo(repo string) ([]*data.Workspace, error)
	ListByRepoIncludingArchived(repo string) ([]*data.Workspace, error)
	Load(id data.WorkspaceID) (*data.Workspace, error)
	LoadMetadataFor(workspace *data.Workspace) (bool, error)
	UpsertFromDiscovery(workspace *data.Workspace) error
	Save(workspace *data.Workspace) error
	Delete(id data.WorkspaceID) error
	Rename(id data.WorkspaceID, newName string) error
	SetEnv(id data.WorkspaceID, env map[string]string) error
	SetScripts(id data.WorkspaceID, scripts data.ScriptsConfig, mode string) error
	MarkDeleting(id data.WorkspaceID) error
	IsDeleting(id data.WorkspaceID) bool
	ClearDeleting(id data.WorkspaceID) error
	PruneStale(options data.WorkspacePruneOptions) (data.WorkspacePruneResult, error)
	ResolvedDefaultAssistant() string
}

// Deps carries the service's injected collaborators beyond New's required
// arguments. The app wires the lifecycle guards and session-teardown hooks
// after construction; tests inject fake git ops and shorter timeouts. Nil
// fields leave the existing value in place.
type Deps struct {
	// MutationInFlight/MutationInFlightGuard take the workspace itself (not a
	// single ID) because every ID form drifts with path existence — the app
	// probes the full identity set plus the root bridge.
	MutationInFlight          func(ws *data.Workspace) bool
	MutationInFlightGuard     func(ws *data.Workspace, fn func()) bool
	KillWorkspaceSessions     func(wsID string) error
	KillWorkspaceSessionNames func(sessionNames []string) error
	GitOps                    GitOperations
	GitPathWaitTimeout        time.Duration
}

// Configure applies the non-nil fields of d to the service's injectable
// seams. Called once during app wiring; also the test seam for fake git ops
// and shorter timeouts.
func (s *Service) Configure(d Deps) {
	if d.MutationInFlight != nil {
		s.mutationInFlight = d.MutationInFlight
	}
	if d.MutationInFlightGuard != nil {
		s.mutationInFlightGuard = d.MutationInFlightGuard
	}
	if d.KillWorkspaceSessions != nil {
		s.killWorkspaceSessions = d.KillWorkspaceSessions
	}
	if d.KillWorkspaceSessionNames != nil {
		s.killWorkspaceSessionNames = d.KillWorkspaceSessionNames
	}
	if d.GitOps != nil {
		s.gitOps = d.GitOps
	}
	if d.GitPathWaitTimeout != 0 {
		s.gitPathWaitTimeout = d.GitPathWaitTimeout
	}
}

// workspaceErrContext builds the "workspace: <detail>" Context string this
// service's errors carry — the same shape errorContext produces for the rest
// of the app, kept local so the package does not import the app's helper.
func workspaceErrContext(detail string) string {
	if detail == "" {
		return "workspace"
	}
	return "workspace: " + detail
}
