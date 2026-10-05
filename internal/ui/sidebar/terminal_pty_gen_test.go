package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// A SidebarPTYStopped stamped with a dead reader's generation must not touch
// the replacement reader: the cancel channel stays open, the pending drain
// never runs, and the restart budget is not consumed.
func TestHandlePTYStopped_StaleGenLeavesCurrentReader(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := TerminalTabID("term-tab-stale-stop")
	state := liveTerminalState()

	cancel := make(chan struct{})
	state.State.ReaderGen = 2
	state.State.ReaderActive = true
	state.State.ReaderCancel = cancel
	state.PendingOutput = []byte("keep-me")
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	if cmd := m.handlePTYStopped(messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID), Gen: 1}); cmd != nil {
		t.Fatalf("stale SidebarPTYStopped must produce no work, got %v", cmd)
	}
	select {
	case <-cancel:
		t.Fatal("stale SidebarPTYStopped closed the current reader's cancel channel")
	default:
	}
	if !state.State.ReaderActive {
		t.Fatal("stale SidebarPTYStopped cleared ReaderActive on the replacement reader")
	}
	if string(state.PendingOutput) != "keep-me" {
		t.Fatalf("stale SidebarPTYStopped drained pending output: %q", state.PendingOutput)
	}
	if state.RestartBackoff != 0 {
		t.Fatalf("stale SidebarPTYStopped consumed restart budget: backoff %v", state.RestartBackoff)
	}
	if state.Detached {
		t.Fatal("stale SidebarPTYStopped marked the tab detached")
	}
	if !state.Running {
		t.Fatal("stale SidebarPTYStopped stopped the live tab")
	}
}

// The matching-gen SidebarPTYStopped still takes the normal restart path, and
// the emitted SidebarPTYRestart carries the stopped generation.
func TestHandlePTYStopped_CurrentGenSchedulesRestart(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := TerminalTabID("term-tab-current-stop")
	state := liveTerminalState()
	state.State.ReaderGen = 1
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	cmd := m.handlePTYStopped(messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID), Gen: 1})
	if cmd == nil {
		t.Fatal("current-gen SidebarPTYStopped must schedule a restart tick")
	}
	if state.State.ReaderGen != 1 {
		t.Fatalf("ReaderGen must stay 1 until the restart tick runs, got %d", state.State.ReaderGen)
	}
}

// Stale-gen output is the dead reader's trailing bytes — dropping it keeps
// the replacement stream's byte order intact.
func TestHandlePTYOutput_StaleGenDropped(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := TerminalTabID("term-tab-stale-output")
	state := liveTerminalState()
	state.State.ReaderGen = 2
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	m.handlePTYOutput(messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: string(tabID), Gen: 1, Data: []byte("stale-bytes")})
	if len(state.PendingOutput) != 0 {
		t.Fatalf("stale-gen output must not reach PendingOutput, got %q", state.PendingOutput)
	}

	m.handlePTYOutput(messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: string(tabID), Gen: 2, Data: []byte("fresh-bytes")})
	if string(state.PendingOutput) != "fresh-bytes" {
		t.Fatalf("current-gen output must reach PendingOutput, got %q", state.PendingOutput)
	}
}

// A restart request stamped for a superseded generation is moot — a newer
// reader already runs.
func TestHandlePTYRestart_StaleGenDropped(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	tabID := TerminalTabID("term-tab-stale-restart")
	state := liveTerminalState()
	state.State.ReaderGen = 2
	state.RestartBackoff = 7
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: state}}

	if cmd := m.handlePTYRestart(messages.SidebarPTYRestart{WorkspaceID: wsID, TabID: string(tabID), Gen: 1}); cmd != nil {
		t.Fatalf("stale SidebarPTYRestart must produce no work, got %v", cmd)
	}
	if state.State.ReaderGen != 2 {
		t.Fatalf("stale SidebarPTYRestart bumped ReaderGen to %d", state.State.ReaderGen)
	}
	if state.RestartBackoff != 7 {
		t.Fatalf("stale SidebarPTYRestart touched backoff bookkeeping: %v", state.RestartBackoff)
	}
}
