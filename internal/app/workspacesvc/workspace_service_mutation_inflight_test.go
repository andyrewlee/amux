package workspacesvc

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// TestWorkspaceServiceIsMutationInFlight verifies the predicate is nil-safe (so a
// service constructed without it — as the rescan tests do — treats every
// workspace as not in flight) and consults the wired function when present.
func TestWorkspaceServiceIsMutationInFlight(t *testing.T) {
	var nilService *Service
	if nilService.isMutationInFlight(&data.Workspace{Root: "/r/ws"}) {
		t.Fatal("nil service should report not in flight")
	}

	unwired := &Service{}
	if unwired.isMutationInFlight(&data.Workspace{Root: "/r/ws"}) {
		t.Fatal("nil predicate should report not in flight")
	}

	wired := &Service{}
	var got *data.Workspace
	wired.mutationInFlight = func(ws *data.Workspace) bool {
		got = ws
		return ws.Root == "/r/mutating"
	}
	if !wired.isMutationInFlight(&data.Workspace{Root: "/r/mutating"}) {
		t.Fatal("wired predicate should report the mutating workspace in flight")
	}
	if got == nil || got.Root != "/r/mutating" {
		t.Fatalf("predicate received %v, want the workspace value", got)
	}
	if wired.isMutationInFlight(&data.Workspace{Root: "/r/other"}) {
		t.Fatal("wired predicate should report a non-mutating workspace not in flight")
	}
}

func TestWorkspaceServiceRunUnlessMutationInFlight(t *testing.T) {
	unwired := &Service{}
	ran := false
	if !unwired.runUnlessMutationInFlight(&data.Workspace{Root: "/r/ws"}, func() { ran = true }) {
		t.Fatal("unwired service should run the callback")
	}
	if !ran {
		t.Fatal("unwired service did not run callback")
	}

	predicateOnly := &Service{
		mutationInFlight: func(ws *data.Workspace) bool { return ws.Root == "/r/mutating" },
	}
	if predicateOnly.runUnlessMutationInFlight(&data.Workspace{Root: "/r/mutating"}, func() {
		t.Fatal("predicate-only service should not run callback for mutating workspace")
	}) {
		t.Fatal("predicate-only service should report skipped callback")
	}

	guarded := &Service{}
	var gotWS *data.Workspace
	guarded.mutationInFlightGuard = func(ws *data.Workspace, fn func()) bool {
		gotWS = ws
		if ws.Root == "/r/blocked" {
			return false
		}
		fn()
		return true
	}
	if guarded.runUnlessMutationInFlight(&data.Workspace{Root: "/r/blocked"}, func() {
		t.Fatal("guard should not run blocked callback")
	}) {
		t.Fatal("guard should report skipped callback")
	}
	if gotWS == nil || gotWS.Root != "/r/blocked" {
		t.Fatalf("guard received %v, want the blocked workspace", gotWS)
	}
	ran = false
	if !guarded.runUnlessMutationInFlight(&data.Workspace{Root: "/r/open"}, func() { ran = true }) {
		t.Fatal("guard should run open callback")
	}
	if !ran {
		t.Fatal("guard did not run open callback")
	}
}
