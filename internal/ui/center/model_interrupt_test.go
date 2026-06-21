package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	appPty "github.com/andyrewlee/amux/internal/pty"
)

// TestCenterCtrlCRoutesToAgentInterrupt locks in the fix for AGT-07: a live
// Ctrl-C in the center pane must be intercepted by the ctrl-key handler and
// routed through the per-agent interrupt (which honors InterruptCount /
// InterruptDelayMs), NOT forwarded as a single raw 0x03 byte.
//
// Regression guard: previously ctrl+c fell through to the raw key-forward path,
// so an agent that needs more than one Ctrl-C within a short window (e.g. Claude:
// InterruptCount 2, 200ms apart) could not be interrupted with one keystroke.
func TestCenterCtrlCRoutesToAgentInterrupt(t *testing.T) {
	m := newTestModel()
	term, err := appPty.New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("new terminal: %v", err)
	}
	defer func() { _ = term.Close() }()

	tab := &Tab{Agent: &appPty.Agent{
		Terminal: term,
		Config:   config.AssistantConfig{InterruptCount: 2, InterruptDelayMs: 1},
	}}

	_, cmd, handled := m.handleTerminalCtrlKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, tab)
	if !handled {
		t.Fatal("ctrl+c must be intercepted by the ctrl-key handler, not forwarded as a raw byte")
	}
	if cmd == nil {
		t.Fatal("ctrl+c must produce an interrupt command for an agent tab")
	}
}

// TestInterruptActiveAgentCmdNilSafe guards the helper against tabs without a
// live agent (detached/placeholder) and a model without an agent manager.
func TestInterruptActiveAgentCmdNilSafe(t *testing.T) {
	m := newTestModel()
	if cmd := m.interruptActiveAgentCmd(&Tab{Agent: nil}); cmd != nil {
		t.Fatal("interrupt on a tab with no agent must be a no-op (nil cmd)")
	}
}
