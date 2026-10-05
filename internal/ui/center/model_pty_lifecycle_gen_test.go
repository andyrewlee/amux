package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A PTYStopped stamped with a dead reader's generation must not touch the
// replacement reader: the cancel channel stays open, the restart budget is
// not consumed, and no noise tail is written. Reader R1 exits, a reattach
// bumps ReaderGen and starts R2, then R1's queued PTYStopped arrives — the
// gen fence is what keeps it from closing R2's cancel channel.
func TestUpdatePTYStopped_StaleGenLeavesCurrentReader(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := livePTYTab(TabID("tab-stale-stop"), ws)

	cancel := make(chan struct{})
	tab.State.ReaderGen = 2
	tab.State.ReaderActive = true
	tab.State.ReaderCancel = cancel
	tab.State.MsgCh = make(chan tea.Msg, 1)
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	if cmd := m.updatePTYStopped(PTYStopped{WorkspaceID: wsID, TabID: tab.ID, Gen: 1}); cmd != nil {
		t.Fatalf("stale PTYStopped must produce no work, got %v", drainBatch(cmd))
	}
	select {
	case <-cancel:
		t.Fatal("stale PTYStopped closed the current reader's cancel channel")
	default:
	}
	if !tab.State.ReaderActive {
		t.Fatal("stale PTYStopped cleared ReaderActive on the replacement reader")
	}
	if tab.State.MsgCh == nil {
		t.Fatal("stale PTYStopped cleared the replacement reader's MsgCh")
	}
	if tab.RestartBackoff != 0 {
		t.Fatalf("stale PTYStopped consumed restart budget: backoff %v", tab.RestartBackoff)
	}
	if tab.Detached {
		t.Fatal("stale PTYStopped marked the tab detached")
	}
}

// The matching-gen PTYStopped still takes the normal restart path, and the
// emitted PTYRestart carries the stopped generation so a later stale check
// can fence it.
func TestUpdatePTYStopped_CurrentGenSchedulesRestart(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := livePTYTab(TabID("tab-current-stop"), ws)
	tab.State.ReaderGen = 1
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	cmd := m.updatePTYStopped(PTYStopped{WorkspaceID: wsID, TabID: tab.ID, Gen: 1})
	if cmd == nil {
		t.Fatal("current-gen PTYStopped must schedule a restart tick")
	}
	var restart *PTYRestart
	for _, msg := range drainBatch(cmd) {
		if r, ok := msg.(PTYRestart); ok {
			cp := r
			restart = &cp
		}
	}
	if restart == nil {
		t.Fatal("expected PTYRestart among emitted messages")
	}
	if restart.Gen != 1 {
		t.Fatalf("expected PTYRestart stamped with stopped gen 1, got %d", restart.Gen)
	}
}

// Stale-gen output is the dead reader's trailing bytes — dropping it keeps
// the replacement stream's byte order intact.
func TestUpdatePTYOutput_StaleGenDropped(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := livePTYTab(TabID("tab-stale-output"), ws)
	tab.State.ReaderGen = 2
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	m.updatePTYOutput(PTYOutput{WorkspaceID: wsID, TabID: tab.ID, Gen: 1, Data: []byte("stale-bytes")})
	if len(tab.PendingOutput) != 0 {
		t.Fatalf("stale-gen output must not reach PendingOutput, got %q", tab.PendingOutput)
	}

	m.updatePTYOutput(PTYOutput{WorkspaceID: wsID, TabID: tab.ID, Gen: 2, Data: []byte("fresh-bytes")})
	if string(tab.PendingOutput) != "fresh-bytes" {
		t.Fatalf("current-gen output must reach PendingOutput, got %q", tab.PendingOutput)
	}
}

// A restart request stamped for a superseded generation is moot — a newer
// reader already runs, so the tick must not start yet another one or clear
// the backoff bookkeeping.
func TestUpdatePTYRestart_StaleGenDropped(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := livePTYTab(TabID("tab-stale-restart"), ws)
	tab.State.ReaderGen = 2
	tab.RestartBackoff = 7
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	if cmd := m.updatePTYRestart(PTYRestart{WorkspaceID: wsID, TabID: tab.ID, Gen: 1}); cmd != nil {
		t.Fatalf("stale PTYRestart must produce no work, got %v", drainBatch(cmd))
	}
	if tab.State.ReaderGen != 2 {
		t.Fatalf("stale PTYRestart bumped ReaderGen to %d", tab.State.ReaderGen)
	}
	if tab.RestartBackoff != 7 {
		t.Fatalf("stale PTYRestart touched backoff bookkeeping: %v", tab.RestartBackoff)
	}
}
