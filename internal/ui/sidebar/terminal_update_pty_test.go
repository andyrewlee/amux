package sidebar

import (
	"testing"

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
		overflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryCSI},
	}
	m.tabsByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	_ = m.handlePTYStopped(messages.SidebarPTYStopped{
		WorkspaceID: wsID,
		TabID:       string(tabID),
	})

	if state.overflowTrimCarry != (vterm.ParserCarryState{Mode: vterm.ParserCarryCSI}) {
		t.Fatalf("expected overflowTrimCarry preserved on PTY stop, got %+v", state.overflowTrimCarry)
	}

	_ = m.handlePTYOutput(messages.SidebarPTYOutput{
		WorkspaceID: wsID,
		TabID:       string(tabID),
		Data:        []byte("31mHello"),
	})
	if string(state.pendingOutput) != "Hello" {
		t.Fatalf("expected post-stop continuation to trim to visible text, got %q", state.pendingOutput)
	}
}

func TestHandlePTYRestart_PreservesOverflowTrimCarry(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := TerminalTabID("term-tab-1")
	state := &TerminalState{
		overflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryCSI},
	}
	m.tabsByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	_ = m.handlePTYRestart(messages.SidebarPTYRestart{
		WorkspaceID: wsID,
		TabID:       string(tabID),
	})

	if state.overflowTrimCarry != (vterm.ParserCarryState{Mode: vterm.ParserCarryCSI}) {
		t.Fatalf("expected overflowTrimCarry preserved on PTY restart, got %+v", state.overflowTrimCarry)
	}

	_ = m.handlePTYOutput(messages.SidebarPTYOutput{
		WorkspaceID: wsID,
		TabID:       string(tabID),
		Data:        []byte("31mHello"),
	})
	if string(state.pendingOutput) != "Hello" {
		t.Fatalf("expected post-restart continuation to trim to visible text, got %q", state.pendingOutput)
	}
}

func TestHandlePTYStopped_TrimsSecondaryDAContinuationAfterEscapeCarry(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := TerminalTabID("term-tab-da-stop")
	state := &TerminalState{
		overflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryEscape},
	}
	m.tabsByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	_ = m.handlePTYStopped(messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID)})
	_ = m.handlePTYOutput(messages.SidebarPTYOutput{
		WorkspaceID: wsID,
		TabID:       string(tabID),
		Data:        []byte("[>1;10;0cvisible"),
	})

	if string(state.pendingOutput) != "visible" {
		t.Fatalf("expected secondary DA continuation trimmed after stop, got %q", state.pendingOutput)
	}
}

func TestHandlePTYRestart_TrimsSecondaryDAContinuationAfterEscapeCarry(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := TerminalTabID("term-tab-da-restart")
	state := &TerminalState{
		overflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryEscape},
	}
	m.tabsByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	_ = m.handlePTYRestart(messages.SidebarPTYRestart{WorkspaceID: wsID, TabID: string(tabID)})
	_ = m.handlePTYOutput(messages.SidebarPTYOutput{
		WorkspaceID: wsID,
		TabID:       string(tabID),
		Data:        []byte("[>1;10;0cvisible"),
	})

	if string(state.pendingOutput) != "visible" {
		t.Fatalf("expected secondary DA continuation trimmed after restart, got %q", state.pendingOutput)
	}
}
