package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/ui/ptyio"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
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
