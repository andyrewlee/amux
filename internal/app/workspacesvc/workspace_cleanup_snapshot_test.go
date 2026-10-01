package workspacesvc

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/testutil"
)

// WorkspaceCleanupSnapshot tests cover the read-only tombstone probe the
// status dialog renders: every metadata-ID form must be checked (alias
// tombstones surface), and the probe must never mutate — it is diagnostics,
// not a retry path.

func TestWorkspaceCleanupSnapshot_NoStore(t *testing.T) {
	svc := New(nil, nil, nil, "")
	ws := data.NewWorkspace("ws", "feat", "main", "/repo", "/repo/.amux/ws")
	if got := svc.WorkspaceCleanupSnapshot(ws); got != WorkspaceCleanupNone {
		t.Fatalf("nil store: got %v, want None", got)
	}
}

func TestWorkspaceCleanupSnapshot_NoTombstone(t *testing.T) {
	store := data.NewWorkspaceStore(t.TempDir())
	svc := New(nil, store, nil, "")
	ws := data.NewWorkspace("ws", "feat", "main", t.TempDir(), t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := svc.WorkspaceCleanupSnapshot(ws); got != WorkspaceCleanupNone {
		t.Fatalf("no tombstone: got %v, want None", got)
	}
}

func TestWorkspaceCleanupSnapshot_Interrupted(t *testing.T) {
	store := data.NewWorkspaceStore(t.TempDir())
	svc := New(nil, store, nil, "")
	ws := data.NewWorkspace("ws", "feat", "main", t.TempDir(), t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	id, ok := ws.StoredID()
	if !ok {
		t.Fatal("saved workspace has no StoredID")
	}
	if err := store.MarkDeleting(id); err != nil {
		t.Fatalf("MarkDeleting: %v", err)
	}
	// Root still exists → interrupted-delete state.
	if got := svc.WorkspaceCleanupSnapshot(ws); got != WorkspaceCleanupInterrupted {
		t.Fatalf("tombstone+live root: got %v, want Interrupted", got)
	}
}

func TestWorkspaceCleanupSnapshot_Pending(t *testing.T) {
	store := data.NewWorkspaceStore(t.TempDir())
	svc := New(nil, store, nil, "")
	ws := data.NewWorkspace("ws", "feat", "main", t.TempDir(), t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	id, _ := ws.StoredID()
	if err := store.MarkDeleting(id); err != nil {
		t.Fatalf("MarkDeleting: %v", err)
	}
	// Root already gone → pending; startup recovery retries on next load.
	ws.Root = t.TempDir() + "/removed"
	if got := svc.WorkspaceCleanupSnapshot(ws); got != WorkspaceCleanupPending {
		t.Fatalf("tombstone+gone root: got %v, want Pending", got)
	}
}

// A tombstone written under a legacy alias ID (the computed path-hash form
// while the canonical store ID differs) still surfaces — mirroring
// finishInterruptedDelete's identity-set probe.
func TestWorkspaceCleanupSnapshot_AliasID(t *testing.T) {
	store := data.NewWorkspaceStore(t.TempDir())
	svc := New(nil, store, nil, "")
	ws := data.NewWorkspace("ws", "feat", "main", t.TempDir(), t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Drift the computed identity away from the stored key: only the
	// legacy path-hash form carries the marker.
	ws.Root = t.TempDir() + "/drifted-root"
	alias := ws.ComputedID()
	stored, _ := ws.StoredID()
	if alias == stored {
		t.Fatal("setup: alias and canonical IDs must differ for this case")
	}
	if err := store.MarkDeleting(alias); err != nil {
		t.Fatalf("MarkDeleting(alias): %v", err)
	}
	if got := svc.WorkspaceCleanupSnapshot(ws); got != WorkspaceCleanupPending {
		t.Fatalf("alias-ID tombstone: got %v, want Pending", got)
	}
}

// The probe is diagnostics only — a spy store proves no mutating call is
// ever issued, even when a tombstone exists.
func TestWorkspaceCleanupSnapshot_NeverMutates(t *testing.T) {
	var mark, clr, del, save, update int
	store := &testutil.FakeWorkspaceStore{
		IsDeletingFunc: func(data.WorkspaceID) bool { return true },
		MarkDeletingFunc: func(data.WorkspaceID) error {
			mark++
			return nil
		},
		ClearDeletingFunc: func(data.WorkspaceID) error {
			clr++
			return nil
		},
		DeleteFunc: func(data.WorkspaceID) error {
			del++
			return nil
		},
		SaveFunc: func(*data.Workspace) error {
			save++
			return nil
		},
		UpdateFunc: func(data.WorkspaceID, func(*data.Workspace) (bool, error)) error {
			update++
			return nil
		},
	}
	svc := New(nil, store, nil, "")
	ws := data.NewWorkspace("ws", "feat", "main", t.TempDir(), t.TempDir())
	if got := svc.WorkspaceCleanupSnapshot(ws); got != WorkspaceCleanupInterrupted {
		t.Fatalf("spy store tombstone: got %v, want Interrupted", got)
	}
	if mark+clr+del+save+update != 0 {
		t.Fatalf("probe mutated: mark=%d clr=%d delete=%d save=%d update=%d",
			mark, clr, del, save, update)
	}
}

// Delete-side regression: a probe error inside IsDeleting degrades to no
// section rather than surfacing a phantom pending state — the boolean
// signature cannot distinguish absence from failure, and None is the
// honest low-side answer.
func TestWorkspaceCleanupSnapshot_ProbeErrorIsNotPending(t *testing.T) {
	store := &testutil.FakeWorkspaceStore{
		IsDeletingFunc: func(data.WorkspaceID) bool { return false },
	}
	svc := New(nil, store, nil, "")
	ws := data.NewWorkspace("ws", "feat", "main", t.TempDir(), t.TempDir())
	if got := svc.WorkspaceCleanupSnapshot(ws); got != WorkspaceCleanupNone {
		t.Fatalf("unreadable marker treated as pending: got %v", got)
	}
}
