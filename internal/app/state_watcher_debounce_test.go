package app

import (
	"sync"
	"testing"
	"time"
)

// fakeTimer is a no-op debounceTimer used to drive stateWatcher's debounce
// deterministically. It never fires on its own; the test invokes the callback
// captured from newTimer, so the debounce is decoupled from wall-clock time.
type fakeTimer struct{}

func (fakeTimer) Reset(time.Duration) bool { return true }
func (fakeTimer) Stop() bool               { return true }

func TestStateWatcher_MixedReasonsPreserveAllPaths(t *testing.T) {
	var mu sync.Mutex
	type notification struct {
		reason string
		paths  []string
	}
	var got []notification

	// fire captures sw.fire, armed by the injected timer; invoking it once drives
	// the coalesced debounce without sleeping past a real timer.
	var fire func()

	sw := &stateWatcher{
		debounce: 50 * time.Millisecond,
		onChanged: func(reason string, paths []string) {
			mu.Lock()
			got = append(got, notification{reason, append([]string(nil), paths...)})
			mu.Unlock()
		},
	}
	sw.newTimer = func(_ time.Duration, f func()) debounceTimer {
		fire = f
		return fakeTimer{}
	}

	// Schedule a "workspaces" event, then a different-reason "registry" event,
	// then a second "workspaces" event — all inside one debounce window.
	sw.scheduleNotify("workspaces", "/path/to/workspace-a.json")
	sw.scheduleNotify("registry", "/path/to/registry.json")
	sw.scheduleNotify("workspaces", "/path/to/workspace-b.json")

	// Drive the debounce deterministically. The scheduleNotify calls coalesce
	// into a single fire (each later call Resets the armed timer), so invoking
	// the captured callback once suffices.
	if fire == nil {
		t.Fatal("expected the debounce timer to be armed after scheduleNotify")
	}
	fire()

	mu.Lock()
	defer mu.Unlock()

	// Debounce coalesces within a reason, never across reasons: one emission
	// per reason, in sorted order for determinism, and the registry paths must
	// not be discarded by the interleaved workspaces events.
	if len(got) != 2 {
		t.Fatalf("emissions = %d, want 2 (one per reason): %+v", len(got), got)
	}
	if got[0].reason != "registry" ||
		len(got[0].paths) != 1 ||
		got[0].paths[0] != "/path/to/registry.json" {
		t.Fatalf("registry emission = %+v, want one path /path/to/registry.json", got[0])
	}
	if got[1].reason != "workspaces" ||
		len(got[1].paths) != 2 ||
		got[1].paths[0] != "/path/to/workspace-a.json" ||
		got[1].paths[1] != "/path/to/workspace-b.json" {
		t.Fatalf("workspaces emission = %+v, want both workspace paths coalesced", got[1])
	}
}
