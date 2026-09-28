package workspacesvc

import (
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
)

// countingWorkspaceStore wraps a real WorkspaceStore and records how often the
// shared-snapshot and per-repo list paths run. Embedding the concrete store
// keeps the optional recordSetLister capability, so listByRepoFromSet must
// pick the snapshot.
type countingWorkspaceStore struct {
	*data.WorkspaceStore
	listAllCalls atomic.Int32
	byRepoCalls  atomic.Int32
}

func (c *countingWorkspaceStore) ListAll() (*data.WorkspaceRecordSet, error) {
	c.listAllCalls.Add(1)
	return c.WorkspaceStore.ListAll()
}

func (c *countingWorkspaceStore) ListByRepo(repo string) ([]*data.Workspace, error) {
	c.byRepoCalls.Add(1)
	return c.WorkspaceStore.ListByRepo(repo)
}

func (c *countingWorkspaceStore) ListByRepoIncludingArchived(repo string) ([]*data.Workspace, error) {
	c.byRepoCalls.Add(1)
	return c.WorkspaceStore.ListByRepoIncludingArchived(repo)
}

// noSnapshotStore hides ListAll behind the interface-typed embed so the store
// does not satisfy recordSetLister; listByRepoFromSet must fall back to the
// per-repo reads this wrapper still delegates.
type noSnapshotStore struct {
	WorkspaceStore
	byRepoCalls *atomic.Int32
}

func (n noSnapshotStore) ListByRepo(repo string) ([]*data.Workspace, error) {
	n.byRepoCalls.Add(1)
	return n.WorkspaceStore.ListByRepo(repo)
}

func (n noSnapshotStore) ListByRepoIncludingArchived(repo string) ([]*data.Workspace, error) {
	n.byRepoCalls.Add(1)
	return n.WorkspaceStore.ListByRepoIncludingArchived(repo)
}

// failingSnapshotStore satisfies recordSetLister but always fails, so
// loadWorkspaceRecordSet must fall back to per-repo reads.
type failingSnapshotStore struct {
	WorkspaceStore
	listAllCalls *atomic.Int32
	byRepoCalls  *atomic.Int32
}

func (f failingSnapshotStore) ListAll() (*data.WorkspaceRecordSet, error) {
	f.listAllCalls.Add(1)
	return nil, errors.New("snapshot unavailable")
}

func (f failingSnapshotStore) ListByRepo(repo string) ([]*data.Workspace, error) {
	f.byRepoCalls.Add(1)
	return f.WorkspaceStore.ListByRepo(repo)
}

func (f failingSnapshotStore) ListByRepoIncludingArchived(repo string) ([]*data.Workspace, error) {
	f.byRepoCalls.Add(1)
	return f.WorkspaceStore.ListByRepoIncludingArchived(repo)
}

// snapshotFixture seeds a real WorkspaceStore for two registered projects:
// each gets a primary checkout and one live managed workspace; project A also
// gets an intentional shelf and an accidental archive. The caller asserts the
// returned store's views match how LoadProjects must group them.
func snapshotFixture(t *testing.T, repoA, repoB, managedRoot string) *data.WorkspaceStore {
	t.Helper()
	store := data.NewWorkspaceStore(t.TempDir())
	save := func(ws *data.Workspace) {
		t.Helper()
		if err := store.Save(ws); err != nil {
			t.Fatalf("Save(%q) error = %v", ws.Name, err)
		}
	}
	save(data.NewWorkspace("repoA", "main", "", repoA, repoA))
	save(data.NewWorkspace("repoB", "main", "", repoB, repoB))
	save(data.NewWorkspace("feat-a", "feat-a", "main", repoA, filepath.Join(managedRoot, "repoA", "feat-a")))
	shelfA := data.NewWorkspace("shelf-a", "shelf-a", "main", repoA, filepath.Join(managedRoot, "repoA", "shelf-a"))
	shelfA.Archived = true
	shelfA.Shelved = true
	save(shelfA)
	accidentalA := data.NewWorkspace("accidental-a", "gone", "main", repoA, filepath.Join(managedRoot, "repoA", "accidental-a"))
	accidentalA.Archived = true
	save(accidentalA)
	save(data.NewWorkspace("feat-b", "feat-b", "main", repoB, filepath.Join(managedRoot, "repoB", "feat-b")))
	return store
}

func snapshotProjectsFromMsg(t *testing.T, msg any, want int) []data.Project {
	t.Helper()
	loaded, ok := msg.(messages.ProjectsLoaded)
	if !ok {
		t.Fatalf("LoadProjects message = %T, want messages.ProjectsLoaded", msg)
	}
	if len(loaded.Projects) != want {
		t.Fatalf("loaded %d projects, want %d", len(loaded.Projects), want)
	}
	return loaded.Projects
}

// assertSnapshotFixtureRows checks row membership, not order: ListAll orders
// by workspace ID, which is derived from the random TempDir fixture paths, so
// a stable row order cannot be asserted across runs.
func assertSnapshotFixtureRows(t *testing.T, projects []data.Project) {
	t.Helper()
	workspaceNames := func(p data.Project) map[string]bool {
		names := make(map[string]bool, len(p.Workspaces))
		for _, ws := range p.Workspaces {
			names[ws.Name] = true
		}
		return names
	}
	projectA, projectB := projects[0], projects[1]
	if names := workspaceNames(projectA); len(names) != 2 || !names["repoA"] || !names["feat-a"] {
		t.Fatalf("project A workspaces = %v, want {repoA, feat-a}", projectA.Workspaces)
	}
	if len(projectA.ShelvedWorkspaces) != 1 || projectA.ShelvedWorkspaces[0].Name != "shelf-a" {
		t.Fatalf("project A shelves = %v, want [shelf-a] only (accidental archive excluded)", projectA.ShelvedWorkspaces)
	}
	if names := workspaceNames(projectB); len(names) != 2 || !names["repoB"] || !names["feat-b"] {
		t.Fatalf("project B workspaces = %v, want {repoB, feat-b}", projectB.Workspaces)
	}
	if len(projectB.ShelvedWorkspaces) != 0 {
		t.Fatalf("project B shelves = %v, want none", projectB.ShelvedWorkspaces)
	}
}

func TestLoadProjectsShelfRowsReuseMetadataSnapshot(t *testing.T) {
	repoA := testutil.InitRepo(t)
	repoB := testutil.InitRepo(t)
	managed := t.TempDir()
	counting := &countingWorkspaceStore{WorkspaceStore: snapshotFixture(t, repoA, repoB, managed)}
	reg := &testutil.FakeProjectRegistry{
		ProjectsFunc: func() ([]string, error) { return []string{repoA, repoB}, nil },
	}
	svc := New(reg, counting, nil, managed)

	projects := snapshotProjectsFromMsg(t, svc.LoadProjects(2)(), 2)
	assertSnapshotFixtureRows(t, projects)

	if got := counting.listAllCalls.Load(); got != 1 {
		t.Fatalf("ListAll calls = %d, want exactly 1 shared snapshot", got)
	}
	if got := counting.byRepoCalls.Load(); got != 0 {
		t.Fatalf("per-repo list calls = %d, want 0 — shelves must reuse the snapshot", got)
	}
}

func TestLoadProjectsShelfRowsFallbackWithoutSnapshotStore(t *testing.T) {
	repoA := testutil.InitRepo(t)
	repoB := testutil.InitRepo(t)
	managed := t.TempDir()
	var byRepoCalls atomic.Int32
	store := noSnapshotStore{
		WorkspaceStore: snapshotFixture(t, repoA, repoB, managed),
		byRepoCalls:    &byRepoCalls,
	}
	reg := &testutil.FakeProjectRegistry{
		ProjectsFunc: func() ([]string, error) { return []string{repoA, repoB}, nil },
	}
	svc := New(reg, store, nil, managed)

	projects := snapshotProjectsFromMsg(t, svc.LoadProjects(2)(), 2)
	assertSnapshotFixtureRows(t, projects)

	if got := byRepoCalls.Load(); got != 4 {
		t.Fatalf("per-repo list calls = %d, want 4 (live + shelf per project)", got)
	}
}

func TestLoadProjectsShelfRowsFallbackOnSnapshotError(t *testing.T) {
	repoA := testutil.InitRepo(t)
	repoB := testutil.InitRepo(t)
	managed := t.TempDir()
	var listAllCalls, byRepoCalls atomic.Int32
	store := failingSnapshotStore{
		WorkspaceStore: snapshotFixture(t, repoA, repoB, managed),
		listAllCalls:   &listAllCalls,
		byRepoCalls:    &byRepoCalls,
	}
	reg := &testutil.FakeProjectRegistry{
		ProjectsFunc: func() ([]string, error) { return []string{repoA, repoB}, nil },
	}
	svc := New(reg, store, nil, managed)

	projects := snapshotProjectsFromMsg(t, svc.LoadProjects(2)(), 2)
	assertSnapshotFixtureRows(t, projects)

	if got := listAllCalls.Load(); got != 1 {
		t.Fatalf("ListAll calls = %d, want 1 attempted snapshot", got)
	}
	if got := byRepoCalls.Load(); got != 4 {
		t.Fatalf("per-repo list calls = %d, want 4 (live + shelf per project)", got)
	}
}

func TestLoadProjectsShelfRowsNilStore(t *testing.T) {
	repoA := testutil.InitRepo(t)
	managed := t.TempDir()
	reg := &testutil.FakeProjectRegistry{
		ProjectsFunc: func() ([]string, error) { return []string{repoA}, nil },
	}
	svc := New(reg, nil, nil, managed)

	projects := snapshotProjectsFromMsg(t, svc.LoadProjects(2)(), 1)
	project := projects[0]
	if len(project.Workspaces) != 1 || !project.Workspaces[0].IsPrimaryCheckout() {
		t.Fatalf("workspaces = %v, want only the transient primary", project.Workspaces)
	}
	if len(project.ShelvedWorkspaces) != 0 {
		t.Fatalf("shelves = %v, want none with nil store", project.ShelvedWorkspaces)
	}
}
