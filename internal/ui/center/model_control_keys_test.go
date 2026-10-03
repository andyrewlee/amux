package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestCenterControlKeyReservations pins the pane-level split between the
// encoder's new control bytes and the center's reserved shortcuts: Ctrl+]
// still cycles tabs (the encoder never sees it), while Ctrl+Q is not a
// center reservation — it must fall through to the terminal as 0x11.
func TestCenterControlKeyReservations(t *testing.T) {
	ws := newTestWorkspace("ws", "/repo/ws")
	m, _, wsID := newActionsModel(t, chatTab(ws, "a"), chatTab(ws, "b"))

	_, cmd, handled := m.handleTerminalCtrlKey(
		tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl},
		m.tabs.ByWorkspace[wsID][0],
	)
	if !handled {
		t.Fatal("ctrl+] must be handled by the center reservation")
	}
	if got := m.tabs.ActiveByWorkspace[wsID]; got != 1 {
		t.Fatalf("ctrl+] active index = %d, want 1", got)
	}
	if cmd == nil {
		t.Fatal("ctrl+] with a changed selection must return a selection cmd")
	}

	before := m.tabs.ActiveByWorkspace[wsID]
	_, cmd, handled = m.handleTerminalCtrlKey(
		tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl},
		m.tabs.ByWorkspace[wsID][before],
	)
	if handled {
		t.Fatal("ctrl+q is not a center reservation — it must reach the terminal")
	}
	if got := m.tabs.ActiveByWorkspace[wsID]; got != before {
		t.Fatalf("ctrl+q changed selection to %d", got)
	}
	if cmd != nil {
		t.Fatal("ctrl+q must not produce a selection cmd")
	}
}
