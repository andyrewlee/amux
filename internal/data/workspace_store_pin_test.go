package data

import "testing"

func TestSetPinnedPersistsAndGuardsNoOp(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ws := NewWorkspace("feature", "feature", "main", "/repo", "/repo-ws/feature")
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := store.SetPinned(ws.ID(), true); err != nil {
		t.Fatalf("SetPinned: %v", err)
	}
	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.Pinned {
		t.Fatal("expected workspace to be pinned after SetPinned(true)")
	}

	// Same-value write is a no-op (mirrors Rename/SetEnv guards).
	if err := store.SetPinned(ws.ID(), true); err != nil {
		t.Fatalf("SetPinned no-op: %v", err)
	}

	if err := store.SetPinned(ws.ID(), false); err != nil {
		t.Fatalf("SetPinned(false): %v", err)
	}
	loaded, err = store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Pinned {
		t.Fatal("expected workspace to be unpinned after SetPinned(false)")
	}
}

func TestSetPinnedMissingWorkspaceErrors(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	if err := store.SetPinned(WorkspaceID("nope"), true); err == nil {
		t.Fatal("expected error for unknown workspace")
	}
}
