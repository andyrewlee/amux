package app

import "testing"

// TestWorkspaceServiceIsDeleteInFlight verifies the predicate is nil-safe (so a
// service constructed without it — as the rescan tests do — treats every
// workspace as not in flight) and consults the wired function when present.
func TestWorkspaceServiceIsDeleteInFlight(t *testing.T) {
	var nilService *workspaceService
	if nilService.isDeleteInFlight("ws") {
		t.Fatal("nil service should report not in flight")
	}

	unwired := &workspaceService{}
	if unwired.isDeleteInFlight("ws") {
		t.Fatal("nil predicate should report not in flight")
	}

	wired := &workspaceService{}
	var got string
	wired.deleteInFlight = func(id string) bool {
		got = id
		return id == "deleting"
	}
	if !wired.isDeleteInFlight("deleting") {
		t.Fatal("wired predicate should report the deleting workspace in flight")
	}
	if got != "deleting" {
		t.Fatalf("predicate received %q, want \"deleting\"", got)
	}
	if wired.isDeleteInFlight("other") {
		t.Fatal("wired predicate should report a non-deleting workspace not in flight")
	}
}

func TestWorkspaceServiceRunUnlessDeleteInFlight(t *testing.T) {
	unwired := &workspaceService{}
	ran := false
	if !unwired.runUnlessDeleteInFlight("ws", func() { ran = true }) {
		t.Fatal("unwired service should run the callback")
	}
	if !ran {
		t.Fatal("unwired service did not run callback")
	}

	predicateOnly := &workspaceService{
		deleteInFlight: func(id string) bool { return id == "deleting" },
	}
	if predicateOnly.runUnlessDeleteInFlight("deleting", func() {
		t.Fatal("predicate-only service should not run callback for deleting workspace")
	}) {
		t.Fatal("predicate-only service should report skipped callback")
	}

	guarded := &workspaceService{}
	var gotID string
	guarded.deleteInFlightGuard = func(id string, fn func()) bool {
		gotID = id
		if id == "blocked" {
			return false
		}
		fn()
		return true
	}
	if guarded.runUnlessDeleteInFlight("blocked", func() {
		t.Fatal("guard should not run blocked callback")
	}) {
		t.Fatal("guard should report skipped callback")
	}
	if gotID != "blocked" {
		t.Fatalf("guard received %q, want \"blocked\"", gotID)
	}
	ran = false
	if !guarded.runUnlessDeleteInFlight("open", func() { ran = true }) {
		t.Fatal("guard should run open callback")
	}
	if !ran {
		t.Fatal("guard did not run open callback")
	}
}
