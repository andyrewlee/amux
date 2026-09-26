package workspacesvc

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/process"
)

// TestAppOpsUseMetadataID pins the whole point of the app-op methods: the
// service picks the persisted store key (MetadataID), not the drifted
// path-derived form (ComputedID). The fixture saves a workspace whose root is
// missing, then creates the root behind a symlink so ComputedID resolves to a
// different identity — a method keyed on the wrong form would write a second
// record instead of mutating the saved one.
func TestAppOpsUseMetadataID(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	repo := filepath.Join(target, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ws := data.NewWorkspace("feat", "feat", "main", repo, filepath.Join(link, "feat"))
	ws.Created = time.Now()

	store := data.NewWorkspaceStore(filepath.Join(base, "store"))
	if err := store.Save(ws); err != nil {
		t.Fatal(err)
	}
	stored := ws.MetadataID()
	if err := os.MkdirAll(filepath.Join(link, "feat"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ws.ComputedID() == stored {
		t.Fatal("fixture produced no drift — identity forms are indistinguishable")
	}

	svc := New(nil, store, nil, filepath.Join(base, "workspaces"))
	if err := svc.RenameWorkspace(ws, "renamed"); err != nil {
		t.Fatalf("RenameWorkspace: %v", err)
	}
	loaded, err := store.Load(stored)
	if err != nil || loaded.Name != "renamed" {
		t.Fatalf("Load(stored): name=%q err=%v — mutation did not land on the persisted key", loaded.Name, err)
	}
	if _, err := store.Load(ws.ComputedID()); err == nil {
		t.Fatal("a record exists under the drifted ComputedID — the method keyed on the wrong identity form")
	}

	if err := svc.SetWorkspaceEnv(ws, map[string]string{"K": "v"}); err != nil {
		t.Fatalf("SetWorkspaceEnv: %v", err)
	}
	loaded, err = store.Load(stored)
	if err != nil || loaded.Env["K"] != "v" {
		t.Fatalf("SetWorkspaceEnv did not land on the persisted key: %+v err=%v", loaded, err)
	}
	if err := svc.SetWorkspaceScripts(ws, data.ScriptsConfig{Run: "make run"}, "concurrent"); err != nil {
		t.Fatalf("SetWorkspaceScripts: %v", err)
	}
	loaded, err = store.Load(stored)
	if err != nil || loaded.Scripts.Run != "make run" || loaded.ScriptMode != "concurrent" {
		t.Fatalf("SetWorkspaceScripts did not land on the persisted key: %+v err=%v", loaded, err)
	}
}

// TestAppOpsNilSafe pins the nil-runner/nil-store contracts app callers rely
// on after dropping their own probing: reads report zero values, mutations
// report an error rather than panic.
func TestAppOpsNilSafe(t *testing.T) {
	var svc *Service
	if err := svc.RenameWorkspace(&data.Workspace{}, "x"); err == nil {
		t.Fatal("nil service RenameWorkspace must report an error")
	}
	if cfg, err := svc.ScriptConfig("/repo"); cfg != nil || err != nil {
		t.Fatal("nil service ScriptConfig must report (nil, nil)")
	}
	if trusted, err := svc.WorkspaceScriptsTrusted("/repo"); trusted || err != nil {
		t.Fatal("nil service WorkspaceScriptsTrusted must report (false, nil)")
	}
	if _, ok := svc.WorkspaceScriptPort(&data.Workspace{}); ok {
		t.Fatal("nil service WorkspaceScriptPort must report not-allocated")
	}
	if _, _, found, err := svc.WorkspacePortInterval(&data.Workspace{}); found || err != nil {
		t.Fatal("nil service WorkspacePortInterval must report (0,0,false,nil)")
	}
	if out := svc.LastScriptOutputs(&data.Workspace{}); out != nil {
		t.Fatal("nil service LastScriptOutputs must report nil")
	}
	if err := svc.RunOnDoneScript(&data.Workspace{}, "s"); err != nil {
		t.Fatal("nil service RunOnDoneScript must be a no-op")
	}
}

// TestWorkspacePortIntervalReadsDurableRegistry pins the status path's
// contract end to end through the service: the interval comes from the shared
// durable registry (not the in-memory map), a lookup for a workspace with no
// reservation reports not-found without allocating, and an unsaved workspace
// reports not-found rather than minting a path-keyed record.
func TestWorkspacePortIntervalReadsDurableRegistry(t *testing.T) {
	base := t.TempDir()
	store := data.NewWorkspaceStore(filepath.Join(base, "store"))
	ws := data.NewWorkspace("feat", "feat", "main", filepath.Join(base, "repo"), filepath.Join(base, "ws"))
	if err := store.Save(ws); err != nil {
		t.Fatal(err)
	}
	unsaved := data.NewWorkspace("other", "other", "main", filepath.Join(base, "repo"), filepath.Join(base, "other"))

	home := filepath.Join(base, "home")
	reservations := data.NewPortReservationStore(home)
	runner := process.NewScriptRunner(6200, 25)
	runner.SetPortReservationStore(reservations)
	svc := New(nil, store, runner, filepath.Join(base, "workspaces"))

	// Another consumer (a "second app instance") mints the reservation first.
	other := process.NewScriptRunner(6200, 25)
	other.SetPortReservationStore(reservations)
	if _, err := other.BuildSessionEnv(ws); err != nil {
		t.Fatalf("BuildSessionEnv: %v", err)
	}

	gotBase, gotEnd, found, err := svc.WorkspacePortInterval(ws)
	if err != nil || !found {
		t.Fatalf("WorkspacePortInterval: found=%v err=%v", found, err)
	}
	if gotBase != 6200 || gotEnd != 6224 {
		t.Fatalf("WorkspacePortInterval = %d-%d, want 6200-6224 (the committed interval)", gotBase, gotEnd)
	}
	// The service's own runner never allocated locally for the read.
	if _, ok := runner.PortAllocated(ws); ok {
		t.Fatal("read through the service populated the runner's in-memory map — lookup allocated")
	}

	if _, _, found, err := svc.WorkspacePortInterval(unsaved); found || err != nil {
		t.Fatalf("unsaved workspace: found=%v err=%v — must report not-found without error", found, err)
	}
}
