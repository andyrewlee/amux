package tmux

import (
	"strings"
	"testing"
	"time"
)

func TestSendKeysEmptySession(t *testing.T) {
	// SendKeys with empty session should be a no-op
	err := SendKeys("", "hello", true, DefaultOptions())
	if err != nil {
		t.Errorf("expected nil error for empty session, got %v", err)
	}
}

func TestSendKeysLiteralMode(t *testing.T) {
	// SendKeys with text "Enter" should not error for an empty session,
	// and the -l flag ensures tmux treats it as literal text, not a key name.
	err := SendKeys("", "Enter", false, DefaultOptions())
	if err != nil {
		t.Errorf("expected nil error for empty session with literal Enter, got %v", err)
	}
}

func TestSendInterruptEmptySession(t *testing.T) {
	err := SendInterrupt("", DefaultOptions())
	if err != nil {
		t.Errorf("expected nil error for empty session interrupt, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Integration tests (require real tmux)
// ---------------------------------------------------------------------------

func TestSendKeysDeliversTextAndEnter(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	// Run cat so typed text + Enter produces a visible echo line.
	createSession(t, opts, "echo-test", "cat")
	time.Sleep(100 * time.Millisecond)

	if err := SendKeys("echo-test", "ping", true, opts); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	// cat echoes each character as typed, then on Enter it reads the line
	// and writes it back. Allow time for the round-trip.
	time.Sleep(200 * time.Millisecond)

	text, ok := CapturePaneTail("echo-test", 10, opts)
	if !ok {
		t.Fatal("CapturePaneTail failed")
	}

	// We expect "ping" to appear at least twice in the capture:
	// once as the typed input line, once as cat's echo output.
	if count := strings.Count(text, "ping"); count < 2 {
		t.Fatalf("expected 'ping' at least twice (typed + echo), got %d in:\n%s", count, text)
	}
}
