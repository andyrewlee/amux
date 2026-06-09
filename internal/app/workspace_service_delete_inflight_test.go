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
