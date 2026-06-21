package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/vterm"
)

// TestApplyActorWriteLocked_DrainsPendingClipboard verifies the OSC 52 drain
// seam: after feeding an OSC 52 sequence through applyActorWriteLocked (under
// the lock, as the production path does), the second call to
// TakePendingClipboard returns nil — proving the write path already drained it.
func TestApplyActorWriteLocked_DrainsPendingClipboard(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	term := vterm.New(40, 5)
	tab := &Tab{
		Assistant: "test-agent",
		Workspace: ws,
		Terminal:  term,
	}
	m.AddTab(tab)

	// OSC 52 write: "\x1b]52;c;aGk=\x07" sets clipboard to "hi"
	osc52 := []byte("\x1b]52;c;aGk=\x07")
	ev := tabEvent{
		tab:    tab,
		output: osc52,
	}
	processedBytes := len(osc52)

	tab.mu.Lock()
	_, _, _, _, _, _, pendingClip := m.applyActorWriteLocked(tab, ev, processedBytes)
	tab.mu.Unlock()

	if len(pendingClip) == 0 {
		t.Fatal("expected applyActorWriteLocked to return non-empty pendingClip for OSC 52 write")
	}
	if string(pendingClip) != "hi" {
		t.Fatalf("expected clipboard payload %q, got %q", "hi", string(pendingClip))
	}

	// Second take must return nil — the production path already drained it.
	if second := tab.Terminal.TakePendingClipboard(); second != nil {
		t.Fatalf("expected second TakePendingClipboard to return nil after drain, got %q", string(second))
	}
}

// TestApplyActorWriteLocked_NilClipboardWhenNoOSC52 verifies that plain text
// output results in a nil pendingClip return — no spurious clipboard writes.
func TestApplyActorWriteLocked_NilClipboardWhenNoOSC52(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	term := vterm.New(40, 5)
	tab := &Tab{
		Assistant: "test-agent",
		Workspace: ws,
		Terminal:  term,
	}
	m.AddTab(tab)

	plain := []byte("hello world\n")
	ev := tabEvent{tab: tab, output: plain}

	tab.mu.Lock()
	_, _, _, _, _, _, pendingClip := m.applyActorWriteLocked(tab, ev, len(plain))
	tab.mu.Unlock()

	if pendingClip != nil {
		t.Fatalf("expected nil pendingClip for plain text output, got %q", string(pendingClip))
	}
}
