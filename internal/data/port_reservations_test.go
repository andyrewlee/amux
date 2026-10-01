package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
)

func newPortStore(t *testing.T, home string) *PortReservationStore {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	return NewPortReservationStore(home)
}

func mustReserve(t *testing.T, s *PortReservationStore, id string, start, size int) (int, int) {
	t.Helper()
	base, end, err := s.Reserve(id, start, size)
	if err != nil {
		t.Fatalf("Reserve(%q) error = %v", id, err)
	}
	return base, end
}

// TestPortReservations_InitializeCreatesEmptyEnvelope proves guarded first
// adoption writes a valid versioned registry under the store's home.
func TestPortReservations_InitializeCreatesEmptyEnvelope(t *testing.T) {
	home := t.TempDir()
	s := NewPortReservationStore(home)
	if err := s.Initialize(nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var file portReservationFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("registry is not valid JSON: %v", err)
	}
	if file.Version != portReservationsFileVersion {
		t.Fatalf("version = %d, want %d", file.Version, portReservationsFileVersion)
	}
	if file.Reservations == nil || len(file.Reservations) != 0 {
		t.Fatalf("reservations = %v, want initialized empty map", file.Reservations)
	}
}

// TestPortReservations_InitializeGuardBlocksMissing proves a missing registry
// is created only through the guard, and a refused guard leaves no file.
func TestPortReservations_InitializeGuardBlocksMissing(t *testing.T) {
	home := t.TempDir()
	s := NewPortReservationStore(home)

	guardErr := errors.New("legacy sessions still live")
	calls := 0
	err := s.Initialize(func() error {
		calls++
		return guardErr
	})
	if !errors.Is(err, guardErr) {
		t.Fatalf("Initialize error = %v, want guard error", err)
	}
	if calls != 1 {
		t.Fatalf("guard ran %d times, want 1", calls)
	}
	if _, statErr := os.Stat(s.Path()); !os.IsNotExist(statErr) {
		t.Fatal("refused guard still created a registry")
	}

	// A succeeding guard then permits exactly one initialization.
	if err := s.Initialize(func() error { calls++; return nil }); err != nil {
		t.Fatalf("Initialize after guard cleared: %v", err)
	}
	if calls != 2 {
		t.Fatalf("guard ran %d times, want 2", calls)
	}
	// Re-initialize on an existing registry must not re-run the guard.
	if err := s.Initialize(func() error {
		calls++
		return errors.New("guard must not run against an existing registry")
	}); err != nil {
		t.Fatalf("re-Initialize: %v", err)
	}
	if calls != 2 {
		t.Fatalf("guard ran on existing registry (%d calls)", calls)
	}
}

// TestPortReservations_TwoStoresShareRegistry proves the second independent
// store object (a second app instance) observes the first's committed
// reservations instead of starting a parallel allocation space.
func TestPortReservations_TwoStoresShareRegistry(t *testing.T) {
	home := t.TempDir()
	a := newPortStore(t, home)
	b := newPortStore(t, home)
	if err := a.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	if err := b.Initialize(nil); err != nil {
		t.Fatal(err)
	}

	baseA, endA := mustReserve(t, a, "id-a", 6200, 10)
	if baseA != 6200 || endA != 6209 {
		t.Fatalf("id-a interval = %d-%d, want 6200-6209", baseA, endA)
	}
	// The second store sees id-a's interval and allocates a disjoint one.
	baseB, endB := mustReserve(t, b, "id-b", 6200, 10)
	if baseB <= endA && endB >= baseA {
		t.Fatalf("id-b interval %d-%d overlaps id-a's %d-%d", baseB, endB, baseA, endA)
	}
	// Same-ID re-reserve through the OTHER store returns the first's interval.
	gotB, gotEnd, err := b.Reserve("id-a", 6200, 10)
	if err != nil {
		t.Fatal(err)
	}
	if gotB != baseA || gotEnd != endA {
		t.Fatalf("id-a via second store = %d-%d, want %d-%d", gotB, gotEnd, baseA, endA)
	}
	// And Lookup sees it without allocating.
	lb, le, found, err := b.Lookup("id-a")
	if err != nil || !found || lb != baseA || le != endA {
		t.Fatalf("Lookup(id-a) = (%d,%d,%v,%v), want (%d,%d,true,nil)", lb, le, found, err, baseA, endA)
	}
	if _, _, found, err := b.Lookup("id-never"); err != nil || found {
		t.Fatalf("Lookup(id-never) = found=%v err=%v, want false/nil", found, err)
	}
}

// TestPortReservations_ChangedConfigKeepsPersistedIntervals proves existing
// IDs keep their stored interval — including its width — after the configured
// start/size changes, and new IDs never overlap the old records.
func TestPortReservations_ChangedConfigKeepsPersistedIntervals(t *testing.T) {
	home := t.TempDir()
	s := newPortStore(t, home)
	if err := s.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	base, end := mustReserve(t, s, "old-id", 6200, 10)
	if base != 6200 || end != 6209 {
		t.Fatalf("old-id = %d-%d, want 6200-6209", base, end)
	}

	// Config moves to a different base and width: old-id must still get its
	// persisted 6200-6209 verbatim.
	gotBase, gotEnd, err := s.Reserve("old-id", 8000, 4)
	if err != nil {
		t.Fatal(err)
	}
	if gotBase != 6200 || gotEnd != 6209 {
		t.Fatalf("old-id under new config = %d-%d, want persisted 6200-6209", gotBase, gotEnd)
	}
	// A new ID under the new config gets a configured-width interval that
	// does not overlap the persisted one.
	nBase, nEnd := mustReserve(t, s, "new-id", 8000, 4)
	if nEnd-nBase != 3 {
		t.Fatalf("new-id width = %d, want configured 4", nEnd-nBase+1)
	}
	if nBase <= 6209 && nEnd >= 6200 {
		t.Fatalf("new-id interval %d-%d overlaps persisted 6200-6209", nBase, nEnd)
	}
}

// TestPortReservations_Exhaustion proves a full space reports the typed
// exhaustion error rather than overlapping a persisted interval.
func TestPortReservations_Exhaustion(t *testing.T) {
	home := t.TempDir()
	s := newPortStore(t, home)
	if err := s.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	mustReserve(t, s, "only", 1, 65535)
	if _, _, err := s.Reserve("no-room", 1, 65535); !errors.Is(err, ErrPortReservationsExhausted) {
		t.Fatalf("exhausted Reserve error = %v, want ErrPortReservationsExhausted", err)
	}
}

// TestPortReservations_InvalidRangeConfig proves malformed configured
// start/size fails before any allocation arithmetic runs.
func TestPortReservations_InvalidRangeConfig(t *testing.T) {
	home := t.TempDir()
	s := newPortStore(t, home)
	if err := s.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ start, size int }{
		{0, 10},    // base below port space
		{65536, 1}, // base above port space
		{6200, 0},  // zero width
		{6200, -3}, // negative width
		{1, 65536}, // width exceeds whole space
		{65535, 2}, // end would exceed 65535
	} {
		if _, _, err := s.Reserve("id", tc.start, tc.size); !errors.Is(err, ErrPortReservationRangeInvalid) {
			t.Fatalf("Reserve(start=%d,size=%d) error = %v, want ErrPortReservationRangeInvalid", tc.start, tc.size, err)
		}
	}
}

// TestPortReservations_RejectsInvalidIDs proves empty/garbage IDs never reach
// the registry.
func TestPortReservations_RejectsInvalidIDs(t *testing.T) {
	home := t.TempDir()
	s := newPortStore(t, home)
	if err := s.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "  ", "a/b", "a\\b", "..", "x\x00y"} {
		if _, _, err := s.Reserve(id, 6200, 10); !errors.Is(err, ErrPortReservationsInvalid) {
			t.Fatalf("Reserve(id=%q) error = %v, want ErrPortReservationsInvalid", id, err)
		}
	}
}

// TestPortReservations_CorruptDataFailsClosed proves every malformed envelope
// shape surfaces the typed error and leaves the bytes byte-for-byte intact.
func TestPortReservations_CorruptDataFailsClosed(t *testing.T) {
	cases := map[string]string{
		"truncated":          `{"version": 1, "reservations": {"a"`,
		"null file":          `null`,
		"null reservations":  `{"version": 1, "reservations": null}`,
		"no version":         `{"reservations": {}}`,
		"old schema":         `{"version": 0, "reservations": {}}`,
		"overlap":            `{"version": 1, "reservations": {"a": {"start": 6200, "end": 6209}, "b": {"start": 6205, "end": 6214}}}`,
		"out of range":       `{"version": 1, "reservations": {"a": {"start": 0, "end": 6209}}}`,
		"inverted":           `{"version": 1, "reservations": {"a": {"start": 6210, "end": 6200}}}`,
		"above max":          `{"version": 1, "reservations": {"a": {"start": 65500, "end": 70000}}}`,
		"dup key":            `{"version": 1, "reservations": {"a": {"start": 6200, "end": 6209}, "a": {"start": 6300, "end": 6309}}}`,
		"bad id":             `{"version": 1, "reservations": {"a/b": {"start": 6200, "end": 6209}}}`,
		"reservations array": `{"version": 1, "reservations": [1, 2]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			s := NewPortReservationStore(home)
			path := s.Path()
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := s.Initialize(nil); !errors.Is(err, ErrPortReservationsInvalid) {
				t.Fatalf("Initialize error = %v, want ErrPortReservationsInvalid", err)
			}
			if _, _, err := s.Reserve("new", 6200, 10); !errors.Is(err, ErrPortReservationsInvalid) {
				t.Fatalf("Reserve error = %v, want ErrPortReservationsInvalid", err)
			}
			if _, _, _, err := s.Lookup("new"); !errors.Is(err, ErrPortReservationsInvalid) {
				t.Fatalf("Lookup error = %v, want ErrPortReservationsInvalid", err)
			}
			// Bytes untouched — no silent repair ever ran.
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != content {
				t.Fatalf("corrupt registry was modified: %q → %q", content, got)
			}
		})
	}
}

// TestPortReservations_FutureSchemaRefused proves a newer-version registry —
// intact data from a newer binary — fails closed with the schema error.
func TestPortReservations_FutureSchemaRefused(t *testing.T) {
	home := t.TempDir()
	s := NewPortReservationStore(home)
	content := `{"version": 99, "reservations": {}}`
	if err := os.WriteFile(s.Path(), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Initialize(nil); !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("Initialize error = %v, want ErrUnsupportedSchemaVersion", err)
	}
	if _, _, err := s.Reserve("id", 6200, 10); !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("Reserve error = %v, want ErrUnsupportedSchemaVersion", err)
	}
}

// TestPortReservations_WriteFailure keeps the refusal honest: when the atomic
// write cannot commit, the typed error surfaces and the prior bytes remain.
func TestPortReservations_WriteFailure(t *testing.T) {
	home := t.TempDir()
	s := newPortStore(t, home)
	if err := s.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	// Read-only home: the lock file already exists so acquisition succeeds,
	// but the atomic rename's temp file cannot be created inside.
	if err := os.Chmod(home, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o700) })
	// Probe: some environments (root, ACL-less filesystems) can still write
	// into a 0555 dir — the assertion below is meaningless there.
	if probe, err := os.CreateTemp(home, "probe-*"); err == nil {
		_ = probe.Close()
		_ = os.Remove(probe.Name())
		t.Skip("home dir permission bits are not enforced here")
	}
	if _, _, err := s.Reserve("id", 6200, 10); err == nil {
		t.Fatal("Reserve succeeded in a read-only home")
	}
	_ = os.Chmod(home, 0o700)
	after, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed write mutated the committed envelope")
	}
}

// TestPortReservations_ConcurrentIndependentStores runs two store objects
// (stand-ins for two app processes) through simultaneous reserves and
// verifies the committed intervals are pairwise non-overlapping.
func TestPortReservations_ConcurrentIndependentStores(t *testing.T) {
	home := t.TempDir()
	a := newPortStore(t, home)
	b := newPortStore(t, home)
	if err := a.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	if err := b.Initialize(nil); err != nil {
		t.Fatal(err)
	}

	const n = 12
	type result struct {
		base, end int
		err       error
	}
	results := make([]result, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			store := a
			if i%2 == 1 {
				store = b
			}
			base, end, err := store.Reserve(fmt.Sprintf("id-%d", i), 6200, 10)
			results[i] = result{base: base, end: end, err: err}
		}(i)
	}
	wg.Wait()

	intervals := make(map[string][2]int, n)
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("Reserve(id-%d) error = %v", i, r.err)
		}
		intervals[fmt.Sprintf("id-%d", i)] = [2]int{r.base, r.end}
	}
	seen := make(map[int]string, n)
	for id, iv := range intervals {
		for other, oiv := range intervals {
			if id == other {
				continue
			}
			if iv[0] <= oiv[1] && iv[1] >= oiv[0] {
				t.Fatalf("intervals %q %v and %q %v overlap", id, iv, other, oiv)
			}
		}
		if dup, ok := seen[iv[0]]; ok {
			t.Fatalf("base %d committed to both %q and %q", iv[0], dup, id)
		}
		seen[iv[0]] = id
	}
	// The committed file holds exactly these intervals.
	_, _, found, err := a.Lookup("id-3")
	if err != nil || !found {
		t.Fatalf("Lookup(id-3) = found=%v err=%v", found, err)
	}
}

// TestPortReservations_MissingRegistryReinitializesThroughGuard proves a
// Reserve that finds the file gone applies the same guarded initialization —
// a missing registry is never a license to skip the adoption check.
func TestPortReservations_MissingRegistryReinitializesThroughGuard(t *testing.T) {
	home := t.TempDir()
	s := newPortStore(t, home)
	if err := s.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.Path()); err != nil {
		t.Fatal(err)
	}
	guardErr := errors.New("legacy sessions still live")
	s.guardMu.Lock()
	s.guard = func() error { return guardErr }
	s.guardMu.Unlock()

	if _, _, err := s.Reserve("id", 6200, 10); !errors.Is(err, guardErr) {
		t.Fatalf("Reserve on missing registry error = %v, want guard refusal", err)
	}
	if _, statErr := os.Stat(s.Path()); !os.IsNotExist(statErr) {
		t.Fatal("refused re-initialization still created a registry")
	}
}

// TestPortReservations_SnapshotIsReadOnlyCopy proves the enumeration view
// is a detached copy: mutating it cannot touch the registry, a missing
// registry reads empty without initializing a file, and every reserved ID
// round-trips with its interval.
func TestPortReservations_SnapshotIsReadOnlyCopy(t *testing.T) {
	s := newPortStore(t, t.TempDir())
	if err := s.Initialize(nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	mustReserve(t, s, "id-a", 6200, 10)
	mustReserve(t, s, "id-b", 6200, 10)

	snap, err := s.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap) != 2 || snap["id-a"].Start == 0 || snap["id-b"].Start == 0 {
		t.Fatalf("snapshot missing reservations: %v", snap)
	}

	// Mutating the copy must not leak into the store.
	snap["id-a"] = PortReservationInterval{Start: 1, End: 1}
	delete(snap, "id-b")
	again, err := s.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot(2): %v", err)
	}
	if len(again) != 2 || again["id-b"].Start == 0 {
		t.Fatalf("store affected by copy mutation: %v", again)
	}
}

func TestPortReservations_SnapshotMissingRegistryReadsEmpty(t *testing.T) {
	s := newPortStore(t, t.TempDir())
	snap, err := s.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot on missing registry: %v", err)
	}
	if len(snap) != 0 {
		t.Fatalf("missing registry snapshot = %v, want empty", snap)
	}
	if _, statErr := os.Stat(s.Path()); !os.IsNotExist(statErr) {
		t.Fatal("a read initialized a registry — reads must not create state")
	}
}
