package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestEmitDashboardStateCmd_EnqueuesBell verifies the connector that wires a
// dashboard state-update command (the opt-in agent-done bell) to the runtime's
// external-message pump: a non-nil command's message is enqueued; a nil command
// is a no-op.
func TestEmitDashboardStateCmd_EnqueuesBell(t *testing.T) {
	a := &App{externalMsgs: make(chan tea.Msg, 4)}

	a.emitDashboardStateCmd(nil)
	select {
	case msg := <-a.externalMsgs:
		t.Fatalf("nil command must enqueue nothing, got %#v", msg)
	default:
	}

	bell := tea.Raw("\a")
	a.emitDashboardStateCmd(bell)
	select {
	case msg := <-a.externalMsgs:
		if _, ok := msg.(tea.RawMsg); !ok {
			t.Fatalf("expected a tea.RawMsg (the bell) enqueued, got %#v", msg)
		}
	default:
		t.Fatal("bell command's message was not enqueued to the external pump")
	}
}
