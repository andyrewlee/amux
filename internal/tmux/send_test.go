package tmux

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeEchoText(t *testing.T) {
	got := normalizeEchoText("  /review   one\t two \n three  ")
	want := "/review one two three"
	if got != want {
		t.Fatalf("normalizeEchoText() = %q, want %q", got, want)
	}
}

func TestEchoContainsTargetNearEnd(t *testing.T) {
	target := normalizeEchoText("/review Focus on regressions and test gaps.")
	capture := strings.Repeat("x", 800) + " /review Focus   on regressions and test gaps. "
	if !echoContainsTargetNearEnd(capture, target) {
		t.Fatal("expected target match near end")
	}
}

func TestEchoContainsTargetNearEnd_IgnoresOldHistory(t *testing.T) {
	target := normalizeEchoText("/review Analyze workspace")
	capture := "/review Analyze workspace " + strings.Repeat("z", 2000)
	if echoContainsTargetNearEnd(capture, target) {
		t.Fatal("expected old-history match to be ignored")
	}
}

func TestShouldProbeEnterEcho(t *testing.T) {
	if shouldProbeEnterEcho(strings.Repeat("a", 40)) {
		t.Fatal("short text should not probe")
	}
	if !shouldProbeEnterEcho(strings.Repeat("a", 120)) {
		t.Fatal("long text should probe")
	}
}

func TestShouldProbeEnterEcho_SlashCommand(t *testing.T) {
	if !shouldProbeEnterEcho("/review uncommitted changes") {
		t.Fatal("slash command should probe")
	}
	if shouldProbeEnterEcho("/x") {
		t.Fatal("very short slash command should not force probe")
	}
}

func TestShouldProbeEnterEcho_MultiLine(t *testing.T) {
	if !shouldProbeEnterEcho("line1\nline2") {
		t.Fatal("multi-line input should probe")
	}
}

func TestEnterSendDelayScalesWithTextLength(t *testing.T) {
	short := enterSendDelay("hi")
	medium := enterSendDelay(strings.Repeat("a", 120))
	long := enterSendDelay(strings.Repeat("a", 2000))

	if short < enterDelayMinNonZero {
		t.Fatalf("short delay = %v, want >= %v", short, enterDelayMinNonZero)
	}
	if medium <= short {
		t.Fatalf("medium delay = %v, want > short delay %v", medium, short)
	}
	if long > enterDelayMaxTotal {
		t.Fatalf("long delay = %v, want <= %v", long, enterDelayMaxTotal)
	}
	wantCapped := enterDelayBase + enterDelayMaxExtra
	if wantCapped > enterDelayMaxTotal {
		wantCapped = enterDelayMaxTotal
	}
	if long != wantCapped {
		t.Fatalf("long delay = %v, want capped value %v", long, wantCapped)
	}
}

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
