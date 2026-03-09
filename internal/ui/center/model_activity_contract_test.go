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

func TestActivityContract_BracketedPasteDoesNotSuppressImmediateRealOutput(t *testing.T) {
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
	recordLocalInputEchoWindow(tab, "\x1b[200~pasted prompt\r\x1b[201~", now)
	tab.Terminal.Write([]byte("agent: ready\n"))
	tab.mu.Lock()
	_, _, _ = m.noteVisibleActivityLockedWithOutput(tab, false, 1, []byte("agent: ready\n"))
	visibleAt := tab.lastVisibleOutput
	echoAt := tab.lastUserInputAt
	promptAt := tab.lastPromptInputAt
	tab.mu.Unlock()

	if !echoAt.IsZero() {
		t.Fatal("expected bracketed paste not to arm the local-echo suppression window")
	}
	if promptAt.IsZero() {
		t.Fatal("expected bracketed paste to keep recent prompt-input state for chat cursor tracking")
	}
	if visibleAt.IsZero() {
		t.Fatal("expected immediate real output after bracketed paste to mark the tab active")
	}
}

func TestActivityContract_SubmittedBracketedPasteEchoSuppressesPromptEchoActivity(t *testing.T) {
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
	recordLocalInputEchoWindow(tab, "\x1b[200~first\r\nsecond\r\x1b[201~", now)
	tab.Terminal.Write([]byte("\r\x1b[Kfirst\r\nsecond"))
	tab.mu.Lock()
	expectedDigest := visibleScreenDigest(tab.Terminal)
	_, _, _ = m.noteVisibleActivityLockedWithOutput(tab, false, 1, []byte("\r\x1b[Kfirst\r\nsecond"))
	visibleAt := tab.lastVisibleOutput
	pending := tab.pendingVisibleOutput
	echoAt := tab.lastUserInputAt
	digest := tab.activityDigest
	digestInit := tab.activityDigestInit
	pendingPaste := tab.pendingSubmitPasteEcho
	tab.mu.Unlock()

	if !echoAt.IsZero() {
		t.Fatal("expected submitted bracketed paste echo suppression not to rely on lastUserInputAt")
	}
	if !visibleAt.IsZero() {
		t.Fatal("expected echoed submitted paste not to mark the tab active")
	}
	if pending {
		t.Fatal("expected echoed submitted paste to clear pending activity when no more visible output is buffered")
	}
	if !digestInit || digest != expectedDigest {
		t.Fatal("expected submitted paste echo suppression to advance the activity digest")
	}
	if pendingPaste != "" {
		t.Fatalf("expected submitted paste echo to be fully consumed, still pending %q", pendingPaste)
	}

	tab.Terminal.Write([]byte("\nagent: ready\n"))
	tab.mu.Lock()
	tab.pendingVisibleOutput = true
	tab.pendingVisibleSeq++
	seq := tab.pendingVisibleSeq
	_, _, _ = m.noteVisibleActivityLockedWithOutput(tab, false, seq, []byte("\nagent: ready\n"))
	visibleAt = tab.lastVisibleOutput
	tab.mu.Unlock()
	if visibleAt.IsZero() {
		t.Fatal("expected real output after submitted paste echo to mark the tab active")
	}
}

func TestActivityContract_PromptOnlyBracketedPasteSuppressesPromptEchoActivity(t *testing.T) {
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
	recordLocalInputEchoWindow(tab, "\x1b[200~wrapped prompt\x1b[201~", now)
	tab.Terminal.Write([]byte("wrapped prompt"))
	tab.mu.Lock()
	_, _, _ = m.noteVisibleActivityLocked(tab, false, 1)
	visibleAt := tab.lastVisibleOutput
	echoAt := tab.lastUserInputAt
	promptAt := tab.lastPromptInputAt
	pending := tab.pendingVisibleOutput
	tab.mu.Unlock()

	if echoAt.IsZero() {
		t.Fatal("expected prompt-only bracketed paste to arm local-echo suppression")
	}
	if promptAt.IsZero() {
		t.Fatal("expected prompt-only bracketed paste to keep recent prompt-input state")
	}
	if !visibleAt.IsZero() {
		t.Fatal("expected prompt-only paste echo not to mark the tab active")
	}
	if !pending {
		t.Fatal("expected prompt-only paste echo to remain pending for re-evaluation after suppression")
	}
}
