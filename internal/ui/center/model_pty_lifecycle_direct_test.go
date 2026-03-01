package center

import (
	"sync/atomic"
	"testing"
)

func TestStartPTYReader_DirectPathKeepsHealthyActiveReader(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := &Tab{
		ID:      TabID("tab-direct-active"),
		Running: true,
	}
	cancel := make(chan struct{})
	tab.readerActive = true
	tab.readerCancel = cancel
	tab.ptyMsgCh = nil // Expected in direct-output mode.
	atomic.StoreUint32(&tab.readerActiveState, 1)

	cmd := m.startPTYReader(string(ws.ID()), tab)
	if cmd != nil {
		t.Fatal("expected no command when reader is already active")
	}

	if !tab.readerActive {
		t.Fatal("expected readerActive to remain true")
	}
	if tab.readerCancel != cancel {
		t.Fatal("expected existing readerCancel channel to be preserved")
	}
	select {
	case <-cancel:
		t.Fatal("expected existing readerCancel channel to remain open")
	default:
	}
}
