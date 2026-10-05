package app

import (
	"sync"
	"testing"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
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
		{"active->mutating", lifecycleActive, lifecycleMutating, true},
		{"creating->active", lifecycleCreating, lifecycleActive, true},
		{"mutating->active", lifecycleMutating, lifecycleActive, true},
		{"creating->creating", lifecycleCreating, lifecycleCreating, true},
		{"mutating->mutating", lifecycleMutating, lifecycleMutating, true},
		{"creating->mutating rejected", lifecycleCreating, lifecycleMutating, false},
		{"mutating->creating rejected", lifecycleMutating, lifecycleCreating, false},
	}
	for _, tc := range cases {
		if got := lifecycleTransitionAllowed(tc.from, tc.to); got != tc.want {
			t.Errorf("%s: lifecycleTransitionAllowed(%s, %s) = %v, want %v", tc.name, tc.from, tc.to, got, tc.want)
		}
	}
}

func TestLifecycleRejectsCreateWhileMutating(t *testing.T) {
	st := newWorkspaceLifecycleState()
	st.markMutating("ws-1", true)

	if st.markCreating("ws-1") {
		t.Fatal("expected markCreating to be rejected while delete is in flight")
	}
	if !st.isMutating("ws-1") {
		t.Fatal("expected deleting phase preserved after rejected create")
	}
	// clearCreating must not stomp the mutating phase either.
	st.clearCreating("ws-1")
	if !st.isMutating("ws-1") {
		t.Fatal("expected deleting phase preserved after clearCreating")
	}

	st.markMutating("ws-1", false)
	if st.phase("ws-1") != lifecycleActive {
		t.Fatalf("expected workspace settled back to active, got %s", st.phase("ws-1"))
	}
	if !st.markCreating("ws-1") {
		t.Fatal("expected markCreating accepted once the delete settled")
	}
}

func TestLifecycleClearsMutatingByWorkspaceRoot(t *testing.T) {
	st := newWorkspaceLifecycleState()
	root := "/repo/.amux/workspaces/feature"

	if !st.markMutatingWorkspace("pre-delete-id", root, true) {
		t.Fatal("expected delete marker to be accepted")
	}
	if !st.isMutatingWorkspace("different-id", root) {
		t.Fatal("expected root identity to report delete in flight")
	}
	if !st.markMutatingWorkspace("post-delete-id", root, false) {
		t.Fatal("expected delete marker clear to be accepted")
	}
	if st.isMutating("pre-delete-id") {
		t.Fatal("expected original delete phase to be cleared by root identity")
	}
	if st.isMutatingWorkspace("post-delete-id", root) {
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
		workspaceService: workspacesvc.New(nil, nil, nil, t.TempDir()),
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
	if app.lifecycle.phase(wsID) != lifecycleCreating {
		t.Fatal("expected creating phase to survive a projects reload")
	}

	pending := app.workspaceService.PendingWorkspace(project, "feature", "main")
	app.handleWorkspaceCreated(messages.WorkspaceCreated{Workspace: pending})
	if app.lifecycle.phase(wsID) != lifecycleActive {
		t.Fatalf("expected workspace settled after WorkspaceCreated, got %s", app.lifecycle.phase(wsID))
	}
}

func TestCreatedWorkspaceSurvivesOlderProjectLoadUntilConfirmed(t *testing.T) {
	repo := "/repo"
	primary := data.NewWorkspace("repo", "main", "", repo, repo)
	created := data.NewWorkspace("feature", "feature", "main", repo, "/workspaces/feature")
	project := data.NewProject(repo)
	project.Workspaces = []data.Workspace{*primary, *created}

	app := &App{
		lifecycle:        newWorkspaceLifecycleState(),
		tmuxActivity:     newTmuxActivityState(),
		dashboard:        dashboard.New(),
		center:           center.New(nil),
		workspaceService: workspacesvc.New(nil, nil, nil, t.TempDir()),
		projects:         []data.Project{*project},
		activeProject:    project,
		activeWorkspace:  created,
	}
	app.center.SetWorkspace(created)
	app.lifecycle.projectsLoadToken = 2
	if !app.lifecycle.markCreating(string(created.ID())) {
		t.Fatal("expected create phase to be accepted")
	}

	app.handleWorkspaceCreated(messages.WorkspaceCreated{Workspace: created})
	if got := app.lifecycle.projectsLoadToken; got != 3 {
		t.Fatalf("post-create load token = %d, want 3", got)
	}

	older := data.NewProject(repo)
	older.Workspaces = []data.Workspace{*primary}
	app.handleProjectsLoaded(messages.ProjectsLoaded{
		Projects:  []data.Project{*older},
		LoadToken: 2,
	})
	if app.activeWorkspace == nil || app.activeWorkspace.ID() != created.ID() {
		t.Fatal("older project snapshot cleared the newly created active workspace")
	}
	if app.showWelcome {
		t.Fatal("older project snapshot returned the UI home")
	}

	confirmed := data.NewProject(repo)
	confirmed.Workspaces = []data.Workspace{*primary, *created}
	app.handleProjectsLoaded(messages.ProjectsLoaded{
		Projects:  []data.Project{*confirmed},
		LoadToken: 3,
	})
	if app.activeWorkspace == nil || app.activeWorkspace.ID() != created.ID() {
		t.Fatal("post-create project snapshot did not retain the active workspace")
	}
	if len(app.lifecycle.createdUntilProjectsLoadToken) != 0 {
		t.Fatalf("post-create barriers were not released: %v", app.lifecycle.createdUntilProjectsLoadToken)
	}

	// Once the confirming load lands, a later real removal must still go home.
	app.handleProjectsLoaded(messages.ProjectsLoaded{
		Projects:  []data.Project{*older},
		LoadToken: 4,
	})
	if app.activeWorkspace != nil || !app.showWelcome {
		t.Fatal("later confirmed removal did not clear the active workspace")
	}
}

func TestHandleCreateWorkspaceStopsWhenLifecycleRejectsCreate(t *testing.T) {
	workspacesRoot := t.TempDir()
	project := data.NewProject("/repo")
	service := workspacesvc.New(nil, nil, nil, workspacesRoot)
	pending := service.PendingWorkspace(project, "feature", "main")
	if pending == nil {
		t.Fatal("expected pending workspace")
	}

	app := &App{
		lifecycle:        newWorkspaceLifecycleState(),
		dashboard:        dashboard.New(),
		workspaceService: service,
	}
	app.lifecycle.markMutating(string(pending.ID()), true)

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
	if !app.lifecycle.isMutating(string(pending.ID())) {
		t.Fatal("expected deleting phase to remain active after rejected create")
	}
	if app.lifecycle.phase(string(pending.ID())) == lifecycleCreating {
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
	if app.lifecycle.phase(string(ws.ID())) != lifecycleCreating {
		t.Fatal("expected creating phase to remain active after rejected delete")
	}
	if app.lifecycle.isMutating(string(ws.ID())) {
		t.Fatal("expected rejected delete not to mark workspace deleting")
	}
}

// TestLifecycleMutationWhilePersisting exercises the mutating phase against the
// persistence paths concurrently (the guard methods are read from Cmd/worker
// goroutines while Update-handler transitions run); run with -race.
func TestLifecycleMutationWhilePersisting(t *testing.T) {
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
				st.markMutating(wsID, j%2 == 0)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 200; j++ {
				_ = st.isMutating(wsID)
				_ = st.snapshotMutating()
				st.runUnlessMutating(wsID, func() {})
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

// TestMarkCreatingWorkspaceRejectsSameIdentityReentry pins the single-owner
// rule: a second create admitted under the same identity would run a
// concurrent worktree add at the same path, and either result could tear
// down the other's guard.
func TestMarkCreatingWorkspaceRejectsSameIdentityReentry(t *testing.T) {
	st := newWorkspaceLifecycleState()
	root := "/repo/.amux/workspaces/feature"

	if !st.markCreatingWorkspace("pre-create-id", root) {
		t.Fatal("expected first create admission")
	}
	if st.markCreatingWorkspace("pre-create-id", root) {
		t.Fatal("expected same-identity re-entry refused")
	}
	if st.phase("pre-create-id") != lifecycleCreating {
		t.Fatal("refused re-entry disturbed the owning create")
	}
	if got := st.creatingRootID[root]; got != "pre-create-id" {
		t.Fatalf("bridge rebound on refused re-entry: %q", got)
	}
}

// TestMarkCreatingWorkspaceRejectsSecondOwnerAtRoot covers the different-ID
// edge: a create marked under a different identity at a claimed root would
// overwrite the bridge and orphan the owning operation's identity.
func TestMarkCreatingWorkspaceRejectsSecondOwnerAtRoot(t *testing.T) {
	st := newWorkspaceLifecycleState()
	root := "/repo/.amux/workspaces/feature"

	if !st.markCreatingWorkspace("owner-a", root) {
		t.Fatal("expected owner admission")
	}
	if st.markCreatingWorkspace("owner-b", root) {
		t.Fatal("expected second root owner refused")
	}
	if got := st.creatingRootID[root]; got != "owner-a" {
		t.Fatalf("bridge stolen by refused create: %q", got)
	}
	if st.phase("owner-b") == lifecycleCreating {
		t.Fatal("refused second owner entered the phase")
	}
	if !st.creatingInFlight("owner-b", root) {
		t.Fatal("probe must report the in-flight collision")
	}
}

// TestMarkCreatingWorkspaceRetryAfterOwnerSettles asserts the refusal is a
// phase guard, not a permanent lock: once the owner settles, a blocked retry
// is admitted.
func TestMarkCreatingWorkspaceRetryAfterOwnerSettles(t *testing.T) {
	st := newWorkspaceLifecycleState()
	root := "/repo/.amux/workspaces/feature"

	if !st.markCreatingWorkspace("owner-a", root) {
		t.Fatal("expected owner admission")
	}
	// The post-create record may carry a different identity — the drift the
	// root bridge exists to settle.
	ws := &data.Workspace{Name: "feature", Root: root, Repo: "/repo"}
	st.clearCreatingWorkspace(ws)
	if st.phase("owner-a") == lifecycleCreating || st.creatingRootID[root] != "" {
		t.Fatal("owner settle did not release phase and bridge")
	}
	if !st.markCreatingWorkspace("owner-b", root) {
		t.Fatal("expected retry admitted once the owner settled")
	}
}

// TestMarkCreatingRejectsRootlessReentry keeps the same-owner rule on the
// rootless path — re-entry is refused there too, mirroring markMutating.
func TestMarkCreatingRejectsRootlessReentry(t *testing.T) {
	st := newWorkspaceLifecycleState()
	if !st.markCreating("ws-1") {
		t.Fatal("expected first admission")
	}
	if st.markCreating("ws-1") {
		t.Fatal("expected rootless same-identity re-entry refused")
	}
	if st.phase("ws-1") != lifecycleCreating {
		t.Fatal("refused re-entry disturbed the owning create")
	}
}
