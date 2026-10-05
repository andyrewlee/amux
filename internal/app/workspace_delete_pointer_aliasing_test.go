package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/testutil/tmuxops"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// aliasingTestProject builds a project whose Workspaces backing array models
// the shared slices dashboard rows and long-running commands point into.
func aliasingTestProject() *data.Project {
	project := data.NewProject("/tmp/repo")
	project.Workspaces = []data.Workspace{
		*data.NewWorkspace("a", "a", "main", "/tmp/repo", "/tmp/workspaces/repo/a"),
		*data.NewWorkspace("b", "b", "main", "/tmp/repo", "/tmp/workspaces/repo/b"),
		*data.NewWorkspace("c", "c", "main", "/tmp/repo", "/tmp/workspaces/repo/c"),
	}
	return project
}

// TestRemoveWorkspaceFromLoadedProjects_DoesNotShiftOutstandingPointers proves
// removing a workspace never compacts the slice in place. Dashboard rows and an
// async delete command hold &Workspaces[j]; an in-place shift would re-point
// those pointers at a different workspace, which made deleting one workspace
// tear down another workspace's tabs and agents.
func TestRemoveWorkspaceFromLoadedProjects_DoesNotShiftOutstandingPointers(t *testing.T) {
	app := &App{projects: []data.Project{*aliasingTestProject()}}
	outstandingB := &app.projects[0].Workspaces[1]
	outstandingC := &app.projects[0].Workspaces[2]

	app.removeWorkspaceFromLoadedProjects(&app.projects[0].Workspaces[0], nil)

	if outstandingB.Root != "/tmp/workspaces/repo/b" || outstandingB.Branch != "b" {
		t.Fatalf("pointer to workspace b drifted to %s (%s): in-place compaction re-pointed it at another workspace", outstandingB.Root, outstandingB.Branch)
	}
	if outstandingC.Root != "/tmp/workspaces/repo/c" || outstandingC.Branch != "c" {
		t.Fatalf("pointer to workspace c drifted to %s (%s): in-place compaction re-pointed it at another workspace", outstandingC.Root, outstandingC.Branch)
	}
	remaining := app.projects[0].Workspaces
	if len(remaining) != 2 || remaining[0].Root != "/tmp/workspaces/repo/b" || remaining[1].Root != "/tmp/workspaces/repo/c" {
		t.Fatalf("unexpected remaining workspaces: %+v", remaining)
	}
}

// TestFilterDeletedWorkspacesFromProjectLoad_DoesNotMutateBackingArray pins the
// same no-in-place-mutation rule on the projects-load filter path.
func TestFilterDeletedWorkspacesFromProjectLoad_DoesNotMutateBackingArray(t *testing.T) {
	app := &App{}
	project := aliasingTestProject()
	outstandingB := &project.Workspaces[1]

	wsA := &project.Workspaces[0]
	if !app.lifecycle.markMutatingWorkspace(string(wsA.ID()), wsA.Root, true) {
		t.Fatal("failed to mark workspace a as mutating")
	}

	filtered := app.filterDeletedWorkspacesFromProjectLoad([]data.Project{*project}, 0)

	if outstandingB.Root != "/tmp/workspaces/repo/b" || outstandingB.Branch != "b" {
		t.Fatalf("pointer to workspace b drifted to %s (%s): load filtering mutated the shared backing array", outstandingB.Root, outstandingB.Branch)
	}
	got := filtered[0].Workspaces
	if len(got) != 2 || got[0].Root != "/tmp/workspaces/repo/b" || got[1].Root != "/tmp/workspaces/repo/c" {
		t.Fatalf("unexpected filtered workspaces: %+v", got)
	}
}

// TestRemoveWorkspaceFromLoadedProjects_StampedSetCatchesLegacyAlias proves a
// loaded project row keyed under a legacy identity is removed when the
// delete message's stamped ID set contains that key — even though neither the
// candidate's live ID nor its root matches the deleted workspace's.
func TestRemoveWorkspaceFromLoadedProjects_StampedSetCatchesLegacyAlias(t *testing.T) {
	// The stale loaded row carries the legacy record key as its ID(); the
	// delete message stamps that key in WorkspaceIDs (the pre-removal
	// identity set) — matching against the set removes the row even though
	// the deleted workspace's own ID and root no longer coincide with it.
	storeRoot := filepath.Join(t.TempDir(), "meta")
	store := data.NewWorkspaceStore(storeRoot)
	repoDir := filepath.Join(t.TempDir(), "repo")
	// The loaded row carries the unresolved /var spelling recorded when its
	// era ran; the deleted workspace's stamped set was minted over the
	// resolved /private/var spelling — same path, same ComputedID, distinct
	// Root strings and store keys.
	unresolvedRoot := filepath.Join(t.TempDir(), "drifted", "legacy")
	if err := os.MkdirAll(unresolvedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	resolvedRoot := data.NormalizePath(unresolvedRoot)
	if resolvedRoot == unresolvedRoot {
		t.Skip("host has no symlinked tempdir prefix — cannot stage the drift")
	}
	legacyID := data.WorkspaceID("legacy-key")

	candidate := data.NewWorkspace("ghost", "b", "main", repoDir, unresolvedRoot)
	if err := store.Save(candidate); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(
		filepath.Join(storeRoot, string(candidate.ID())),
		filepath.Join(storeRoot, string(legacyID)),
	); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(legacyID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID() != legacyID {
		t.Fatalf("fixture: loaded ID = %s, want legacy key %s", loaded.ID(), legacyID)
	}

	deleted := data.NewWorkspace("gone", "b", "main", repoDir, resolvedRoot)
	if deleted.ID() == legacyID || deleted.Root == loaded.Root {
		t.Fatal("fixture: deleted workspace must differ from the stale row in both ID and root")
	}
	if deleted.ComputedID() != loaded.ComputedID() {
		t.Fatal("fixture: resolved and unresolved spellings must share the computed identity")
	}

	project := data.NewProject(repoDir)
	project.Workspaces = []data.Workspace{*loaded}
	app := &App{projects: []data.Project{*project}}

	app.removeWorkspaceFromLoadedProjects(deleted, workspacesvc.WorkspaceIDStrings(deleted))

	if len(app.projects[0].Workspaces) != 0 {
		t.Fatalf("stamped-alias row survived removal: %+v", app.projects[0].Workspaces)
	}
}

// delete pipeline reads a frozen snapshot: even if the element the caller
// handed over is overwritten mid-delete (the aliasing drift observed in the
// logs, where a delete of "cleanup" ended up running git branch -D pickup), the
// async cmd and the resulting WorkspaceDeleted keep the original identity.
func TestHandleDeleteWorkspace_FreezesIdentityAgainstAliasedMutation(t *testing.T) {
	project := aliasingTestProject()
	handedOver := &project.Workspaces[0]
	victimID := string(handedOver.ID())
	victimRoot := handedOver.Root

	svc := workspacesvc.New(nil, nil, nil, "/tmp/workspaces")
	svc.Configure(workspacesvc.Deps{GitOps: &testutil.FakeGitOps{}})
	app := &App{
		dashboard:        dashboard.New(),
		center:           center.New(nil),
		sidebar:          sidebar.NewTabbedSidebar(),
		sidebarTerminal:  sidebar.NewTerminalModel(),
		tmuxService:      &tmuxops.FakeTmuxOps{},
		tmuxOptions:      tmux.Options{},
		workspaceService: svc,
	}

	cmds := app.handleDeleteWorkspace(messages.DeleteWorkspace{Project: project, Workspace: handedOver})

	project.Workspaces[0] = *data.NewWorkspace("c", "c", "main", "/tmp/repo", "/tmp/workspaces/repo/c")

	var deleted *messages.WorkspaceDeleted
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		if msg := cmd(); msg != nil {
			if d, ok := msg.(messages.WorkspaceDeleted); ok {
				deleted = &d
			}
		}
	}
	if deleted == nil {
		t.Fatal("expected a messages.WorkspaceDeleted from the delete cmd")
	}
	if string(deleted.Workspace.ID()) != victimID || deleted.Workspace.Root != victimRoot {
		t.Fatalf("delete result drifted: got id=%s root=%s, want id=%s root=%s",
			deleted.Workspace.ID(), deleted.Workspace.Root, victimID, victimRoot)
	}
}
