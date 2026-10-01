package data

import (
	"os"
	"sync"
	"testing"
)

func TestPortReservations_ReleaseMany(t *testing.T) {
	home := t.TempDir()
	s := newPortStore(t, home)
	if err := s.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	b1, e1 := mustReserve(t, s, "id-a", 6200, 10)
	mustReserve(t, s, "id-b", 6200, 10)
	mustReserve(t, s, "id-c", 6200, 10)

	// Two real owners + one that was never reserved: the missing ID is a
	// no-op, not an error — release must tolerate stale caller input.
	released, err := s.ReleaseMany([]string{"id-a", "id-c", "id-never"})
	if err != nil {
		t.Fatalf("ReleaseMany: %v", err)
	}
	if len(released) != 2 || released["id-a"].Start != b1 || released["id-a"].End != e1 {
		t.Fatalf("released = %v, want id-a(%d-%d) + id-c", released, b1, e1)
	}
	snap, err := s.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap) != 1 {
		t.Fatalf("remaining reservations = %v, want exactly id-b", snap)
	}
	if _, ok := snap["id-b"]; !ok {
		t.Fatalf("id-b evicted by release: %v", snap)
	}

	// Releasing an already-absent set leaves the registry untouched.
	released, err = s.ReleaseMany([]string{"id-a", "id-never"})
	if err != nil || len(released) != 0 {
		t.Fatalf("re-release = %v, %v; want empty,nil", released, err)
	}
	if snap, _ := s.Snapshot(); len(snap) != 1 {
		t.Fatalf("re-release mutated registry: %v", snap)
	}
}

func TestPortReservations_ReleaseManyNeverInitializes(t *testing.T) {
	s := newPortStore(t, t.TempDir())
	released, err := s.ReleaseMany([]string{"id-a"})
	if err != nil || len(released) != 0 {
		t.Fatalf("ReleaseMany on missing registry = %v, %v", released, err)
	}
	if _, statErr := os.Stat(s.Path()); !os.IsNotExist(statErr) {
		t.Fatal("release initialized a registry — writes must not create state for absent IDs")
	}
}

func TestPortReservations_ReleaseConcurrentWithReserve(t *testing.T) {
	home := t.TempDir()
	s := newPortStore(t, home)
	if err := s.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	mustReserve(t, s, "id-0", 6200, 10)
	mustReserve(t, s, "id-1", 6200, 10)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 25; i++ {
			if _, _, err := s.Reserve("id-2", 6200, 10); err != nil {
				t.Errorf("Reserve: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 25; i++ {
			if _, err := s.ReleaseMany([]string{"id-0", "id-1"}); err != nil {
				t.Errorf("ReleaseMany: %v", err)
				return
			}
		}
	}()
	wg.Wait()

	// The registry survived interleaved writes: still valid, no torn file.
	snap, err := s.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot after race: %v", err)
	}
	if _, ok := snap["id-2"]; !ok {
		t.Fatal("reserve lost to a concurrent release")
	}
}
