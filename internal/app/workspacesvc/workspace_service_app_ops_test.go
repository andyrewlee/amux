package workspacesvc

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
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
	if out := svc.LastScriptOutputs(&data.Workspace{}); out != nil {
		t.Fatal("nil service LastScriptOutputs must report nil")
	}
	if err := svc.RunOnDoneScript(&data.Workspace{}, "s"); err != nil {
		t.Fatal("nil service RunOnDoneScript must be a no-op")
	}
}
