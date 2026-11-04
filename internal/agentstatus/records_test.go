package agentstatus

import "testing"

func rep(state, id string) Report {
	r := Report{State: state, ID: id}
	if state == StateBlocked {
		r.Kind = KindQuestion
	}
	r.Progress = progressAbsent
	return r
}

// TestRecordSetReplaceAndClear pins the protocol's record semantics: every
// report replaces its record completely, and clear removes the addressed
// record plus its descendants.
func TestRecordSetReplaceAndClear(t *testing.T) {
	rs := NewRecordSet()
	rs.Apply(rep(StateWorking, RootID))
	rs.Apply(Report{State: StateWorking, ID: "build", App: "deploy"})
	rs.Apply(Report{State: StateBlocked, ID: "build/test", Kind: KindPermission, Progress: progressAbsent})

	// A worker's blocked state does not replace the root's working state:
	// both records exist at once.
	if rec, _ := rs.Fold(); rec.State != StateBlocked || rec.ID != "build/test" {
		t.Fatalf("fold must surface the blocked child, got %#v", rec)
	}

	// Replacing build/test with done leaves the root untouched.
	rs.Apply(Report{State: StateDone, ID: "build/test", Progress: progressAbsent})
	if len(rs.records) != 3 {
		t.Fatalf("records = %d, want 3", len(rs.records))
	}

	// Clearing build removes build and build/test, keeps the root.
	if !rs.Apply(rep(StateClear, "build")) {
		t.Fatal("clear of an existing record must report change")
	}
	if len(rs.records) != 1 {
		t.Fatalf("records after clear = %d, want 1", len(rs.records))
	}
	if rec, ok := rs.Fold(); !ok || rec.ID != RootID {
		t.Fatalf("root record must survive, got %#v ok=%v", rec, ok)
	}

	// Clearing with no id removes everything.
	if !rs.Apply(rep(StateClear, RootID)) {
		t.Fatal("root clear must report change")
	}
	if _, ok := rs.Fold(); ok || len(rs.records) != 0 {
		t.Fatal("root clear must empty the set")
	}

	// Clearing an unknown id is a no-op, not an error.
	if rs.Apply(rep(StateClear, "ghost")) {
		t.Fatal("clear of unknown id must not report change")
	}
}

// TestRecordSetFoldPresentation pins the fold's presentation rules:
// severity first, root wins ties, newest wins identical-class ties.
func TestRecordSetFoldPresentation(t *testing.T) {
	rs := NewRecordSet()
	rs.Apply(Report{State: StateIdle, ID: RootID, App: "agent", Progress: progressAbsent})
	rs.Apply(Report{State: StateWorking, ID: "w1", Progress: 10})
	rs.Apply(Report{State: StateWorking, ID: "w2", Progress: 20})
	if rec, _ := rs.Fold(); rec.ID != "w2" {
		t.Fatalf("newest working record should win the tie, got %#v", rec)
	}
	// A root record of equal severity outranks workers.
	rs.Apply(Report{State: StateWorking, ID: RootID, App: "agent", Progress: progressAbsent})
	if rec, _ := rs.Fold(); rec.ID != RootID {
		t.Fatalf("root must win an equal-severity tie, got %#v", rec)
	}
	// Error outranks working; blocked outranks everything.
	rs.Apply(Report{State: StateError, ID: "w1", Progress: progressAbsent})
	if rec, _ := rs.Fold(); rec.State != StateError {
		t.Fatalf("error must outrank working, got %#v", rec)
	}
	rs.Apply(Report{State: StateBlocked, ID: "w2", Kind: KindAuth, Progress: progressAbsent})
	if rec, _ := rs.Fold(); rec.State != StateBlocked || rec.Kind != KindAuth {
		t.Fatalf("blocked must outrank error, got %#v", rec)
	}
}

// TestRecordSetDropTransient pins the protocol lifetime: prompt/exit drops
// working and blocked records, keeps done, error, and idle.
func TestRecordSetDropTransient(t *testing.T) {
	rs := NewRecordSet()
	rs.Apply(rep(StateWorking, RootID))
	rs.Apply(rep(StateBlocked, "child"))
	rs.Apply(rep(StateDone, "keeper"))
	rs.Apply(rep(StateError, "failed"))
	rs.Apply(rep(StateIdle, "resting"))
	if !rs.DropTransient() {
		t.Fatal("dropping present transient records must report change")
	}
	remaining := map[string]bool{}
	for id := range rs.records {
		remaining[id] = true
	}
	for _, id := range []string{RootID, "child"} {
		if remaining[id] {
			t.Fatalf("transient record %q survived", id)
		}
	}
	for _, id := range []string{"keeper", "failed", "resting"} {
		if !remaining[id] {
			t.Fatalf("durable record %q was dropped", id)
		}
	}
	if rs.DropTransient() {
		t.Fatal("second drop with nothing transient must not report change")
	}
}

// TestRecordSetEviction pins the cap: least-recently-updated goes first.
func TestRecordSetEviction(t *testing.T) {
	rs := NewRecordSet()
	for i := 0; i < MaxRecordsPerPane+5; i++ {
		rs.Apply(Report{State: StateWorking, ID: "r" + string(rune('a'+i%26)) + itoa(i), Progress: progressAbsent})
	}
	if len(rs.records) != MaxRecordsPerPane {
		t.Fatalf("records = %d, want cap %d", len(rs.records), MaxRecordsPerPane)
	}
	// The very first record (least recently updated) must be gone.
	if _, ok := rs.records["ra0"]; ok {
		t.Fatal("oldest record must have been evicted")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestRecordSetRepeatReportNoChange pins the change signal: an identical
// re-report refreshes recency but does not report a change, while a new
// state for the same record does.
func TestRecordSetRepeatReportNoChange(t *testing.T) {
	rs := NewRecordSet()
	rs.Apply(rep(StateWorking, RootID))
	if rs.Apply(rep(StateWorking, RootID)) {
		t.Fatal("identical re-report must not report change")
	}
	if !rs.Apply(rep(StateIdle, RootID)) {
		t.Fatal("a new state for the same record is a change")
	}
}
