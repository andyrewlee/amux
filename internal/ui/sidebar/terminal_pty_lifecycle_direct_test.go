package sidebar

import "testing"

func TestStartPTYReader_DirectPathKeepsHealthyActiveReader(t *testing.T) {
	m := NewTerminalModel()
	wsID := "ws-direct-active"
	tabID := TerminalTabID("tab-direct-active")
	cancel := make(chan struct{})
	ts := &TerminalState{
		Running:      true,
		readerActive: true,
		readerCancel: cancel,
		ptyMsgCh:     nil, // Expected in direct-output mode.
	}
	m.tabsByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: ts}}

	cmd := m.startPTYReader(wsID, tabID)
	if cmd != nil {
		t.Fatal("expected no command when reader is already active")
	}

	if !ts.readerActive {
		t.Fatal("expected readerActive to remain true")
	}
	if ts.readerCancel != cancel {
		t.Fatal("expected existing readerCancel channel to be preserved")
	}
	select {
	case <-cancel:
		t.Fatal("expected existing readerCancel channel to remain open")
	default:
	}
}
