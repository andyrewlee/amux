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
		Running:  true,
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

	// Each of the first ptyRestartMax calls must schedule a restart tick and
	// leave the terminal attached (not detached). Backoff must grow each time
	// until it reaches the cap.
	lastBackoff := -1 // sentinel below any real backoff
	for i := 0; i < ptyRestartMax; i++ {
		cmd := m.handlePTYStopped(messages.SidebarPTYStopped{
			WorkspaceID: wsID,
			TabID:       string(tabID),
		})
		if cmd == nil {
			t.Fatalf("call %d: expected a non-nil restart tick cmd while under the limit", i+1)
		}
		if state.Detached {
			t.Fatalf("call %d: expected Detached==false while restarting, got true", i+1)
		}
		if !state.Running {
			t.Fatalf("call %d: expected Running to stay true while restarting", i+1)
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
	for i := 0; i < ptyRestartMax; i++ {
		if cmd := m.handlePTYStopped(messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID)}); cmd == nil {
			t.Fatalf("call %d: expected restart tick while under the limit", i+1)
		}
	}
	if state.Detached {
		t.Fatal("expected terminal to remain attached up to the restart limit")
	}

	// One more stop within the window exceeds ptyRestartMax: the FSM gives up
	// and marks the terminal detached with no restart scheduled.
	cmd := m.handlePTYStopped(messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID)})
	if cmd != nil {
		t.Fatal("expected nil cmd once the restart limit is exceeded")
	}
	if !state.Detached {
		t.Fatal("expected Detached==true after exceeding the restart limit")
	}
	if state.Running {
		t.Fatal("expected Running==false after exceeding the restart limit")
	}
	if state.UserDetached {
		t.Fatal("expected UserDetached==false (give-up detach is not user-initiated)")
	}
}
