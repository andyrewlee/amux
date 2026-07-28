package sidebar

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

func inFlightTerminalTab(id TerminalTabID, sessionName string) *TerminalTab {
	tab := &TerminalTab{
		ID:   id,
		Name: "Terminal 1",
		State: &TerminalState{
			SessionName: sessionName,
			Detached:    true,
		},
	}
	tab.State.beginReattachLocked()
	return tab
}

// An attach outcome stamped with a workspace ID the tab is no longer filed
// under must still reach the tab. Routing by workspace alone dropped it, and
// because shouldAttachExistingTerminalTab refuses to retry while the reattach
// lock is held, the terminal could never attach again.
func TestSidebarResolveTabForResultFallsBackToTabID(t *testing.T) {
	m := &TerminalModel{}
	m.tabs.ByWorkspace = map[string][]*TerminalTab{}
	tab := inFlightTerminalTab(TerminalTabID("term-tab-drift"), "sess-drift")
	m.tabs.ByWorkspace["real-ws"] = []*TerminalTab{tab}

	found, wsID := m.resolveTabForResult("stale-ws", tab.ID, "test")
	if found != tab {
		t.Fatalf("expected the tab to be found despite the workspace mismatch, got %v", found)
	}
	if wsID != "real-ws" {
		t.Fatalf("expected the key the tab is filed under, got %q", wsID)
	}

	if missing, _ := m.resolveTabForResult("stale-ws", TerminalTabID("term-tab-missing"), "test"); missing != nil {
		t.Fatal("expected a genuinely absent tab to resolve to nil")
	}
}

func TestSidebarSweepStalledReattachesReleasesOnlyStaleLocks(t *testing.T) {
	m := &TerminalModel{}
	m.tabs.ByWorkspace = map[string][]*TerminalTab{}

	stalled := inFlightTerminalTab(TerminalTabID("term-tab-stalled"), "sess-stalled")
	stalled.State.reattachStartedAt = time.Now().Add(-2 * ptyio.ReattachStallTimeout)

	fresh := inFlightTerminalTab(TerminalTabID("term-tab-fresh"), "sess-fresh")

	running := inFlightTerminalTab(TerminalTabID("term-tab-running"), "sess-running")
	running.State.reattachStartedAt = time.Now().Add(-2 * ptyio.ReattachStallTimeout)
	running.State.Running = true

	m.tabs.ByWorkspace["ws"] = []*TerminalTab{stalled, fresh, running}

	m.SweepStalledReattaches()

	for _, tc := range []struct {
		name         string
		tab          *TerminalTab
		wantInFlight bool
	}{
		{"stalled", stalled, false},
		{"fresh", fresh, true},
		{"running", running, true},
	} {
		tc.tab.State.mu.Lock()
		got := tc.tab.State.reattachInFlight
		tc.tab.State.mu.Unlock()
		if got != tc.wantInFlight {
			t.Fatalf("%s: reattachInFlight = %v, want %v", tc.name, got, tc.wantInFlight)
		}
	}

	// A released terminal must become eligible for the next attach sweep,
	// which is how it recovers without the user doing anything.
	if !shouldAttachExistingTerminalTab(stalled) {
		t.Fatal("expected the swept terminal to be eligible for reattach")
	}
	if shouldAttachExistingTerminalTab(fresh) {
		t.Fatal("expected a terminal still within the timeout to stay locked")
	}
}

// An unstamped lock is timed from the first sweep rather than released
// immediately, so an attach in progress is never cut short.
func TestSidebarSweepStampsUntimedLock(t *testing.T) {
	m := &TerminalModel{}
	m.tabs.ByWorkspace = map[string][]*TerminalTab{}
	tab := inFlightTerminalTab(TerminalTabID("term-tab-unstamped"), "sess-unstamped")
	tab.State.reattachStartedAt = time.Time{}
	m.tabs.ByWorkspace["ws"] = []*TerminalTab{tab}

	m.SweepStalledReattaches()

	tab.State.mu.Lock()
	defer tab.State.mu.Unlock()
	if !tab.State.reattachInFlight {
		t.Fatal("expected the lock to survive the stamping sweep")
	}
	if tab.State.reattachStartedAt.IsZero() {
		t.Fatal("expected the sweep to stamp the lock so a later sweep can time it")
	}
}
