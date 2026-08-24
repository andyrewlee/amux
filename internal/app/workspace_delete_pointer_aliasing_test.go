package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
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

	app.removeWorkspaceFromLoadedProjects(&app.projects[0].Workspaces[0])

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
	if !app.lifecycle.markDeletingWorkspace(string(wsA.ID()), wsA.Root, true) {
		t.Fatal("failed to mark workspace a as deleting")
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

// TestHandleDeleteWorkspace_FreezeIdentityAgainstAliasedMutation proves the
// delete pipeline reads a frozen snapshot: even if the element the caller
// handed over is overwritten mid-delete (the aliasing drift observed in the
// logs, where a delete of "cleanup" ended up running git branch -D pickup), the
// async cmd and the resulting WorkspaceDeleted keep the original identity.
func TestHandleDeleteWorkspace_FreezesIdentityAgainstAliasedMutation(t *testing.T) {
	project := aliasingTestProject()
	handedOver := &project.Workspaces[0]
	victimID := string(handedOver.ID())
	victimRoot := handedOver.Root

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	svc.gitOps = &mockGitOps{}
	app := &App{
		dashboard:        dashboard.New(),
		center:           center.New(nil),
		sidebar:          sidebar.NewTabbedSidebar(),
		sidebarTerminal:  sidebar.NewTerminalModel(),
		tmuxService:      &killRecordingTmuxOps{},
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
