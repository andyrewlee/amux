package app

import (
	"sync"
	"testing"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/testutil/tmuxops"
)

// TestHandleTmuxTabsSyncResult_MutationInFlightIsolatesSibling proves a delete in
// flight for ws-A does not suppress a legitimate tab-status save for ws-B.
func TestHandleTmuxTabsSyncResult_MutationInFlightIsolatesSibling(t *testing.T) {
	wsA := data.NewWorkspace("a", "a", "main", "/repo", "/repo/a")
	wsB := data.NewWorkspace("b", "b", "main", "/repo", "/repo/b")
	for _, ws := range []*data.Workspace{wsA, wsB} {
		ws.OpenTabs = []data.TabInfo{{
			Name:        "agent",
			Assistant:   "claude",
			SessionName: "sess-" + ws.Name,
			Status:      "running",
		}}
	}

	store := &testutil.FakeWorkspaceStore{}
	svc := workspacesvc.New(nil, store, nil, "")
	app := &App{
		workspaceService: svc,
		projects: []data.Project{{
			Name: "repo", Path: "/repo",
			Workspaces: []data.Workspace{*wsA, *wsB},
		}},
		lifecycle: workspaceLifecycleState{
			phases: make(map[string]lifecyclePhase),
		},
	}
	app.markWorkspaceMutationInFlight(wsA, true)

	run := func(ws *data.Workspace) {
		cmds := app.handleTmuxTabsSyncResult(tmuxTabsSyncResult{
			WorkspaceID: string(ws.ID()),
			Updates: []tmuxTabStatusUpdate{{
				SessionName: "sess-" + ws.Name,
				Status:      "stopped",
			}},
		})
		for _, cmd := range cmds {
			if cmd != nil {
				_ = cmd()
			}
		}
	}
	run(wsA)
	run(wsB)

	saved := store.SavedIDs()
	for _, id := range saved {
		if id == string(wsA.ID()) {
			t.Fatalf("mutation-in-flight ws-A must not be saved, saved=%v", saved)
		}
	}
	foundB := false
	for _, id := range saved {
		if id == string(wsB.ID()) {
			foundB = true
		}
	}
	if !foundB {
		t.Fatalf("sibling ws-B save must proceed, saved=%v", saved)
	}
}

// TestKillWorkspaceSessionsSync_TagArgsAreWorkspaceScoped proves each cleanup
// carries only its own @amux_workspace tag — no cross-contamination between two
// workspaces' teardowns.
func TestKillWorkspaceSessionsSync_TagArgsAreWorkspaceScoped(t *testing.T) {
	ops := &tmuxops.FakeTmuxOps{}
	app := &App{tmuxService: ops, instanceID: "inst-A"}

	if err := app.killWorkspaceSessionsSync("ws-A"); err != nil {
		t.Fatal(err)
	}
	if err := app.killWorkspaceSessionsSync("ws-B"); err != nil {
		t.Fatal(err)
	}

	if len(ops.KillTagMatches()) != 2 {
		t.Fatalf("expected exactly two kill calls, got %d", len(ops.KillTagMatches()))
	}
	if got := ops.KillTagMatches()[0]["@amux_workspace"]; got != "ws-A" {
		t.Fatalf("first cleanup must target ws-A, got @amux_workspace=%q", got)
	}
	if got := ops.KillTagMatches()[1]["@amux_workspace"]; got != "ws-B" {
		t.Fatalf("second cleanup must target ws-B, got @amux_workspace=%q", got)
	}
	for i, tags := range ops.KillTagMatches() {
		if _, ok := tags["@amux_instance"]; ok {
			t.Fatalf("call %d must cover all instances, got %v", i, tags)
		}
	}
}

// TestWorkspaceMutationInFlight_PerWorkspaceIsolation proves the guard is keyed per
// workspace (not global/single-key) and is race-safe across two workspaces.
func TestWorkspaceMutationInFlight_PerWorkspaceIsolation(t *testing.T) {
	wsA := data.NewWorkspace("a", "a", "main", "/repo", "/repo/a")
	wsB := data.NewWorkspace("b", "b", "main", "/repo", "/repo/b")
	app := &App{lifecycle: workspaceLifecycleState{phases: make(map[string]lifecyclePhase)}}

	app.markWorkspaceMutationInFlight(wsA, true)
	if !app.isWorkspaceMutationInFlight(string(wsA.ID())) {
		t.Fatal("expected ws-A marked mutation-in-flight")
	}
	if app.isWorkspaceMutationInFlight(string(wsB.ID())) {
		t.Fatal("ws-B must not be affected by ws-A's mutation-in-flight mark")
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			app.markWorkspaceMutationInFlight(wsB, true)
			_ = app.isWorkspaceMutationInFlight(string(wsB.ID()))
			app.markWorkspaceMutationInFlight(wsB, false)
		}()
		go func() {
			defer wg.Done()
			_ = app.isWorkspaceMutationInFlight(string(wsA.ID()))
			_ = app.snapshotMutatingWorkspaceIDs()
		}()
	}
	wg.Wait()

	if !app.isWorkspaceMutationInFlight(string(wsA.ID())) {
		t.Fatal("ws-A must remain mutation-in-flight independent of ws-B churn")
	}
	if app.isWorkspaceMutationInFlight(string(wsB.ID())) {
		t.Fatal("ws-B must end un-marked after its final clear")
	}
}
