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

func TestSendKeysEnterDelayBounds(t *testing.T) {
	// Guard against accidental zero or absurd delay values.
	if sendKeysEnterDelay < 10*time.Millisecond {
		t.Fatalf("sendKeysEnterDelay too small (%v), Enter will be dropped by some coding agents", sendKeysEnterDelay)
	}
	if sendKeysEnterDelay > 1*time.Second {
		t.Fatalf("sendKeysEnterDelay too large (%v), will degrade interactive feel", sendKeysEnterDelay)
	}
}

// ---------------------------------------------------------------------------
// Integration tests (require real tmux)
// ---------------------------------------------------------------------------

func TestSendKeysEnterDelay_IsApplied(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "delay-test", "sleep 300")
	time.Sleep(100 * time.Millisecond)

	// Sending without enter should be fast.
	startNoEnter := time.Now()
	if err := SendKeys("delay-test", "hello", false, opts); err != nil {
		t.Fatalf("SendKeys(enter=false): %v", err)
	}
	noEnterDur := time.Since(startNoEnter)

	// Sending with enter should take at least sendKeysEnterDelay longer.
	startEnter := time.Now()
	if err := SendKeys("delay-test", "world", true, opts); err != nil {
		t.Fatalf("SendKeys(enter=true): %v", err)
	}
	enterDur := time.Since(startEnter)

	margin := sendKeysEnterDelay / 2 // allow some jitter
	if enterDur < noEnterDur+margin {
		t.Fatalf("enter=true (%v) should take at least %v longer than enter=false (%v)",
			enterDur, margin, noEnterDur)
	}
}

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
