package workspacesvc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
)

// renameFixture wires a service whose registry, store, and git ops are
// fakes, plus a project with on-disk worktree directories under the old
// container. Tests drive svc.RenameProject and assert the message, the
// recorded moves, and the rebound records.
type renameFixture struct {
	svc      *Service
	registry *testutil.FakeProjectRegistry
	store    *testutil.FakeWorkspaceStore
	gitOps   *testutil.FakeGitOps

	root    string // workspaces root
	repo    string // project repo path
	project *data.Project
	moves   *[]moveCall
}

type moveCall struct {
	from, to string
}

func newRenameFixture(t *testing.T) *renameFixture {
	t.Helper()
	root := t.TempDir()
	repo := testutil.InitRepo(t)
	project := data.NewProject(repo)

	registry := &testutil.FakeProjectRegistry{
		EntriesFunc: func() ([]data.RegistryEntry, error) {
			return []data.RegistryEntry{{Name: project.Name, Path: repo}}, nil
		},
	}
	store := &testutil.FakeWorkspaceStore{}
	// The rename migration reads the repo's records through the store's
	// per-repo listing (the migration's record index), so give the fake the
	// real store's shape: every saved record for the repo, archived ones
	// included.
	store.ListByRepoIncludingArchivedFunc = func(repo string) ([]*data.Workspace, error) {
		var out []*data.Workspace
		for _, ws := range store.SavedWorkspaces() {
			if data.SamePath(ws.Repo, repo) {
				cp := *ws
				out = append(out, &cp)
			}
		}
		return out, nil
	}
	var moves []moveCall
	gitOps := &testutil.FakeGitOps{
		MoveWorktreeFunc: func(_, from, to string) error {
			// Simulate the directory relocation so the retry/idempotency
			// branches see real filesystem state.
			if err := os.Rename(from, to); err != nil {
				return err
			}
			moves = append(moves, moveCall{from: from, to: to})
			return nil
		},
	}
	svc := New(registry, store, nil, root)
	svc.gitOps = gitOps

	return &renameFixture{
		svc:      svc,
		registry: registry,
		store:    store,
		gitOps:   gitOps,
		root:     root,
		repo:     repo,
		project:  project,
		moves:    &moves,
	}
}

// addWorktree creates a worktree directory under the project container and
// persists its record through the fake store.
func (f *renameFixture) addWorktree(t *testing.T, name string) *data.Workspace {
	t.Helper()
	wsRoot := filepath.Join(f.root, f.project.Name, name)
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	ws := data.NewWorkspace(name, name, "main", f.repo, wsRoot)
	if err := f.store.Save(ws); err != nil {
		t.Fatalf("save record: %v", err)
	}
	return ws
}

// discoverFunc makes discovery report exactly the fixture's workspaces.
func (f *renameFixture) discoverFunc(workspaces ...*data.Workspace) {
	list := make([]data.Workspace, 0, len(workspaces))
	for _, ws := range workspaces {
		list = append(list, *ws)
	}
	f.gitOps.DiscoverWorkspacesFunc = func(*data.Project) ([]data.Workspace, error) {
		return list, nil
	}
}

// savedRoots returns every root the store has saved a record with.
func (f *renameFixture) savedRoots() map[string]bool {
	roots := make(map[string]bool)
	for _, ws := range f.store.SavedWorkspaces() {
		roots[ws.Root] = true
	}
	return roots
}

func runRenameCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	return cmd()
}

// asProjectRenamed asserts the rename result message type once so each test
// reads as an outcome assertion, not a type puzzle (errcheck's
// check-type-assertions requires the checked two-value form).
func asProjectRenamed(t *testing.T, msg tea.Msg) messages.ProjectRenamed {
	t.Helper()
	renamed, ok := msg.(messages.ProjectRenamed)
	if !ok {
		t.Fatalf("expected messages.ProjectRenamed, got %T", msg)
	}
	return renamed
}

func TestRenameProjectMovesWorktreesAndRebindsRecords(t *testing.T) {
	f := newRenameFixture(t)
	wsA := f.addWorktree(t, "feature-a")
	wsB := f.addWorktree(t, "feature-b")
	f.discoverFunc(wsA, wsB)

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "checkfu-old"))

	renamed, ok := msg.(messages.ProjectRenamed)
	if !ok {
		t.Fatalf("expected messages.ProjectRenamed, got %T", msg)
	}
	if renamed.Err != nil || renamed.Failed != 0 || renamed.Skipped != 0 || renamed.Moved != 2 {
		t.Fatalf("unexpected outcome: %+v", renamed)
	}
	if renamed.OldName != f.project.Name || renamed.NewName != "checkfu-old" {
		t.Fatalf("unexpected names: %+v", renamed)
	}
	for _, m := range *f.moves {
		if filepath.Dir(m.to) != filepath.Join(f.root, "checkfu-old") {
			t.Fatalf("move destination %q not in the new container", m.to)
		}
	}
	if _, err := os.Stat(filepath.Join(f.root, "checkfu-old", "feature-a")); err != nil {
		t.Fatalf("feature-a not moved: %v", err)
	}
	roots := f.savedRoots()
	if !roots[filepath.Join(f.root, "checkfu-old", "feature-a")] ||
		!roots[filepath.Join(f.root, "checkfu-old", "feature-b")] {
		t.Fatalf("records not rebound into the new container: %v", roots)
	}
}

func TestRenameProjectSkipsWorktreesWithRunningScripts(t *testing.T) {
	f := newRenameFixture(t)
	wsA := f.addWorktree(t, "busy")
	wsB := f.addWorktree(t, "idle")
	f.discoverFunc(wsA, wsB)
	f.svc.scriptsRunning = func(ws *data.Workspace) bool {
		return ws.Root == wsA.Root
	}

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "renamed"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Moved != 1 || renamed.Skipped != 1 || renamed.Failed != 0 || renamed.Err != nil {
		t.Fatalf("unexpected outcome: %+v", renamed)
	}
	if _, err := os.Stat(wsA.Root); err != nil {
		t.Fatalf("busy worktree must stay in place: %v", err)
	}
	roots := f.savedRoots()
	if roots[filepath.Join(f.root, "renamed", "busy")] {
		t.Fatalf("busy worktree's record must not be rebound")
	}
}

func TestRenameProjectRollsBackMoveWhenRebindFails(t *testing.T) {
	f := newRenameFixture(t)
	wsA := f.addWorktree(t, "feature-a")
	f.discoverFunc(wsA)
	f.store.RebindRootFunc = func(id data.WorkspaceID, newRoot string) error {
		return errors.New("rebind refused")
	}

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "checkfu-old"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Failed != 1 || renamed.Moved != 0 {
		t.Fatalf("unexpected outcome: %+v", renamed)
	}
	if renamed.Err == nil {
		t.Fatalf("expected the rebind failure to be reported")
	}
	// The move must have rolled back: the worktree is where it started.
	if _, err := os.Stat(wsA.Root); err != nil {
		t.Fatalf("rollback left the worktree missing: %v", err)
	}
	if len(*f.moves) != 2 {
		t.Fatalf("expected move + rollback, got %d moves", len(*f.moves))
	}
}

func TestRenameProjectRefusesConflictingDestination(t *testing.T) {
	f := newRenameFixture(t)
	wsA := f.addWorktree(t, "feature-a")
	f.discoverFunc(wsA)
	// Something already occupies the destination while the source is still
	// present — refusing beats silently clobbering either side.
	if err := os.MkdirAll(filepath.Join(f.root, "checkfu-old", "feature-a"), 0o755); err != nil {
		t.Fatalf("mkdir conflict: %v", err)
	}

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "checkfu-old"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Failed != 1 || renamed.Moved != 0 {
		t.Fatalf("unexpected outcome: %+v", renamed)
	}
	if _, err := os.Stat(wsA.Root); err != nil {
		t.Fatalf("source must stay in place on conflict: %v", err)
	}
}

func TestRenameProjectLeavesFlatLayoutWorktreesAlone(t *testing.T) {
	f := newRenameFixture(t)
	// A flat worktree directly under the workspaces root (legacy layout).
	flatRoot := filepath.Join(f.root, "flat-ws")
	if err := os.MkdirAll(flatRoot, 0o755); err != nil {
		t.Fatalf("mkdir flat worktree: %v", err)
	}
	flat := data.NewWorkspace("flat-ws", "flat-ws", "main", f.repo, flatRoot)
	f.discoverFunc(flat)

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "checkfu-old"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Moved != 0 || renamed.Failed != 0 {
		t.Fatalf("flat worktree must not move: %+v", renamed)
	}
	if _, err := os.Stat(flatRoot); err != nil {
		t.Fatalf("flat worktree must stay in place: %v", err)
	}
}

func TestRenameProjectToNameWhoseContainerHoldsTheWorktrees(t *testing.T) {
	f := newRenameFixture(t)
	// Second half of a basename collision: the stored name was minted
	// basename-2 when another repo held the bare basename, but the
	// worktrees never left the basename container — the one the bare name
	// maps to again once the other repo vacates it. Renaming back must be
	// metadata-only: no move, nothing failed, the record stays put.
	base := f.project.Name // the repo's basename
	minted := data.NewProjectNamed(base+"-2", f.repo)
	wsRoot := filepath.Join(f.root, base, "feature-a")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	ws := data.NewWorkspace("feature-a", "feature-a", "main", f.repo, wsRoot)
	if err := f.store.Save(ws); err != nil {
		t.Fatalf("save record: %v", err)
	}
	f.discoverFunc(ws)

	msg := runRenameCmd(t, f.svc.RenameProject(minted, base))
	renamed := asProjectRenamed(t, msg)
	if renamed.Moved != 0 || renamed.Failed != 0 || renamed.Skipped != 0 || renamed.Err != nil {
		t.Fatalf("renaming onto the container the worktrees already live in must be a no-op: %+v", renamed)
	}
	if len(*f.moves) != 0 {
		t.Fatalf("no git move may be attempted when source is destination: %v", *f.moves)
	}
	if _, err := os.Stat(wsRoot); err != nil {
		t.Fatalf("worktree must stay exactly where it was: %v", err)
	}
	roots := f.savedRoots()
	if !roots[wsRoot] {
		t.Fatalf("record root must be untouched: %v", roots)
	}
}

func TestRenameProjectRegistryFailureSurfacesError(t *testing.T) {
	f := newRenameFixture(t)
	f.registry.RenameProjectFunc = func(path, newName string) error {
		return errors.New("name taken")
	}

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "checkfu-old"))
	errMsg, ok := msg.(messages.Error)
	if !ok {
		t.Fatalf("expected messages.Error, got %T", msg)
	}
	if errMsg.Err == nil || errMsg.Err.Error() != "name taken" {
		t.Fatalf("unexpected error: %v", errMsg.Err)
	}
	if len(*f.moves) != 0 {
		t.Fatalf("no worktree may move when the registry rename fails")
	}
}

func TestRenameProjectRebindsShelvedRecords(t *testing.T) {
	f := newRenameFixture(t)
	shelfRoot := filepath.Join(f.root, f.project.Name, "shelved-ws")
	shelved := data.NewWorkspace("shelved-ws", "shelved-ws", "main", f.repo, shelfRoot)
	shelved.Shelved = true
	shelved.Archived = true
	if err := f.store.Save(shelved); err != nil {
		t.Fatalf("save record: %v", err)
	}
	f.discoverFunc()
	// The store's fake must serve shelved workspaces through the repo filter
	// the service consults.
	f.store.ListByRepoIncludingArchivedFunc = func(repo string) ([]*data.Workspace, error) {
		return []*data.Workspace{shelved}, nil
	}

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "checkfu-old"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Failed != 0 || renamed.Err != nil {
		t.Fatalf("unexpected outcome: %+v", renamed)
	}
	roots := f.savedRoots()
	if !roots[filepath.Join(f.root, "checkfu-old", "shelved-ws")] {
		t.Fatalf("shelved record root not rebound: %v", roots)
	}
}

func TestRenameProjectDoesNotMoveForeignRepoWorktrees(t *testing.T) {
	f := newRenameFixture(t)
	// A worktree of a DIFFERENT repo sitting in this project's container
	// (the shared-basename collision this feature fixes) must never move.
	otherRepo := testutil.InitRepo(t)
	foreignRoot := filepath.Join(f.root, f.project.Name, "foreign")
	if err := os.MkdirAll(foreignRoot, 0o755); err != nil {
		t.Fatalf("mkdir foreign: %v", err)
	}
	foreign := data.NewWorkspace("foreign", "foreign", "main", otherRepo, foreignRoot)
	f.discoverFunc(foreign)

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "checkfu-old"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Moved != 0 || renamed.Failed != 0 {
		t.Fatalf("foreign worktree must not move: %+v", renamed)
	}
	if _, err := os.Stat(foreignRoot); err != nil {
		t.Fatalf("foreign worktree must stay in place: %v", err)
	}
}
