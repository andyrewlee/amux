package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestNoteVisibleActivityLocked_StaleVisibleSeqKeepsPendingFlag(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := &Tab{
		Assistant:            "codex",
		Workspace:            ws,
		Terminal:             vterm.New(40, 4),
		Running:              true,
		pendingVisibleOutput: true,
		pendingVisibleSeq:    2,
	}

	tab.mu.Lock()
	tab.activityDigest = visibleScreenDigest(tab.Terminal)
	tab.activityDigestInit = true
	_, _, _ = m.noteVisibleActivityLocked(tab, false, 1)
	pending := tab.pendingVisibleOutput
	tab.mu.Unlock()

	if !pending {
		t.Fatal("expected stale visible sequence to preserve pendingVisibleOutput")
	}
}

func TestNoteVisibleActivityLocked_ScrolledViewportStillDetectsLiveOutput(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	term := vterm.New(20, 3)
	term.Write([]byte("line1\nline2\nline3\nline4\n"))
	term.ScrollView(1) // User is viewing older content.

	tab := &Tab{
		Assistant:            "codex",
		Workspace:            ws,
		Terminal:             term,
		Running:              true,
		pendingVisibleOutput: true,
		pendingVisibleSeq:    1,
		activityDigestInit:   true,
	}

	tab.mu.Lock()
	tab.activityDigest = visibleScreenDigest(tab.Terminal)
	tab.mu.Unlock()

	term.Write([]byte("line5\n"))
	tab.mu.Lock()
	tab.pendingVisibleOutput = true
	tab.pendingVisibleSeq = 2
	before := tab.lastVisibleOutput
	_, _, _ = m.noteVisibleActivityLocked(tab, false, 2)
	after := tab.lastVisibleOutput
	tab.mu.Unlock()

	if !before.IsZero() {
		t.Fatalf("expected initial lastVisibleOutput zero, got %v", before)
	}
	if after.IsZero() {
		t.Fatal("expected live output to update activity while viewport is scrolled")
	}
	if time.Since(after) > time.Second {
		t.Fatalf("expected recent activity timestamp, got %v", after)
	}
}

func TestNoteVisibleActivityLocked_SuppressesBootstrapOutputAfterReattach(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	term := vterm.New(20, 3)
	tab := &Tab{
		Assistant:            "codex",
		Workspace:            ws,
		Terminal:             term,
		Running:              true,
		pendingVisibleOutput: true,
		pendingVisibleSeq:    1,
		bootstrapActivity:    true,
	}

	term.Write([]byte("bootstrap\n"))
	expectedDigest := visibleScreenDigest(term)
	tab.mu.Lock()
	_, _, tagged := m.noteVisibleActivityLocked(tab, false, 1)
	last := tab.lastVisibleOutput
	pending := tab.pendingVisibleOutput
	digest := tab.activityDigest
	tab.mu.Unlock()

	if tagged {
		t.Fatal("expected no activity tag during reattach bootstrap suppression")
	}
	if !last.IsZero() {
		t.Fatalf("expected lastVisibleOutput to remain zero during suppression, got %v", last)
	}
	if pending {
		t.Fatal("expected pendingVisibleOutput to clear after suppressed bootstrap flush")
	}
	if digest != expectedDigest {
		t.Fatal("expected activityDigest to update during bootstrap suppression")
	}
}

func TestNoteVisibleActivityLocked_RecordsWhenBootstrapInactive(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	term := vterm.New(20, 3)
	tab := &Tab{
		Assistant:            "codex",
		Workspace:            ws,
		Terminal:             term,
		Running:              true,
		pendingVisibleOutput: true,
		pendingVisibleSeq:    1,
	}

	term.Write([]byte("real-output\n"))
	tab.mu.Lock()
	_, _, _ = m.noteVisibleActivityLocked(tab, false, 1)
	last := tab.lastVisibleOutput
	tab.mu.Unlock()

	if last.IsZero() {
		t.Fatal("expected visible output timestamp after suppression window")
	}
}

func TestNoteVisibleActivityLocked_SubmittedPasteSuppressionDoesNotLeakOnInvisibleFollowup(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	term := vterm.New(20, 4)
	tab := &Tab{
		Assistant:            "codex",
		Workspace:            ws,
		Terminal:             term,
		Running:              true,
		pendingVisibleOutput: true,
		pendingVisibleSeq:    1,
	}

	recordLocalInputEchoWindow(tab, "\x1b[200~first\r\nsecond\r\x1b[201~", time.Now())
	rawEcho := []byte("\r\x1b[Kfirst\r\nsecond")
	term.Write(rawEcho)
	expectedDigest := visibleScreenDigest(term)

	tab.mu.Lock()
	_, _, tagged := m.noteVisibleActivityLockedWithOutput(tab, true, 1, rawEcho)
	last := tab.lastVisibleOutput
	pending := tab.pendingVisibleOutput
	digest := tab.activityDigest
	tab.mu.Unlock()

	if tagged {
		t.Fatal("expected submitted paste echo suppression not to emit an activity tag")
	}
	if !last.IsZero() {
		t.Fatalf("expected submitted paste echo suppression not to mark visible output, got %v", last)
	}
	if !pending {
		t.Fatal("expected buffered follow-up work to keep pendingVisibleOutput armed")
	}
	if digest != expectedDigest {
		t.Fatal("expected submitted paste echo suppression to advance the activity digest")
	}

	tab.mu.Lock()
	_, _, tagged = m.noteVisibleActivityLockedWithOutput(tab, false, 1, []byte("\x1b[?25l"))
	last = tab.lastVisibleOutput
	pending = tab.pendingVisibleOutput
	tab.mu.Unlock()

	if tagged {
		t.Fatal("expected invisible control-only follow-up not to emit an activity tag")
	}
	if !last.IsZero() {
		t.Fatalf("expected invisible control-only follow-up not to mark visible output, got %v", last)
	}
	if pending {
		t.Fatal("expected pendingVisibleOutput to clear after the invisible follow-up flush")
	}
}

func TestConsumeSubmittedPasteEchoLocked_ClearsPendingOnVisibleMismatch(t *testing.T) {
	tab := &Tab{pendingSubmitPasteEcho: "first\nsecond"}

	if consumeSubmittedPasteEchoLocked(tab, []byte("agent: ready\n")) {
		t.Fatal("expected visible mismatch not to be treated as consumed paste echo")
	}
	if tab.pendingSubmitPasteEcho != "" {
		t.Fatalf("expected visible mismatch to drop stale pending paste echo, still have %q", tab.pendingSubmitPasteEcho)
	}
}
