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

func TestStateWatcher_ReasonChangeResetsPendingPaths(t *testing.T) {
	var mu sync.Mutex
	var gotReason string
	var gotPaths []string
	fireCount := 0

	// fire captures sw.fire, armed by the injected timer; invoking it once drives
	// the coalesced debounce without sleeping past a real timer.
	var fire func()

	sw := &stateWatcher{
		debounce: 50 * time.Millisecond,
		onChanged: func(reason string, paths []string) {
			mu.Lock()
			gotReason = reason
			gotPaths = paths
			fireCount++
			mu.Unlock()
		},
	}
	sw.newTimer = func(_ time.Duration, f func()) debounceTimer {
		fire = f
		return fakeTimer{}
	}

	// Schedule a "registry" event with a path.
	sw.scheduleNotify("registry", "/path/to/registry.json")

	// Before the timer fires, schedule a "workspaces" event with a different path.
	sw.scheduleNotify("workspaces", "/path/to/workspace.json")

	// Drive the debounce deterministically. The two scheduleNotify calls must
	// coalesce into a single fire (the second Resets the armed timer, it does not
	// arm a second one), so invoking the captured callback once suffices.
	if fire == nil {
		t.Fatal("expected the debounce timer to be armed after scheduleNotify")
	}
	fire()

	mu.Lock()
	defer mu.Unlock()

	if fireCount != 1 {
		t.Fatalf("debounce fired %d times, want 1 (coalesced events)", fireCount)
	}
	if gotReason != "workspaces" {
		t.Fatalf("reason = %q, want %q", gotReason, "workspaces")
	}
	// The registry path should have been discarded when the reason changed.
	for _, p := range gotPaths {
		if p == "/path/to/registry.json" {
			t.Fatal("expected registry path to be discarded when reason changed to workspaces")
		}
	}
	if len(gotPaths) != 1 || gotPaths[0] != "/path/to/workspace.json" {
		t.Fatalf("paths = %v, want [/path/to/workspace.json]", gotPaths)
	}
}
