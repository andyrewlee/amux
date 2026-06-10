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
