package data

import (
	"testing"
)

// TestWorkspaceIdentitySet pins the canonical artifact-match set: stable
// forms first, ComputedID last, duplicates removed. The drifted-persisted
// case is the one that matters — artifacts stamped under the old path hash
// stay reachable only if ComputedID is in the set.
func TestWorkspaceIdentitySet(t *testing.T) {
	t.Run("persisted and drifted: store key plus computed form", func(t *testing.T) {
		f := newDriftFixture(t)
		if err := f.store.Save(f.ws); err != nil {
			t.Fatal(err)
		}
		stored := f.ws.MetadataID()
		f.createRoot() // ComputedID drifts to the resolved form
		if f.ws.ComputedID() == stored {
			t.Fatal("fixture produced no drift")
		}
		got := WorkspaceIdentitySet(f.ws)
		want := []WorkspaceID{stored, f.ws.ComputedID()}
		if len(got) != len(want) {
			t.Fatalf("identity set = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("identity set[%d] = %s, want %s (stable forms first)", i, got[i], want[i])
			}
		}
	})

	t.Run("persisted without drift collapses to the store key", func(t *testing.T) {
		f := newDriftFixture(t)
		if err := f.store.Save(f.ws); err != nil {
			t.Fatal(err)
		}
		got := WorkspaceIdentitySet(f.ws)
		if len(got) != 1 || got[0] != f.ws.MetadataID() {
			t.Fatalf("identity set = %v, want [%s]", got, f.ws.MetadataID())
		}
	})

	t.Run("unsaved collapses to the computed form", func(t *testing.T) {
		f := newDriftFixture(t)
		got := WorkspaceIdentitySet(f.ws)
		if len(got) != 1 || got[0] != f.ws.ComputedID() {
			t.Fatalf("identity set = %v, want [%s]", got, f.ws.ComputedID())
		}
	})

	t.Run("nil workspace returns nil", func(t *testing.T) {
		if got := WorkspaceIdentitySet(nil); got != nil {
			t.Fatalf("identity set = %v, want nil", got)
		}
	})

	t.Run("string form preserves order and values", func(t *testing.T) {
		f := newDriftFixture(t)
		if err := f.store.Save(f.ws); err != nil {
			t.Fatal(err)
		}
		f.createRoot()
		ids := WorkspaceIdentitySet(f.ws)
		strs := WorkspaceIdentityStrings(f.ws)
		if len(strs) != len(ids) {
			t.Fatalf("strings = %v, ids = %v", strs, ids)
		}
		for i := range ids {
			if strs[i] != string(ids[i]) {
				t.Fatalf("strings[%d] = %s, want %s", i, strs[i], ids[i])
			}
		}
	})
}
