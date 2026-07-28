package center

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// drainMsgs runs a command and flattens any batch it produces into messages.
func drainMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	out := make([]tea.Msg, 0, len(batch))
	for _, c := range batch {
		out = append(out, drainMsgs(t, c)...)
	}
	return out
}

func inFlightTab(id TabID, sessionName string) *Tab {
	return &Tab{
		ID:               id,
		Assistant:        "claude",
		SessionName:      sessionName,
		Running:          false,
		Detached:         true,
		reattachInFlight: true,
	}
}

// A reattach outcome stamped with a workspace ID the tab is no longer filed
// under must still reach the tab. Routing it by workspace alone used to drop
// the result, leaving the tab pinned in the reattaching state permanently.
func TestReattachFailedResolvesTabAcrossWorkspaceDrift(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := inFlightTab(TabID("tab-drift"), "sess-drift")
	tab.Workspace = ws
	m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{tab}

	_, cmd := m.updatePtyTabReattachFailed(ptyTabReattachFailed{
		WorkspaceID: "some-other-workspace-id",
		TabID:       tab.ID,
		Err:         errors.New("boom"),
		Action:      "reattach",
	})

	tab.mu.Lock()
	inFlight := tab.reattachInFlight
	tab.mu.Unlock()
	if inFlight {
		t.Fatalf("expected the reattach lock to be released despite the workspace mismatch")
	}

	var sawStateChange bool
	for _, msg := range drainMsgs(t, cmd) {
		if changed, ok := msg.(messages.TabStateChanged); ok {
			sawStateChange = true
			// Follow-ups must carry the key the tab is actually filed under,
			// not the stale one the result arrived with.
			if changed.WorkspaceID != string(ws.ID()) {
				t.Fatalf("state change carried workspace %q, want %q", changed.WorkspaceID, string(ws.ID()))
			}
		}
	}
	if !sawStateChange {
		t.Fatal("expected a TabStateChanged follow-up")
	}
}

// A success message that carries no agent cannot attach anything; it must still
// release the lock so the tab reads as detached and can be retried.
func TestReattachResultWithoutAgentReleasesLock(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := inFlightTab(TabID("tab-noagent"), "sess-noagent")
	tab.Workspace = ws
	m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{tab}

	m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID: string(ws.ID()),
		TabID:       tab.ID,
	})

	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.reattachInFlight {
		t.Fatal("expected the reattach lock to be released when the result carries no agent")
	}
	if !tab.Detached {
		t.Fatal("expected the tab to read as detached so the user can retry")
	}
}

func TestSweepStalledReattachesReleasesOnlyStaleLocks(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	stalled := inFlightTab(TabID("tab-stalled"), "sess-stalled")
	stalled.reattachStartedAt = time.Now().Add(-2 * ptyio.ReattachStallTimeout)

	fresh := inFlightTab(TabID("tab-fresh"), "sess-fresh")
	fresh.reattachStartedAt = time.Now()

	// Running tabs hold no meaningful lock even if the flag lingers.
	running := inFlightTab(TabID("tab-running"), "sess-running")
	running.reattachStartedAt = time.Now().Add(-2 * ptyio.ReattachStallTimeout)
	running.Running = true
	running.Detached = false

	for _, tab := range []*Tab{stalled, fresh, running} {
		tab.Workspace = ws
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{stalled, fresh, running}

	cmd := m.SweepStalledReattaches()
	if cmd == nil {
		t.Fatal("expected the sweep to report the stalled tab")
	}

	stalled.mu.Lock()
	stalledInFlight := stalled.reattachInFlight
	stalled.mu.Unlock()
	if stalledInFlight {
		t.Fatal("expected the stalled reattach lock to be released")
	}

	fresh.mu.Lock()
	freshInFlight := fresh.reattachInFlight
	fresh.mu.Unlock()
	if !freshInFlight {
		t.Fatal("expected a reattach still within the timeout to be left alone")
	}

	running.mu.Lock()
	runningInFlight := running.reattachInFlight
	running.mu.Unlock()
	if !runningInFlight {
		t.Fatal("expected a running tab to be skipped by the sweep")
	}

	var sawToast bool
	for _, msg := range drainMsgs(t, cmd) {
		if _, ok := msg.(messages.Toast); ok {
			sawToast = true
		}
	}
	if !sawToast {
		t.Fatal("expected the user to be told the reattach timed out")
	}
}

// An unstamped lock is timed from the first sweep rather than released
// immediately, so a reattach in progress is never cut short.
func TestSweepStalledReattachesStampsUntimedLock(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := inFlightTab(TabID("tab-unstamped"), "sess-unstamped")
	tab.Workspace = ws
	m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{tab}

	if cmd := m.SweepStalledReattaches(); cmd != nil {
		t.Fatal("expected an unstamped lock to be timed, not released")
	}

	tab.mu.Lock()
	defer tab.mu.Unlock()
	if !tab.reattachInFlight {
		t.Fatal("expected the lock to survive the stamping sweep")
	}
	if tab.reattachStartedAt.IsZero() {
		t.Fatal("expected the sweep to stamp the lock so a later sweep can time it")
	}
}

// The stall sweep makes retries possible while an earlier attempt may still be
// running, so a result from the superseded attempt must not install its agent
// over the newer one's.
func TestSupersededReattachResultIsDropped(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := &Tab{ID: TabID("tab-superseded"), Assistant: "claude", Workspace: ws, Detached: true}
	m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{tab}

	// Attempt 1 stalls and is swept; attempt 2 starts.
	tab.mu.Lock()
	_ = tab.beginReattachLocked()
	staleEpoch := tab.reattachEpochLocked()
	tab.markReattachFailedLocked(false)
	_ = tab.beginReattachLocked()
	liveEpoch := tab.reattachEpochLocked()
	tab.mu.Unlock()
	if staleEpoch == liveEpoch {
		t.Fatal("expected each acquisition to get its own epoch")
	}

	// Attempt 1 finally returns.
	m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID: string(ws.ID()),
		TabID:       tab.ID,
		Epoch:       staleEpoch,
		Agent:       &appPty.Agent{},
	})

	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Agent != nil {
		t.Fatal("expected the superseded result's agent to be dropped, not installed")
	}
	if !tab.reattachInFlight {
		t.Fatal("expected the live attempt to keep its reattach lock")
	}
}

// A failure from a superseded attempt must not release the live attempt's lock.
func TestSupersededReattachFailureIsIgnored(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := &Tab{ID: TabID("tab-superseded-fail"), Assistant: "claude", Workspace: ws, Detached: true}
	m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{tab}

	tab.mu.Lock()
	_ = tab.beginReattachLocked()
	staleEpoch := tab.reattachEpochLocked()
	tab.markReattachFailedLocked(false)
	_ = tab.beginReattachLocked()
	tab.mu.Unlock()

	_, cmd := m.updatePtyTabReattachFailed(ptyTabReattachFailed{
		WorkspaceID: string(ws.ID()),
		TabID:       tab.ID,
		Epoch:       staleEpoch,
		Err:         errors.New("stale boom"),
	})
	if cmd != nil {
		t.Fatal("expected a superseded failure to be ignored entirely")
	}

	tab.mu.Lock()
	defer tab.mu.Unlock()
	if !tab.reattachInFlight {
		t.Fatal("expected the live attempt to keep its reattach lock")
	}
}

// Outcomes that predate epoch tracking carry epoch 0 and must still apply, so
// the guard can only reject a message it can positively identify as stale.
func TestZeroEpochResultStillApplies(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := inFlightTab(TabID("tab-zero-epoch"), "sess-zero")
	tab.Workspace = ws
	m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{tab}

	if superseded, _ := m.reattachSuperseded(tab, 0); superseded {
		t.Fatal("expected a zero epoch to be treated as untracked, not stale")
	}
}

func TestBeginReattachStampsAcquisition(t *testing.T) {
	tab := &Tab{ID: TabID("tab-stamp")}
	if !tab.beginReattachLocked() {
		t.Fatal("expected to acquire the reattach lock")
	}
	if tab.reattachStartedAt.IsZero() {
		t.Fatal("expected the acquisition to be stamped")
	}
}
