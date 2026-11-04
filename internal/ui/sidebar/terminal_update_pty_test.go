package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/ui/ptyio"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestHandlePTYStopped_PreservesOverflowTrimCarry(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := TerminalTabID("term-tab-1")
	state := &TerminalState{
		State: ptyio.State{
			OverflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryCSI},
		},
	}
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	_ = m.handlePTYStopped(messages.SidebarPTYStopped{
		WorkspaceID: wsID,
		TabID:       string(tabID),
	})

	if state.OverflowTrimCarry != (vterm.ParserCarryState{Mode: vterm.ParserCarryCSI}) {
		t.Fatalf("expected overflowTrimCarry preserved on PTY stop, got %+v", state.OverflowTrimCarry)
	}

	_ = m.handlePTYOutput(messages.SidebarPTYOutput{
		WorkspaceID: wsID,
		TabID:       string(tabID),
		Data:        []byte("31mHello"),
	})
	if string(state.PendingOutput) != "Hello" {
		t.Fatalf("expected post-stop continuation to trim to visible text, got %q", state.PendingOutput)
	}
}

func TestHandlePTYRestart_PreservesOverflowTrimCarry(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := TerminalTabID("term-tab-1")
	state := &TerminalState{
		State: ptyio.State{
			OverflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryCSI},
		},
	}
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	_ = m.handlePTYRestart(messages.SidebarPTYRestart{
		WorkspaceID: wsID,
		TabID:       string(tabID),
	})

	if state.OverflowTrimCarry != (vterm.ParserCarryState{Mode: vterm.ParserCarryCSI}) {
		t.Fatalf("expected overflowTrimCarry preserved on PTY restart, got %+v", state.OverflowTrimCarry)
	}

	_ = m.handlePTYOutput(messages.SidebarPTYOutput{
		WorkspaceID: wsID,
		TabID:       string(tabID),
		Data:        []byte("31mHello"),
	})
	if string(state.PendingOutput) != "Hello" {
		t.Fatalf("expected post-restart continuation to trim to visible text, got %q", state.PendingOutput)
	}
}

func TestHandlePTYStopped_TrimsSecondaryDAContinuationAfterEscapeCarry(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := TerminalTabID("term-tab-da-stop")
	state := &TerminalState{
		State: ptyio.State{
			OverflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryEscape},
		},
	}
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	_ = m.handlePTYStopped(messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID)})
	_ = m.handlePTYOutput(messages.SidebarPTYOutput{
		WorkspaceID: wsID,
		TabID:       string(tabID),
		Data:        []byte("[>1;10;0cvisible"),
	})

	if string(state.PendingOutput) != "visible" {
		t.Fatalf("expected secondary DA continuation trimmed after stop, got %q", state.PendingOutput)
	}
}

func TestHandlePTYRestart_TrimsSecondaryDAContinuationAfterEscapeCarry(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := TerminalTabID("term-tab-da-restart")
	state := &TerminalState{
		State: ptyio.State{
			OverflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryEscape},
		},
	}
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	_ = m.handlePTYRestart(messages.SidebarPTYRestart{WorkspaceID: wsID, TabID: string(tabID)})
	_ = m.handlePTYOutput(messages.SidebarPTYOutput{
		WorkspaceID: wsID,
		TabID:       string(tabID),
		Data:        []byte("[>1;10;0cvisible"),
	})

	if string(state.PendingOutput) != "visible" {
		t.Fatalf("expected secondary DA continuation trimmed after restart, got %q", state.PendingOutput)
	}
}

// liveTerminalState returns a TerminalState whose Terminal is a zero-value
// *pty.Terminal. A freshly constructed terminal has closed==false, so
// IsClosed() reports false and handlePTYStopped takes the termAlive
// restart-backoff branch. This is the same fake the attach tests rely on.
func liveTerminalState() *TerminalState {
	return &TerminalState{
		Terminal: &pty.Terminal{},
		tab:      tabRunning,
	}
}

func TestHandlePTYStopped_RestartBackoffSchedulesTickAndGrows(t *testing.T) {
	if (&pty.Terminal{}).IsClosed() {
		t.Fatal("precondition: a zero-value *pty.Terminal must report IsClosed()==false")
	}

	m := NewTerminalModel()
	wsID := "ws-restart"
	tabID := TerminalTabID("term-tab-restart")
	state := liveTerminalState()
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	// Each of the first ptyio.PtyRestartMax calls must schedule a restart tick and
	// leave the terminal attached (not detached). Backoff must grow each time
	// until it reaches the cap.
	lastBackoff := -1 // sentinel below any real backoff
	for i := 0; i < ptyio.PtyRestartMax; i++ {
		cmd := m.handlePTYStopped(messages.SidebarPTYStopped{
			WorkspaceID: wsID,
			TabID:       string(tabID),
		})
		if cmd == nil {
			t.Fatalf("call %d: expected a non-nil restart tick cmd while under the limit", i+1)
		}
		if state.tab != tabRunning {
			t.Fatalf("call %d: expected the tab to stay running while restarting, got %v", i+1, state.tab)
		}
		got := int(state.RestartBackoff)
		if got <= 0 {
			t.Fatalf("call %d: expected positive backoff, got %d", i+1, got)
		}
		// Backoff doubles until it saturates at the cap, so it must be
		// non-decreasing across calls.
		if got < lastBackoff {
			t.Fatalf("call %d: expected backoff to grow or hold, got %d after %d", i+1, got, lastBackoff)
		}
		lastBackoff = got
	}
}

func TestHandlePTYStopped_DetachesAfterRestartLimit(t *testing.T) {
	m := NewTerminalModel()
	wsID := "ws-restart-limit"
	tabID := TerminalTabID("term-tab-restart-limit")
	state := liveTerminalState()
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	// Exhaust the restart budget; these all schedule ticks.
	for i := 0; i < ptyio.PtyRestartMax; i++ {
		if cmd := m.handlePTYStopped(messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID)}); cmd == nil {
			t.Fatalf("call %d: expected restart tick while under the limit", i+1)
		}
	}
	if state.tab != tabRunning {
		t.Fatal("expected terminal to remain attached up to the restart limit")
	}

	// One more stop within the window exceeds ptyio.PtyRestartMax: the FSM gives up
	// and marks the terminal detached with no restart scheduled.
	cmd := m.handlePTYStopped(messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID)})
	if cmd != nil {
		t.Fatal("expected nil cmd once the restart limit is exceeded")
	}
	if state.tab != tabDetachedAuto {
		t.Fatalf("expected the automatic give-up detach state after exceeding the restart limit (not user-initiated), got %v", state.tab)
	}
}

// TestHandlePTYStopped_GiveUpDuringReattachInvalidatesAttempt pins the
// wedge fix: a stopped-reader message can be processed after a reattach
// attempt began (StopReader never bumps ReaderGen), and the give-up detach
// must invalidate that attempt — otherwise the attempt stays current and
// its late result accepts a terminal the detached tab can never run,
// wedging the tab detached-but-armed until the stall sweep fires.
func TestHandlePTYStopped_GiveUpDuringReattachInvalidatesAttempt(t *testing.T) {
	m := NewTerminalModel()
	wsID := "ws-giveup-reattach"
	tabID := TerminalTabID("term-giveup-reattach")
	state := liveTerminalState()
	originalTerm := state.Terminal
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	// Exhaust the restart budget while running, then give up: the tab
	// lands detached-auto, from which a reattach attempt may begin.
	for i := 0; i < ptyio.PtyRestartMax; i++ {
		if cmd := m.handlePTYStopped(messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID)}); cmd == nil {
			t.Fatalf("call %d: expected restart tick while under the limit", i+1)
		}
	}
	if cmd := m.handlePTYStopped(messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID)}); cmd != nil {
		t.Fatal("expected nil cmd once the restart limit is exceeded")
	}
	epoch := uint64(1)
	withMu(&state.mu, func() {
		if state.tab != tabDetachedAuto {
			t.Fatalf("expected detached-auto after give-up, got %v", state.tab)
		}
		if !state.beginReattachLocked() {
			t.Fatal("beginReattachLocked must arm from the detached state")
		}
		epoch = state.reattachEpoch
	})

	// A second stop arrives while the attempt is in flight (a stopped-reader
	// message can outlive a detach: StopReader never bumps ReaderGen). The
	// give-up detach must invalidate the attempt, not just step the tab.
	if cmd := m.handlePTYStopped(messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID)}); cmd != nil {
		t.Fatal("expected nil cmd once the restart limit is exceeded")
	}
	withMu(&state.mu, func() {
		if state.tab != tabDetachedAuto {
			t.Fatalf("expected detached-auto after give-up during attach, got %v", state.tab)
		}
		if state.Reattach.InFlight {
			t.Fatal("the give-up detach must invalidate the in-flight attempt")
		}
		if state.reattachAttemptCurrentLocked(epoch) {
			t.Fatal("the late result's epoch must be stale after invalidation")
		}
	})

	// The attempt's own late result must be rejected as stale, leaving the
	// tab retryably detached instead of accepting an unusable terminal.
	m.handleReattachResult(SidebarTerminalReattachResult{
		WorkspaceID: wsID,
		TabID:       tabID,
		Epoch:       epoch,
		Terminal:    &pty.Terminal{},
	})
	withMu(&state.mu, func() {
		if state.tab != tabDetachedAuto {
			t.Fatalf("late result must not move the detached tab, got %v", state.tab)
		}
		if state.Reattach.InFlight {
			t.Fatal("late result must not re-arm the released guard")
		}
		if state.Terminal != originalTerm {
			t.Fatal("late result must not install its terminal on a detached tab")
		}
	})
}
