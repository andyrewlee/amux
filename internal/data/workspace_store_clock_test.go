package data

import (
	"testing"
	"time"
)

// TestWorkspaceStore_UpsertFromDiscovery_StampsCreatedFromInjectedClock verifies
// the discovery "no stored record" path stamps Created from the store's
// injectable clock rather than the wall clock, so the timestamp is assertable.
func TestWorkspaceStore_UpsertFromDiscovery_StampsCreatedFromInjectedClock(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	fixed := time.Date(2021, 6, 21, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return fixed }

	discovered := &Workspace{
		Name:   "fresh",
		Branch: "feature",
		Repo:   "/repo",
		Root:   "/root/fresh",
	}
	if err := store.UpsertFromDiscovery(discovered); err != nil {
		t.Fatalf("UpsertFromDiscovery() error = %v", err)
	}

	loaded, err := store.Load(discovered.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !loaded.Created.Equal(fixed) {
		t.Fatalf("Created = %v, want %v", loaded.Created, fixed)
	}
}

// TestWorkspaceStore_ClockFallsBackToTimeNow guards the nil-clock path so a
// zero-value or literal-constructed store doesn't panic.
func TestWorkspaceStore_ClockFallsBackToTimeNow(t *testing.T) {
	var s WorkspaceStore // no constructor: now is nil
	if got := s.clock(); got.IsZero() {
		t.Fatal("clock() with nil now should fall back to time.Now, got zero time")
	}
}
