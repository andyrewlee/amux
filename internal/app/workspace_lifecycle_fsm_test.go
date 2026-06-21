package app

import (
	"sync"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestLifecyclePhaseTransitionTable(t *testing.T) {
	cases := []struct {
		name string
		from lifecyclePhase
		to   lifecyclePhase
		want bool
	}{
		{"active->creating", lifecycleActive, lifecycleCreating, true},
		{"active->deleting", lifecycleActive, lifecycleDeleting, true},
		{"creating->active", lifecycleCreating, lifecycleActive, true},
		{"deleting->active", lifecycleDeleting, lifecycleActive, true},
		{"creating->creating", lifecycleCreating, lifecycleCreating, true},
		{"deleting->deleting", lifecycleDeleting, lifecycleDeleting, true},
		{"creating->deleting rejected", lifecycleCreating, lifecycleDeleting, false},
		{"deleting->creating rejected", lifecycleDeleting, lifecycleCreating, false},
	}
	for _, tc := range cases {
		if got := lifecycleTransitionAllowed(tc.from, tc.to); got != tc.want {
			t.Errorf("%s: lifecycleTransitionAllowed(%s, %s) = %v, want %v", tc.name, tc.from, tc.to, got, tc.want)
		}
	}
}

func TestLifecycleRejectsCreateWhileDeleting(t *testing.T) {
	st := newWorkspaceLifecycleState()
	st.markDeleting("ws-1", true)

	if st.markCreating("ws-1") {
		t.Fatal("expected markCreating to be rejected while delete is in flight")
	}
	if !st.isDeleting("ws-1") {
		t.Fatal("expected deleting phase preserved after rejected create")
	}
	// clearCreating must not stomp the deleting phase either.
	st.clearCreating("ws-1")
	if !st.isDeleting("ws-1") {
		t.Fatal("expected deleting phase preserved after clearCreating")
	}

	st.markDeleting("ws-1", false)
	if st.phase("ws-1") != lifecycleActive {
		t.Fatalf("expected workspace settled back to active, got %s", st.phase("ws-1"))
	}
	if !st.markCreating("ws-1") {
		t.Fatal("expected markCreating accepted once the delete settled")
	}
}

func TestLifecycleClearsDeletingByWorkspaceRoot(t *testing.T) {
	st := newWorkspaceLifecycleState()
	root := "/repo/.amux/workspaces/feature"

	if !st.markDeletingWorkspace("pre-delete-id", root, true) {
		t.Fatal("expected delete marker to be accepted")
	}
	if !st.isDeletingWorkspace("different-id", root) {
		t.Fatal("expected root identity to report delete in flight")
	}
	if !st.markDeletingWorkspace("post-delete-id", root, false) {
		t.Fatal("expected delete marker clear to be accepted")
	}
	if st.isDeleting("pre-delete-id") {
		t.Fatal("expected original delete phase to be cleared by root identity")
	}
	if st.isDeletingWorkspace("post-delete-id", root) {
		t.Fatal("expected root identity to be settled after clear")
	}
}

// TestLifecycleCreateWhileProjectsLoading proves the message interleaving that
// motivated the creating phase: a workspace marked create-in-flight stays in
// that phase across a ProjectsLoaded that does not yet contain it, and only
// settles when WorkspaceCreated lands.
func TestLifecycleCreateWhileProjectsLoading(t *testing.T) {
	app := &App{
		lifecycle:        newWorkspaceLifecycleState(),
		tmuxActivity:     newTmuxActivityState(),
		dashboard:        dashboard.New(),
		center:           center.New(nil),
		workspaceService: newWorkspaceService(nil, nil, nil, t.TempDir()),
	}

	project := data.NewProject("/repo")
	app.handleCreateWorkspace(messages.CreateWorkspace{
		Project:   project,
		Name:      "feature",
		Base:      "main",
		Assistant: "claude",
	})
	creating := app.lifecycle.snapshotCreating()
	if len(creating) != 1 {
		t.Fatalf("expected one create-in-flight workspace, got %v", creating)
	}
	var wsID string
	for id := range creating {
		wsID = id
	}

	// A projects reload that does not include the half-created workspace must
	// not clear the creating phase (the workspace is not loaded yet).
	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*project}})
	if !app.lifecycle.isCreating(wsID) {
		t.Fatal("expected creating phase to survive a projects reload")
	}

	pending := app.workspaceService.pendingWorkspace(project, "feature", "main")
	app.handleWorkspaceCreated(messages.WorkspaceCreated{Workspace: pending})
	if app.lifecycle.phase(wsID) != lifecycleActive {
		t.Fatalf("expected workspace settled after WorkspaceCreated, got %s", app.lifecycle.phase(wsID))
	}
}

func TestHandleCreateWorkspaceStopsWhenLifecycleRejectsCreate(t *testing.T) {
	workspacesRoot := t.TempDir()
	project := data.NewProject("/repo")
	service := newWorkspaceService(nil, nil, nil, workspacesRoot)
	pending := service.pendingWorkspace(project, "feature", "main")
	if pending == nil {
		t.Fatal("expected pending workspace")
	}

	app := &App{
		lifecycle:        newWorkspaceLifecycleState(),
		dashboard:        dashboard.New(),
		workspaceService: service,
	}
	app.lifecycle.markDeleting(string(pending.ID()), true)

	cmds := app.handleCreateWorkspace(messages.CreateWorkspace{
		Project:   project,
		Name:      "feature",
		Base:      "main",
		Assistant: "claude",
	})
	if len(cmds) != 1 {
		t.Fatalf("expected only the create-failed command, got %d commands", len(cmds))
	}
	msg := cmds[0]()
	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Workspace == nil || failed.Workspace.ID() != pending.ID() {
		t.Fatalf("expected failed pending workspace %s, got %#v", pending.ID(), failed.Workspace)
	}
	if failed.Err == nil {
		t.Fatal("expected lifecycle rejection error")
	}
	if !app.lifecycle.isDeleting(string(pending.ID())) {
		t.Fatal("expected deleting phase to remain active after rejected create")
	}
	if app.lifecycle.isCreating(string(pending.ID())) {
		t.Fatal("expected rejected create not to mark workspace creating")
	}
}

func TestHandleDeleteWorkspaceStopsWhenLifecycleRejectsDelete(t *testing.T) {
	project := data.NewProject("/repo")
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	app := &App{
		lifecycle: newWorkspaceLifecycleState(),
		dashboard: dashboard.New(),
	}
	if !app.lifecycle.markCreating(string(ws.ID())) {
		t.Fatal("expected setup create phase accepted")
	}

	cmds := app.handleDeleteWorkspace(messages.DeleteWorkspace{Project: project, Workspace: ws})
	if len(cmds) != 0 {
		t.Fatalf("expected rejected delete to queue no commands, got %d", len(cmds))
	}
	if !app.lifecycle.isCreating(string(ws.ID())) {
		t.Fatal("expected creating phase to remain active after rejected delete")
	}
	if app.lifecycle.isDeleting(string(ws.ID())) {
		t.Fatal("expected rejected delete not to mark workspace deleting")
	}
}

// TestLifecycleDeleteWhilePersisting exercises the deleting phase against the
// persistence paths concurrently (the guard methods are read from Cmd/worker
// goroutines while Update-handler transitions run); run with -race.
func TestLifecycleDeleteWhilePersisting(t *testing.T) {
	st := newWorkspaceLifecycleState()
	const wsID = "ws-race"
	st.markDirty(wsID)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 200; j++ {
				st.markDeleting(wsID, j%2 == 0)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 200; j++ {
				_ = st.isDeleting(wsID)
				_ = st.snapshotDeleting()
				st.runUnlessDeleting(wsID, func() {})
			}
		}()
	}
	close(start)
	wg.Wait()

	// The dirty marker is orthogonal to the lifecycle phase and must survive
	// the delete churn so a failed delete can requeue persistence.
	if !st.dirty[wsID] {
		t.Fatal("expected dirty marker to survive delete-phase churn")
	}
}
