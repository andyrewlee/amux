package sidebar

import (
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// beginAttachAttempt stamps a genuine in-flight attach attempt on ts and
// returns its epoch. Fixtures that deliver a SidebarTerminalReattachResult or
// SidebarTerminalReattachFailed must begin the attempt they are answering —
// production never produces an outcome for an attempt that was not begun.
// Not t.Parallel-safe alongside other begin/mutation of the same state.
func beginAttachAttempt(t *testing.T, ts *TerminalState) uint64 {
	t.Helper()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !ts.beginReattachLocked() {
		t.Fatal("fixture: attach attempt already in flight")
	}
	return ts.reattachEpoch
}

// invalidateAttachAttempt mirrors the detach/teardown invalidation so tests
// can supersede an in-flight attempt without running a whole lifecycle.
func invalidateAttachAttempt(t *testing.T, ts *TerminalState) {
	t.Helper()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.invalidateReattachLocked()
}

// attachAttemptFixture builds a model with one detached tab that has an
// in-flight attach attempt, returning the state and that attempt's epoch.
func attachAttemptFixture(t *testing.T) (*TerminalModel, *data.Workspace, TerminalTabID, *TerminalState, uint64) {
	t.Helper()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	state := &TerminalState{
		SessionName: "session-1",
		Detached:    true,
	}
	epoch := beginAttachAttempt(t, state)
	m := NewTerminalModel()
	m.workspace = ws
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, Name: "Terminal 1", State: state}}
	m.tabs.ActiveByWorkspace[wsID] = 0
	return m, ws, tabID, state, epoch
}

func reattachResult(ws *data.Workspace, tabID TerminalTabID, epoch uint64, term *pty.Terminal) SidebarTerminalReattachResult {
	return SidebarTerminalReattachResult{
		WorkspaceID: string(ws.ID()),
		TabID:       tabID,
		Epoch:       epoch,
		Terminal:    term,
		SessionName: "session-1",
	}
}

// A slow attempt whose lock was released and retried must not overwrite the
// newer attachment when its own client finally arrives.
func TestSidebarReattachGeneration_OldSuccessAfterNewerSuccess(t *testing.T) {
	m, ws, tabID, state, epoch1 := attachAttemptFixture(t)

	// Supersede attempt 1 and complete attempt 2.
	invalidateAttachAttempt(t, state)
	state.mu.Lock()
	if !state.beginReattachLocked() {
		state.mu.Unlock()
		t.Fatal("second attempt should begin after invalidation")
	}
	epoch2 := state.reattachEpoch
	state.mu.Unlock()

	term2 := &pty.Terminal{}
	_, _ = m.Update(reattachResult(ws, tabID, epoch2, term2))

	state.mu.Lock()
	if state.Terminal != term2 || !state.Running {
		state.mu.Unlock()
		t.Fatal("expected the newer attempt's terminal to be applied")
	}
	state.mu.Unlock()

	// Attempt 1's late success must be rejected and its client closed.
	term1 := &pty.Terminal{}
	_, _ = m.Update(reattachResult(ws, tabID, epoch1, term1))

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.Terminal != term2 {
		t.Fatal("stale success overwrote the newer terminal")
	}
	if !term1.IsClosed() {
		t.Fatal("stale success leaked its orphaned client")
	}
	if term2.IsClosed() {
		t.Fatal("the accepted terminal must not be closed by a stale result")
	}
}

// A stale attempt's failure must not mark the newer attachment stopped and
// must not emit a failure toast for an attempt that is no longer current.
func TestSidebarReattachGeneration_OldFailureAfterNewerSuccess(t *testing.T) {
	m, ws, tabID, state, epoch1 := attachAttemptFixture(t)

	invalidateAttachAttempt(t, state)
	state.mu.Lock()
	if !state.beginReattachLocked() {
		state.mu.Unlock()
		t.Fatal("second attempt should begin after invalidation")
	}
	epoch2 := state.reattachEpoch
	state.mu.Unlock()

	_, _ = m.Update(reattachResult(ws, tabID, epoch2, &pty.Terminal{}))

	cmd := m.handleReattachFailed(SidebarTerminalReattachFailed{
		WorkspaceID: string(ws.ID()),
		TabID:       tabID,
		Epoch:       epoch1,
		Err:         errors.New("stale failure"),
	})
	if cmd != nil {
		t.Fatal("stale failure must not emit a toast")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.Running || state.Terminal == nil {
		t.Fatal("stale failure marked the newer attachment stopped")
	}
}

// A user detach while an attach is in flight invalidates the attempt: the
// late success is rejected and its client closed rather than resurrecting a
// terminal the user explicitly detached.
func TestSidebarReattachGeneration_UserDetachWhilePending(t *testing.T) {
	m, ws, tabID, state, epoch := attachAttemptFixture(t)

	m.DetachActiveTab()

	term := &pty.Terminal{}
	_, _ = m.Update(reattachResult(ws, tabID, epoch, term))

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.Terminal != nil || state.Running {
		t.Fatal("late success resurrected a user-detached tab")
	}
	if !state.Detached || !state.UserDetached {
		t.Fatal("user detach intent must be retained")
	}
	if !term.IsClosed() {
		t.Fatal("the rejected client must be closed")
	}
}

// A duplicate delivery of the accepted result must not close the terminal it
// carries — the pointer is now the live client.
func TestSidebarReattachGeneration_DuplicateSuccessKeepsAcceptedClient(t *testing.T) {
	m, ws, tabID, state, epoch := attachAttemptFixture(t)

	term := &pty.Terminal{}
	msg := reattachResult(ws, tabID, epoch, term)
	_, _ = m.Update(msg)
	_, _ = m.Update(msg)

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.Terminal != term || !state.Running {
		t.Fatal("expected the accepted terminal to stay current")
	}
	if term.IsClosed() {
		t.Fatal("duplicate delivery closed the accepted client")
	}
}

// A stalled attempt is invalidated by the sweep, so a late success is
// rejected even before any retry — and the retry's own result applies.
func TestSidebarReattachGeneration_StallInvalidatesThenRetryApplies(t *testing.T) {
	m, ws, tabID, state, epoch1 := attachAttemptFixture(t)

	state.mu.Lock()
	state.Reattach.StartedAt = time.Now().Add(-2 * ptyio.ReattachStallTimeout)
	state.mu.Unlock()

	m.SweepStalledReattaches()

	state.mu.Lock()
	if state.Reattach.InFlight {
		state.mu.Unlock()
		t.Fatal("expected the sweep to release the stalled lock")
	}
	state.mu.Unlock()

	// The abandoned attempt's late success is stale the moment the sweep
	// released it — before any retry exists.
	stale := &pty.Terminal{}
	_, _ = m.Update(reattachResult(ws, tabID, epoch1, stale))
	if !stale.IsClosed() {
		t.Fatal("swept attempt's late client must be closed")
	}

	// The next attach sweep begins a fresh attempt that can still succeed.
	if _, ok := shouldAttachExistingTerminalTab(m.getTabByID(string(ws.ID()), tabID)); !ok {
		t.Fatal("swept terminal should be eligible for a retry")
	}
	state.mu.Lock()
	epoch2 := state.reattachEpoch
	state.mu.Unlock()

	term2 := &pty.Terminal{}
	_, _ = m.Update(reattachResult(ws, tabID, epoch2, term2))
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.Terminal != term2 || !state.Running {
		t.Fatal("expected the retry attempt's result to apply")
	}
}

// A second manual reattach while one is in flight coalesces to an
// informational toast instead of dispatching a competing attach command.
func TestSidebarReattachGeneration_DuplicateManualReattachToasts(t *testing.T) {
	m, _, _, state, _ := attachAttemptFixture(t)

	cmd := m.ReattachActiveTab()
	if cmd == nil {
		t.Fatal("expected the duplicate reattach to produce a toast")
	}
	msg, ok := cmd().(messages.Toast)
	if !ok {
		t.Fatalf("expected a toast for the duplicate reattach, got %T", cmd())
	}
	if msg.Level != messages.ToastInfo {
		t.Fatalf("expected info-level toast, got %v", msg.Level)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.Reattach.InFlight {
		t.Fatal("the in-flight attempt must still own the lock")
	}
}

// Restart invalidates the in-flight attach it replaces: the abandoned
// attempt's result is rejected instead of overwriting the restarted terminal.
func TestSidebarReattachGeneration_RestartInvalidatesPending(t *testing.T) {
	m, ws, tabID, state, epoch1 := attachAttemptFixture(t)

	// RestartActiveTab kills the named tmux session (harmless failure for a
	// nonexistent one) and returns the attach cmd, which is not run here.
	cmd := m.RestartActiveTab()
	if cmd == nil {
		t.Fatal("expected restart to dispatch a fresh attach")
	}
	state.mu.Lock()
	epoch2 := state.reattachEpoch
	inFlight := state.Reattach.InFlight
	state.mu.Unlock()
	if !inFlight || epoch2 == epoch1 {
		t.Fatal("restart must begin a new attempt invalidating the old one")
	}

	stale := &pty.Terminal{}
	_, _ = m.Update(reattachResult(ws, tabID, epoch1, stale))
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.Terminal == stale || !stale.IsClosed() {
		t.Fatal("pre-restart attach result must be rejected and closed")
	}
}

// A current-attempt success carrying no terminal is a failed attach: it
// releases the attempt and surfaces a failure rather than leaving the tab
// running or wedged in-flight.
func TestSidebarReattachGeneration_NilTerminalSuccessIsFailure(t *testing.T) {
	m, ws, tabID, state, epoch := attachAttemptFixture(t)

	cmd := m.handleReattachResult(reattachResult(ws, tabID, epoch, nil))
	if cmd == nil {
		t.Fatal("nil-terminal success must surface a failure")
	}
	if _, ok := cmd().(messages.Toast); !ok {
		t.Fatalf("expected a failure toast, got %T", cmd())
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.Running || state.Reattach.InFlight {
		t.Fatal("nil-terminal success must release the attempt, not mark running")
	}
}

// Teardown invalidates a pending attempt even though the tab stays filed:
// the late result is rejected and its client closed.
func TestSidebarReattachGeneration_TeardownRejectsPendingResult(t *testing.T) {
	m, ws, tabID, state, epoch := attachAttemptFixture(t)

	m.teardownTabState(state, "test teardown")

	incoming := &pty.Terminal{}
	_, _ = m.Update(reattachResult(ws, tabID, epoch, incoming))
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.Terminal == incoming {
		t.Fatal("teardown must not accept a pending attach result")
	}
	if !incoming.IsClosed() {
		t.Fatal("the rejected client must be closed after teardown")
	}
}

// An attempt survives workspace rebind because TabID/TerminalState identity
// persists: a result stamped with the pre-rebind workspace ID still applies.
func TestSidebarReattachGeneration_AttemptSurvivesWorkspaceRebind(t *testing.T) {
	m, ws, tabID, state, epoch := attachAttemptFixture(t)

	oldID := string(ws.ID())
	newID := "ws-rebound"
	m.tabs.ByWorkspace[newID] = m.tabs.ByWorkspace[oldID]
	delete(m.tabs.ByWorkspace, oldID)

	term := &pty.Terminal{}
	_, _ = m.Update(SidebarTerminalReattachResult{
		WorkspaceID: oldID,
		TabID:       tabID,
		Epoch:       epoch,
		Terminal:    term,
		SessionName: "session-1",
	})
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.Terminal != term || !state.Running {
		t.Fatal("a current attempt's result must apply across workspace rebind")
	}
}

// Automatic attach must keep honoring user-detach intent and must not begin
// a second attempt over a live terminal.
func TestSidebarReattachGeneration_AutoAttachGates(t *testing.T) {
	m, ws, tabID, state, _ := attachAttemptFixture(t)
	_ = m
	tab := &TerminalTab{ID: tabID, State: state}

	// Already in flight: refused without touching the epoch.
	before := state.reattachEpoch
	if _, ok := shouldAttachExistingTerminalTab(tab); ok {
		t.Fatal("auto attach must not start while an attempt is in flight")
	}
	if state.reattachEpoch != before {
		t.Fatal("refused auto attach must not advance the epoch")
	}

	// User-detached: refused.
	invalidateAttachAttempt(t, state)
	state.mu.Lock()
	state.UserDetached = true
	state.mu.Unlock()
	if _, ok := shouldAttachExistingTerminalTab(tab); ok {
		t.Fatal("auto attach must honor UserDetached")
	}
	_ = ws
}
