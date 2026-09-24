package testutil

// Hook-driven structural fakes shared across test packages. Each method runs
// its Func hook when non-nil and otherwise returns the zero value — the same
// semantics the hand-rolled stubs they replace had. Recorders are always on
// and mutex-guarded; read them through the accessors.
//
// FakeGitOps, FakeWorkspaceStore, and FakeProjectRegistry satisfy the
// workspacesvc interfaces by name (asserted in fakes_test.go). FakeTmuxOps
// lives in internal/testutil/tmuxops — it references internal/tmux, which this
// package must not import (tmux imports process; process tests import
// testutil).

import (
	"sync"

	"github.com/andyrewlee/amux/internal/data"
)

// FakeGitOps satisfies workspacesvc.GitOperations.
type FakeGitOps struct {
	CreateWorkspaceFunc    func(repoPath, workspacePath, branch, base string) error
	RemoveWorkspaceFunc    func(repoPath, workspacePath string) error
	DeleteBranchFunc       func(repoPath, branch string) error
	DiscoverWorkspacesFunc func(project *data.Project) ([]data.Workspace, error)
}

func (f *FakeGitOps) CreateWorkspace(repoPath, workspacePath, branch, base string) error {
	if f.CreateWorkspaceFunc != nil {
		return f.CreateWorkspaceFunc(repoPath, workspacePath, branch, base)
	}
	return nil
}

func (f *FakeGitOps) RemoveWorkspace(repoPath, workspacePath string) error {
	if f.RemoveWorkspaceFunc != nil {
		return f.RemoveWorkspaceFunc(repoPath, workspacePath)
	}
	return nil
}

func (f *FakeGitOps) DeleteBranch(repoPath, branch string) error {
	if f.DeleteBranchFunc != nil {
		return f.DeleteBranchFunc(repoPath, branch)
	}
	return nil
}

func (f *FakeGitOps) DiscoverWorkspaces(project *data.Project) ([]data.Workspace, error) {
	if f.DiscoverWorkspacesFunc != nil {
		return f.DiscoverWorkspacesFunc(project)
	}
	return nil, nil
}

// FakeProjectRegistry satisfies workspacesvc.ProjectRegistry and records
// RemoveProject calls.
type FakeProjectRegistry struct {
	ProjectsFunc      func() ([]string, error)
	AddProjectFunc    func(path string) error
	RemoveProjectFunc func(path string) error

	mu           sync.Mutex
	removedPaths []string
}

func (f *FakeProjectRegistry) Projects() ([]string, error) {
	if f.ProjectsFunc != nil {
		return f.ProjectsFunc()
	}
	return nil, nil
}

func (f *FakeProjectRegistry) AddProject(path string) error {
	if f.AddProjectFunc != nil {
		return f.AddProjectFunc(path)
	}
	return nil
}

func (f *FakeProjectRegistry) RemoveProject(path string) error {
	f.mu.Lock()
	f.removedPaths = append(f.removedPaths, path)
	f.mu.Unlock()
	if f.RemoveProjectFunc != nil {
		return f.RemoveProjectFunc(path)
	}
	return nil
}

// RemovedPaths returns a copy of every path passed to RemoveProject.
func (f *FakeProjectRegistry) RemovedPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.removedPaths...)
}

// RemoveCalls returns how many times RemoveProject ran.
func (f *FakeProjectRegistry) RemoveCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.removedPaths)
}

// FakeWorkspaceStore satisfies workspacesvc.WorkspaceStore and records
// Save/Delete calls.
type FakeWorkspaceStore struct {
	ListByRepoFunc                  func(repo string) ([]*data.Workspace, error)
	ListByRepoIncludingArchivedFunc func(repo string) ([]*data.Workspace, error)
	LoadFunc                        func(id data.WorkspaceID) (*data.Workspace, error)
	LoadMetadataForFunc             func(workspace *data.Workspace) (bool, error)
	UpsertFromDiscoveryFunc         func(workspace *data.Workspace) error
	SaveFunc                        func(workspace *data.Workspace) error
	DeleteFunc                      func(id data.WorkspaceID) error
	RenameFunc                      func(id data.WorkspaceID, newName string) error
	SetEnvFunc                      func(id data.WorkspaceID, env map[string]string) error
	SetScriptsFunc                  func(id data.WorkspaceID, scripts data.ScriptsConfig, mode string) error
	MarkDeletingFunc                func(id data.WorkspaceID) error
	IsDeletingFunc                  func(id data.WorkspaceID) bool
	ClearDeletingFunc               func(id data.WorkspaceID) error
	PruneStaleFunc                  func(options data.WorkspacePruneOptions) (data.WorkspacePruneResult, error)
	ResolvedDefaultAssistantFunc    func() string

	mu         sync.Mutex
	savedIDs   []string
	saved      []*data.Workspace
	deletedIDs []data.WorkspaceID
}

func (f *FakeWorkspaceStore) ListByRepo(repo string) ([]*data.Workspace, error) {
	if f.ListByRepoFunc != nil {
		return f.ListByRepoFunc(repo)
	}
	return nil, nil
}

func (f *FakeWorkspaceStore) ListByRepoIncludingArchived(repo string) ([]*data.Workspace, error) {
	if f.ListByRepoIncludingArchivedFunc != nil {
		return f.ListByRepoIncludingArchivedFunc(repo)
	}
	return nil, nil
}

func (f *FakeWorkspaceStore) Load(id data.WorkspaceID) (*data.Workspace, error) {
	if f.LoadFunc != nil {
		return f.LoadFunc(id)
	}
	return nil, nil
}

func (f *FakeWorkspaceStore) LoadMetadataFor(workspace *data.Workspace) (bool, error) {
	if f.LoadMetadataForFunc != nil {
		return f.LoadMetadataForFunc(workspace)
	}
	return false, nil
}

func (f *FakeWorkspaceStore) UpsertFromDiscovery(workspace *data.Workspace) error {
	if f.UpsertFromDiscoveryFunc != nil {
		return f.UpsertFromDiscoveryFunc(workspace)
	}
	return nil
}

func (f *FakeWorkspaceStore) Save(ws *data.Workspace) error {
	f.mu.Lock()
	cp := *ws
	f.saved = append(f.saved, &cp)
	f.savedIDs = append(f.savedIDs, string(ws.ID()))
	f.mu.Unlock()
	if f.SaveFunc != nil {
		return f.SaveFunc(ws)
	}
	return nil
}

func (f *FakeWorkspaceStore) Delete(id data.WorkspaceID) error {
	f.mu.Lock()
	f.deletedIDs = append(f.deletedIDs, id)
	f.mu.Unlock()
	if f.DeleteFunc != nil {
		return f.DeleteFunc(id)
	}
	return nil
}

func (f *FakeWorkspaceStore) Rename(id data.WorkspaceID, newName string) error {
	if f.RenameFunc != nil {
		return f.RenameFunc(id, newName)
	}
	return nil
}

func (f *FakeWorkspaceStore) SetEnv(id data.WorkspaceID, env map[string]string) error {
	if f.SetEnvFunc != nil {
		return f.SetEnvFunc(id, env)
	}
	return nil
}

func (f *FakeWorkspaceStore) SetScripts(id data.WorkspaceID, scripts data.ScriptsConfig, mode string) error {
	if f.SetScriptsFunc != nil {
		return f.SetScriptsFunc(id, scripts, mode)
	}
	return nil
}

func (f *FakeWorkspaceStore) MarkDeleting(id data.WorkspaceID) error {
	if f.MarkDeletingFunc != nil {
		return f.MarkDeletingFunc(id)
	}
	return nil
}

func (f *FakeWorkspaceStore) IsDeleting(id data.WorkspaceID) bool {
	if f.IsDeletingFunc != nil {
		return f.IsDeletingFunc(id)
	}
	return false
}

func (f *FakeWorkspaceStore) ClearDeleting(id data.WorkspaceID) error {
	if f.ClearDeletingFunc != nil {
		return f.ClearDeletingFunc(id)
	}
	return nil
}

func (f *FakeWorkspaceStore) PruneStale(options data.WorkspacePruneOptions) (data.WorkspacePruneResult, error) {
	if f.PruneStaleFunc != nil {
		return f.PruneStaleFunc(options)
	}
	return data.WorkspacePruneResult{}, nil
}

func (f *FakeWorkspaceStore) ResolvedDefaultAssistant() string {
	if f.ResolvedDefaultAssistantFunc != nil {
		return f.ResolvedDefaultAssistantFunc()
	}
	return data.DefaultAssistant
}

// SavedIDs returns the ID of every workspace passed to Save, in call order.
func (f *FakeWorkspaceStore) SavedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.savedIDs...)
}

// SavedWorkspaces returns a copy of every workspace passed to Save (each
// saved by value at call time, so later caller mutation does not leak in).
func (f *FakeWorkspaceStore) SavedWorkspaces() []*data.Workspace {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*data.Workspace(nil), f.saved...)
}

// LastSaved returns the most recently saved workspace, or nil.
func (f *FakeWorkspaceStore) LastSaved() *data.Workspace {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.saved) == 0 {
		return nil
	}
	return f.saved[len(f.saved)-1]
}

// DeletedIDs returns every ID passed to Delete, in call order.
func (f *FakeWorkspaceStore) DeletedIDs() []data.WorkspaceID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]data.WorkspaceID(nil), f.deletedIDs...)
}
