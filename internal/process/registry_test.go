package process

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testRecord(root string, pid int) ServiceRecord {
	return ServiceRecord{
		WorkspaceRoot: root,
		PID:           pid,
		PGID:          pid,
		Command:       "sh -c pnpm run dev",
		StartedAt:     time.Now(),
	}
}

func TestServiceRegistryPersistsAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service-registry.json")
	reg := NewServiceRegistry(path)
	if err := reg.Record(testRecord("/ws/a", 123)); err != nil {
		t.Fatalf("Record: %v", err)
	}

	reloaded := NewServiceRegistry(path)
	rec, ok := reloaded.Get("/ws/a")
	if !ok || rec.PID != 123 || rec.Command != "sh -c pnpm run dev" {
		t.Fatalf("expected persisted record, got %+v ok=%v", rec, ok)
	}
}

func TestServiceRegistryClearGuardsPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service-registry.json")
	reg := NewServiceRegistry(path)
	if err := reg.Record(testRecord("/ws/a", 123)); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// A stale clear (older pid) must not drop the newer record.
	if err := reg.Clear("/ws/a", 999); err != nil {
		t.Fatalf("Clear mismatched: %v", err)
	}
	if _, ok := reg.Get("/ws/a"); !ok {
		t.Fatal("mismatched-pid clear dropped the record")
	}

	if err := reg.Clear("/ws/a", 123); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, ok := reg.Get("/ws/a"); ok {
		t.Fatal("matching clear left the record behind")
	}
}

func TestServiceRegistryCorruptFileDegradesToEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service-registry.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := NewServiceRegistry(path)
	if got := reg.Records(); len(got) != 0 {
		t.Fatalf("expected empty registry, got %+v", got)
	}
	// And it must still accept writes.
	if err := reg.Record(testRecord("/ws/a", 1)); err != nil {
		t.Fatalf("Record after corrupt load: %v", err)
	}
}

func TestServiceRecordMatches(t *testing.T) {
	rec := testRecord("/ws/a", 100)
	live := []ProcessInfo{{PID: 100, PGID: 100, Command: "sh -c pnpm run dev"}}
	if !rec.Matches(live) {
		t.Error("expected identity match")
	}
	recycled := []ProcessInfo{{PID: 100, PGID: 100, Command: "totally different"}}
	if rec.Matches(recycled) {
		t.Error("recycled PID with different command must not match")
	}
	regrouped := []ProcessInfo{{PID: 100, PGID: 42, Command: "sh -c pnpm run dev"}}
	if rec.Matches(regrouped) {
		t.Error("changed process group must not match")
	}
	if rec.Matches(nil) {
		t.Error("dead process must not match")
	}
}

func TestServiceRecordMatchesByStartTime(t *testing.T) {
	rec := testRecord("/ws/a", 100)
	// The shell's exec optimization replaces the command line but never the
	// start time: identity must hold on start time alone.
	execReplaced := []ProcessInfo{{
		PID: 100, PGID: 100,
		Command:   "node /ws/a/node_modules/.bin/dev-server",
		StartedAt: rec.StartedAt.Add(2 * time.Second),
	}}
	if !rec.Matches(execReplaced) {
		t.Error("exec-replaced command with matching start time must match")
	}
	// A recycled PID running the identical generic command must NOT match:
	// start time is the identity when both sides know it.
	recycledSameCommand := []ProcessInfo{{
		PID: 100, PGID: 100,
		Command:   rec.Command,
		StartedAt: rec.StartedAt.Add(time.Hour),
	}}
	if rec.Matches(recycledSameCommand) {
		t.Error("recycled PID with same command but different start time must not match")
	}
}

func TestServiceRegistryReconcileDropsDead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service-registry.json")
	reg := NewServiceRegistry(path)
	if err := reg.Record(testRecord("/ws/live", 100)); err != nil {
		t.Fatal(err)
	}
	// Old enough to be outside the reconcile grace window.
	dead := testRecord("/ws/dead", 200)
	dead.StartedAt = time.Now().Add(-time.Hour)
	if err := reg.Record(dead); err != nil {
		t.Fatal(err)
	}

	snap := []ProcessInfo{{PID: 100, PGID: 100, Command: "sh -c pnpm run dev"}}
	live := reg.Reconcile(snap)
	if len(live) != 1 || live[0].WorkspaceRoot != "/ws/live" {
		t.Fatalf("expected only /ws/live to survive, got %+v", live)
	}
	// The drop must be durable.
	reloaded := NewServiceRegistry(path)
	if _, ok := reloaded.Get("/ws/dead"); ok {
		t.Error("dead record survived reconcile on disk")
	}
}

func TestServiceRegistryReconcileGraceKeepsFreshRecords(t *testing.T) {
	// A record created while the snapshot was being taken is legitimately
	// absent from it and must not be dropped.
	reg := NewServiceRegistry(filepath.Join(t.TempDir(), "service-registry.json"))
	if err := reg.Record(testRecord("/ws/fresh", 300)); err != nil {
		t.Fatal(err)
	}
	live := reg.Reconcile(nil)
	if len(live) != 1 || live[0].WorkspaceRoot != "/ws/fresh" {
		t.Fatalf("expected fresh record kept through reconcile, got %+v", live)
	}
	if _, ok := reg.Get("/ws/fresh"); !ok {
		t.Error("fresh record dropped by reconcile despite grace window")
	}
}
