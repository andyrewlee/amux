package data

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransientReservationKey_Scopes(t *testing.T) {
	k1 := TransientReservationKey(111, "/repo/ws")
	k2 := TransientReservationKey(222, "/repo/ws")
	k3 := TransientReservationKey(111, "/repo/other")

	if !strings.HasPrefix(k1, "transient-") {
		t.Fatalf("key %q lacks transient prefix", k1)
	}
	if k1 == k2 {
		t.Fatal("same root under different owners minted the same key")
	}
	if k1 == k3 {
		t.Fatal("different roots under the same owner minted the same key")
	}
	// The key must satisfy the registry's own ID validation or Reserve
	// would reject every transient hold.
	for _, k := range []string{k1, k2, k3} {
		if !validReservationID(k) {
			t.Fatalf("key %q fails reservation ID validation", k)
		}
	}
}

func TestTransientReservationKey_NormalizesRoot(t *testing.T) {
	// Symlink-equivalent spellings of one workspace must mint one key —
	// the same workspace degrading in two instances shares its hold.
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	a := TransientReservationKey(7, dir)
	b := TransientReservationKey(7, link)
	if a != b {
		t.Fatalf("normalized-root key drift: %q != %q", a, b)
	}
}

func TestTransientOwnerPID_RoundTrip(t *testing.T) {
	key := TransientReservationKey(4242, "/root")
	pid, ok := transientOwnerPID(key)
	if !ok || pid != 4242 {
		t.Fatalf("transientOwnerPID(%q) = %d,%v want 4242,true", key, pid, ok)
	}
	for _, id := range []string{"", "durable-id", "transient-", "transient-abc-x", "transient-0-ff"} {
		if _, ok := transientOwnerPID(id); ok {
			t.Fatalf("transientOwnerPID(%q) reported ok for a non-owner key", id)
		}
	}
}

// stubTransientOwnerAlive pins liveness for the named PIDs only — every
// other PID reads as dead.
func stubTransientOwnerAlive(t *testing.T, live ...int) {
	t.Helper()
	orig := transientOwnerAlive
	transientOwnerAlive = func(pid int) bool {
		for _, p := range live {
			if pid == p {
				return true
			}
		}
		return false
	}
	t.Cleanup(func() { transientOwnerAlive = orig })
}

func TestReserve_SweepsDeadOwnerTransientHolds(t *testing.T) {
	store := newPortStore(t, t.TempDir())

	// Both owners read live while the holds mint — the sweep must not eat
	// the seed it is checking on its own write.
	stubTransientOwnerAlive(t, 999, 111)
	if _, _, err := store.Reserve(TransientReservationKey(999, "/gone"), 6200, 10); err != nil {
		t.Fatalf("seed dead-owner hold: %v", err)
	}
	if _, _, err := store.Reserve(TransientReservationKey(111, "/alive"), 6200, 10); err != nil {
		t.Fatalf("seed live-owner hold: %v", err)
	}
	// Owner 999 dies: only 111 still reports alive.
	transientOwnerAlive = func(pid int) bool { return pid == 111 }

	base, end, err := store.Reserve("durable-ws", 6200, 10)
	if err != nil {
		t.Fatalf("durable reserve: %v", err)
	}
	// The dead owner's 6200-6209 hold swept, so the mint reclaims it; the
	// live owner's 6210-6219 hold must be preserved.
	if base != 6200 || end != 6209 {
		t.Fatalf("mint = %d-%d, want reclaimed 6200-6209", base, end)
	}
	snap, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap[TransientReservationKey(999, "/gone")]; ok {
		t.Fatal("dead-owner hold survived the sweep")
	}
	if _, ok := snap[TransientReservationKey(111, "/alive")]; !ok {
		t.Fatal("live-owner hold was swept")
	}
}

func TestReserve_IdempotentReplaySkipsSweepSideEffects(t *testing.T) {
	store := newPortStore(t, t.TempDir())
	// 999 reads live for the seed mint so its own sweep keeps the hold.
	stubTransientOwnerAlive(t, 999)
	if _, _, err := store.Reserve(TransientReservationKey(999, "/ws"), 6200, 10); err != nil {
		t.Fatal(err)
	}
	// Re-reserving an existing ID returns early without a write — a read-
	// only replay must not mutate the file even when the stub now reports
	// the owner dead (the sweep is a mint-side concern only).
	transientOwnerAlive = func(int) bool { return false }
	base, _, err := store.Reserve(TransientReservationKey(999, "/ws"), 6200, 10)
	if err != nil || base != 6200 {
		t.Fatalf("re-reserve = %d,%v want 6200,nil", base, err)
	}
	snap, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap[TransientReservationKey(999, "/ws")]; !ok {
		t.Fatal("idempotent replay deleted the replayed hold")
	}
}

func TestTransientOwnerAlive_RealPIDs(t *testing.T) {
	// The current process is alive; an implausibly large PID cannot exist
	// on any supported platform.
	if !transientOwnerAlive(os.Getpid()) {
		t.Fatal("own PID reported dead")
	}
	if transientOwnerAlive(999999999) {
		t.Fatal("implausibly large PID reported alive")
	}
}

func TestIsTransientReservationID(t *testing.T) {
	if !IsTransientReservationID(TransientReservationKey(1, "/r")) {
		t.Fatal("transient key not recognized")
	}
	for _, id := range []string{"", "ws-1", fmt.Sprintf("xtransient-%d", 1)} {
		if IsTransientReservationID(id) {
			t.Fatalf("%q misclassified as transient", id)
		}
	}
}
