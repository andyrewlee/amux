package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	appPty "github.com/andyrewlee/amux/internal/pty"
)

// newClosedPTYTab spawns a real PTY-backed terminal and closes it so the next
// SendString fails deterministically (a closed terminal's write returns
// io.ErrClosedPipe). Same technique as the actor-path failure tests.
func newClosedPTYTab(t *testing.T, id TabID) *Tab {
	t.Helper()
	term, err := appPty.NewWithSize("cat >/dev/null", t.TempDir(), nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("close terminal: %v", err)
	}
	return &Tab{
		ID:        id,
		Assistant: "bash",
		Agent:     &appPty.Agent{Terminal: term},
		Running:   true,
	}
}

// TestDirectSendToTerminal_ClosedTerminalDetachesAndNotifies covers the
// failure branch of directSendToTerminal: a dead PTY must mark the tab
// detached and return a command that yields TabInputFailed, instead of
// leaving the tab "running" while silently dropping input.
func TestDirectSendToTerminal_ClosedTerminalDetachesAndNotifies(t *testing.T) {
	m := newTestModel()
	tabID := TabID("tab-direct-fail")
	tab := newClosedPTYTab(t, tabID)

	_, sent, cmd := m.directSendToTerminal(tab, "x", "Direct input")

	if sent {
		t.Fatal("expected sent=false when SendString fails on a closed terminal")
	}

	tab.mu.Lock()
	detached := tab.Detached
	running := tab.Running
	tab.mu.Unlock()
	if !detached {
		t.Fatal("expected tab.Detached=true after direct-send failure")
	}
	if running {
		t.Fatal("expected tab.Running=false after direct-send failure")
	}

	if cmd == nil {
		t.Fatal("expected a failure command from directSendToTerminal")
	}
	failed, ok := cmd().(TabInputFailed)
	if !ok {
		t.Fatalf("expected TabInputFailed, got %T", cmd())
	}
	if failed.TabID != tabID {
		t.Errorf("TabInputFailed.TabID = %q, want %q", failed.TabID, tabID)
	}
	if failed.Err == nil {
		t.Error("expected TabInputFailed.Err to be set")
	}
}

// TestSendKeyToTerminal_DirectSendFailureDetaches drives a keystroke through
// sendKeyToTerminal while the tab actor is NOT ready, so the key genuinely
// takes the direct fallback path (isTabActorReady()==false), not the actor
// queue. With a dead PTY the direct write fails: the tab must detach and the
// returned command must yield TabInputFailed.
func TestSendKeyToTerminal_DirectSendFailureDetaches(t *testing.T) {
	m := newTestModel()
	// Precondition proving the direct path is taken: a freshly constructed
	// model has never started its tab actor, so readiness is false and
	// sendKeyToTerminal skips the actor queue entirely.
	if m.isTabActorReady() {
		t.Fatal("precondition: tab actor must not be ready so the key takes the direct send path")
	}

	tabID := TabID("tab-key-direct-fail")
	tab := newClosedPTYTab(t, tabID)

	_, cmd := m.sendKeyToTerminal(tea.KeyPressMsg{Code: 'x', Text: "x"}, tab)

	tab.mu.Lock()
	detached := tab.Detached
	running := tab.Running
	tab.mu.Unlock()
	if !detached {
		t.Fatal("expected tab.Detached=true after direct key-send failure")
	}
	if running {
		t.Fatal("expected tab.Running=false after direct key-send failure")
	}

	// On the failure halt path sendKeyToTerminal returns the error command
	// directly (no activity-tag batch), so cmd() is the TabInputFailed itself.
	if cmd == nil {
		t.Fatal("expected a failure command from sendKeyToTerminal")
	}
	failed, ok := cmd().(TabInputFailed)
	if !ok {
		t.Fatalf("expected TabInputFailed, got %T", cmd())
	}
	if failed.TabID != tabID {
		t.Errorf("TabInputFailed.TabID = %q, want %q", failed.TabID, tabID)
	}
	if failed.Err == nil {
		t.Error("expected TabInputFailed.Err to be set")
	}
}
