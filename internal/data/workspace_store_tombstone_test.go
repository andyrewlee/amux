package data

import "testing"

// TestWorkspaceStore_DeleteTombstone covers the tombstone lifecycle: MarkDeleting
// makes IsDeleting true, ClearDeleting removes the marker without touching
// metadata, and Delete removes the marker along with the whole metadata dir.
func TestWorkspaceStore_DeleteTombstone(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := &Workspace{Name: "tomb", Repo: "/repo", Root: "/repo/tomb"}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	id := ws.ID()

	if store.IsDeleting(id) {
		t.Fatal("a fresh workspace must not be marked deleting")
	}

	if err := store.MarkDeleting(id); err != nil {
		t.Fatalf("MarkDeleting: %v", err)
	}
	if !store.IsDeleting(id) {
		t.Fatal("expected IsDeleting true after MarkDeleting")
	}

	// ClearDeleting removes only the marker; the metadata survives.
	if err := store.ClearDeleting(id); err != nil {
		t.Fatalf("ClearDeleting: %v", err)
	}
	if store.IsDeleting(id) {
		t.Fatal("expected marker cleared after ClearDeleting")
	}
	if _, err := store.Load(id); err != nil {
		t.Fatalf("metadata must survive ClearDeleting: %v", err)
	}
	// Clearing an already-clear tombstone is not an error.
	if err := store.ClearDeleting(id); err != nil {
		t.Fatalf("ClearDeleting (idempotent): %v", err)
	}

	// A surviving tombstone (crash before metadata removal) is cleared by Delete
	// along with the whole directory.
	if err := store.MarkDeleting(id); err != nil {
		t.Fatalf("re-MarkDeleting: %v", err)
	}
	if err := store.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if store.IsDeleting(id) {
		t.Fatal("Delete must clear the tombstone with the directory")
	}
	if _, err := store.Load(id); err == nil {
		t.Fatal("metadata must be gone after Delete")
	}
}
