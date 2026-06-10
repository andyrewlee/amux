package app

import (
	"sync"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

// recordingWorkspaceStore records the IDs passed to Save so a test can prove the
// delete-in-flight guard suppresses only the targeted workspace's save.
type recordingWorkspaceStore struct {
	mu       sync.Mutex
	savedIDs []string
}

func (s *recordingWorkspaceStore) ListByRepo(string) ([]*data.Workspace, error) { return nil, nil }
func (s *recordingWorkspaceStore) ListByRepoIncludingArchived(string) ([]*data.Workspace, error) {
	return nil, nil
}

func (s *recordingWorkspaceStore) LoadMetadataFor(*data.Workspace) (bool, error) { return false, nil }
func (s *recordingWorkspaceStore) UpsertFromDiscovery(*data.Workspace) error     { return nil }

func (s *recordingWorkspaceStore) Save(ws *data.Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.savedIDs = append(s.savedIDs, string(ws.ID()))
	return nil
}

func (s *recordingWorkspaceStore) Delete(data.WorkspaceID) error    { return nil }
func (s *recordingWorkspaceStore) ResolvedDefaultAssistant() string { return data.DefaultAssistant }

func (s *recordingWorkspaceStore) saved() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.savedIDs...)
}

// multiKillRecordingTmuxOps captures the tag map of every KillSessionsMatchingTags
// call so cross-workspace cleanup contamination can be asserted.
type multiKillRecordingTmuxOps struct {
	stubTmuxOps
	allKillTags []map[string]string
}

func (k *multiKillRecordingTmuxOps) KillSessionsMatchingTags(tags map[string]string, _ tmux.Options) (bool, error) {
	copyTags := make(map[string]string, len(tags))
	for key, val := range tags {
		copyTags[key] = val
	}
	k.allKillTags = append(k.allKillTags, copyTags)
	return false, nil
}

func (k *multiKillRecordingTmuxOps) KillWorkspaceSessions(string, tmux.Options) error { return nil }

// TestHandleTmuxTabsSyncResult_DeleteInFlightIsolatesSibling proves a delete in
// flight for ws-A does not suppress a legitimate tab-status save for ws-B.
func TestHandleTmuxTabsSyncResult_DeleteInFlightIsolatesSibling(t *testing.T) {
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

	store := &recordingWorkspaceStore{}
	svc := newWorkspaceService(nil, store, nil, "")
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
	app.markWorkspaceDeleteInFlight(wsA, true)

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

	saved := store.saved()
	for _, id := range saved {
		if id == string(wsA.ID()) {
			t.Fatalf("delete-in-flight ws-A must not be saved, saved=%v", saved)
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
	ops := &multiKillRecordingTmuxOps{}
	app := &App{tmuxService: ops, instanceID: "inst-A"}

	app.killWorkspaceSessionsSync("ws-A")
	app.killWorkspaceSessionsSync("ws-B")

	if len(ops.allKillTags) != 2 {
		t.Fatalf("expected exactly two kill calls, got %d", len(ops.allKillTags))
	}
	if got := ops.allKillTags[0]["@amux_workspace"]; got != "ws-A" {
		t.Fatalf("first cleanup must target ws-A, got @amux_workspace=%q", got)
	}
	if got := ops.allKillTags[1]["@amux_workspace"]; got != "ws-B" {
		t.Fatalf("second cleanup must target ws-B, got @amux_workspace=%q", got)
	}
	for i, tags := range ops.allKillTags {
		if tags["@amux_instance"] != "inst-A" {
			t.Fatalf("call %d must stay instance-scoped, got %v", i, tags)
		}
	}
}

// TestWorkspaceDeleteInFlight_PerWorkspaceIsolation proves the guard is keyed per
// workspace (not global/single-key) and is race-safe across two workspaces.
func TestWorkspaceDeleteInFlight_PerWorkspaceIsolation(t *testing.T) {
	wsA := data.NewWorkspace("a", "a", "main", "/repo", "/repo/a")
	wsB := data.NewWorkspace("b", "b", "main", "/repo", "/repo/b")
	app := &App{lifecycle: workspaceLifecycleState{phases: make(map[string]lifecyclePhase)}}

	app.markWorkspaceDeleteInFlight(wsA, true)
	if !app.isWorkspaceDeleteInFlight(string(wsA.ID())) {
		t.Fatal("expected ws-A marked delete-in-flight")
	}
	if app.isWorkspaceDeleteInFlight(string(wsB.ID())) {
		t.Fatal("ws-B must not be affected by ws-A's delete-in-flight mark")
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			app.markWorkspaceDeleteInFlight(wsB, true)
			_ = app.isWorkspaceDeleteInFlight(string(wsB.ID()))
			app.markWorkspaceDeleteInFlight(wsB, false)
		}()
		go func() {
			defer wg.Done()
			_ = app.isWorkspaceDeleteInFlight(string(wsA.ID()))
			_ = app.snapshotDeletingWorkspaceIDs()
		}()
	}
	wg.Wait()

	if !app.isWorkspaceDeleteInFlight(string(wsA.ID())) {
		t.Fatal("ws-A must remain delete-in-flight independent of ws-B churn")
	}
	if app.isWorkspaceDeleteInFlight(string(wsB.ID())) {
		t.Fatal("ws-B must end un-marked after its final clear")
	}
}
