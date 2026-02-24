package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestActivityContract_ReattachBootstrapSuppressedThenRealOutputMarksActive(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := &Tab{
		Assistant:            "codex",
		Workspace:            ws,
		Terminal:             vterm.New(40, 5),
		Running:              true,
		pendingVisibleOutput: true,
		pendingVisibleSeq:    1,
		bootstrapActivity:    true,
	}

	tab.Terminal.Write([]byte("bootstrap prompt\n"))
	tab.mu.Lock()
	_, _, _ = m.noteVisibleActivityLocked(tab, false, 1)
	first := tab.lastVisibleOutput
	tab.mu.Unlock()
	if !first.IsZero() {
		t.Fatalf("expected bootstrap output not to mark active, got %v", first)
	}

	tab.mu.Lock()
	tab.bootstrapActivity = false
	tab.pendingVisibleOutput = true
	tab.pendingVisibleSeq++
	seq := tab.pendingVisibleSeq
	tab.mu.Unlock()

	tab.Terminal.Write([]byte("real response\n"))
	tab.mu.Lock()
	_, _, _ = m.noteVisibleActivityLocked(tab, false, seq)
	second := tab.lastVisibleOutput
	tab.mu.Unlock()
	if second.IsZero() {
		t.Fatal("expected post-bootstrap real output to mark active")
	}
	if !m.IsTabActive(tab) {
		t.Fatal("expected tab to be active after real visible output")
	}
}

func TestActivityContract_TypingEchoSuppressedButRealOutputAfterWindowCounts(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := &Tab{
		Assistant:            "codex",
		Workspace:            ws,
		Terminal:             vterm.New(40, 5),
		Running:              true,
		pendingVisibleOutput: true,
		pendingVisibleSeq:    1,
	}

	now := time.Now()
	recordLocalInputEchoWindow(tab, "é", now)
	tab.Terminal.Write([]byte("é"))
	tab.mu.Lock()
	_, _, _ = m.noteVisibleActivityLocked(tab, false, 1)
	echoVisible := tab.lastVisibleOutput
	tab.mu.Unlock()
	if !echoVisible.IsZero() {
		t.Fatalf("expected local echo not to mark active, got %v", echoVisible)
	}

	tab.mu.Lock()
	tab.lastUserInputAt = time.Now().Add(-1 * time.Second)
	tab.pendingVisibleOutput = true
	tab.pendingVisibleSeq++
	seq := tab.pendingVisibleSeq
	tab.mu.Unlock()

	tab.Terminal.Write([]byte("\nagent: done\n"))
	tab.mu.Lock()
	_, _, _ = m.noteVisibleActivityLocked(tab, false, seq)
	finalVisible := tab.lastVisibleOutput
	tab.mu.Unlock()
	if finalVisible.IsZero() {
		t.Fatal("expected real output after echo window to mark active")
	}
}
