package tmux

import (
	"strings"
	"testing"
	"time"
)

func TestValidSendKey(t *testing.T) {
	t.Parallel()
	// Every name tmux resolves as a key (verified byte-level on the dev
	// target): anything else would be typed as literal text by tmux's
	// unknown-key fallback.
	valid := []string{
		"cr", "escape", "space", "tab", "btab", "up", "down", "left", "right",
		"home", "end", "pgup", "pgdn", "bspace", "delete", "insert",
		"f1", "f9", "f12", "c-c", "c-q", "s-s", "m-v", "m-=", "c-1",
	}
	for _, key := range valid {
		if !ValidSendKey(key) {
			t.Errorf("ValidSendKey(%q) = false, want true", key)
		}
	}
	// Refused: injection forms (send-keys argv is tmux syntax), and names
	// that look like keys but are NOT in tmux's key table — tmux would
	// type them as literal text with exit 0, the silent-wrong-bytes case
	// the vocabulary exists to prevent.
	invalid := []string{
		"", "enter", "Enter", "CR",
		"esc", "bs", "backspace", "del", "pgdown", "a-x", // not tmux key names
		"-l", "-H", "0D", ";", "send-keys",
		"kill-session", "c-c;kill-session", "up up", "f13", "f0", "cs-a", "ctrl-c", "M-x",
	}
	for _, key := range invalid {
		if ValidSendKey(key) {
			t.Errorf("ValidSendKey(%q) = true, want false", key)
		}
	}
}

// capturePaneText returns the pane's current screen content.
func capturePaneText(t *testing.T, opts Options, session string) string {
	t.Helper()
	cmd, cancel := tmuxCommand(opts, "capture-pane", "-p", "-t", session)
	defer cancel()
	out, err := runTmuxCmd(cmd)
	if err != nil {
		t.Fatalf("capture-pane: %v", err)
	}
	return string(out)
}

// TestSendSessionTextAndCR proves the delivery discipline end-to-end on a
// real server: literal text arrives verbatim, and the `cr` key arrives as
// a carriage return that submits the buffered line. The CR's observable
// effect is submission: the pane shows the text once as terminal echo and
// again as `cat`'s output — without the CR byte, cat has read nothing and
// only the echo exists.
func TestSendSessionTextAndCR(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)
	createSession(t, opts, "send-echo", "cat")
	waitForPaneReady(t, opts, "send-echo")

	if err := SendSessionText("send-echo", "hello amux", opts); err != nil {
		t.Fatalf("SendSessionText: %v", err)
	}
	if !eventually(5*time.Second, func() bool {
		return strings.Contains(capturePaneText(t, opts, "send-echo"), "hello amux")
	}) {
		t.Fatalf("literal text did not arrive: %q", capturePaneText(t, opts, "send-echo"))
	}
	if err := SendSessionKeys("send-echo", []string{"cr"}, opts); err != nil {
		t.Fatalf("SendSessionKeys cr: %v", err)
	}
	if !eventually(5*time.Second, func() bool {
		// Echo + cat's own output: the CR terminated the line, so cat
		// read and re-printed it. (The tty's ICRNL translation means the
		// byte reaches the reader as the line end, not a printable ^M.)
		return strings.Count(capturePaneText(t, opts, "send-echo"), "hello amux") >= 2
	}) {
		t.Fatalf("cr did not submit the buffered line: %q", capturePaneText(t, opts, "send-echo"))
	}
}

// TestSendSessionTextDashLeading pins the `--` terminator: tmux's client
// parses argv itself, so dash-leading text would otherwise be consumed as
// a client flag (-R even resets the pane terminal) or silently swallowed
// (--). Both must arrive verbatim instead.
func TestSendSessionTextDashLeading(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)
	createSession(t, opts, "send-dash", "cat")
	waitForPaneReady(t, opts, "send-dash")

	for _, text := range []string{"--verbose", "-R"} {
		if err := SendSessionText("send-dash", text, opts); err != nil {
			t.Fatalf("SendSessionText(%q): %v", text, err)
		}
		if !eventually(5*time.Second, func() bool {
			return strings.Contains(capturePaneText(t, opts, "send-dash"), text)
		}) {
			t.Fatalf("dash-leading text %q did not arrive verbatim: %q", text, capturePaneText(t, opts, "send-dash"))
		}
		if err := SendSessionKeys("send-dash", []string{"cr"}, opts); err != nil {
			t.Fatalf("SendSessionKeys cr: %v", err)
		}
	}
}

// TestSendSessionKeysChordOrder pins that a multi-key sequence is one
// chained tmux command (ordered delivery) and that unknown keys are
// refused before anything is sent.
func TestSendSessionKeysChordOrder(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)
	createSession(t, opts, "send-keys", "cat")
	waitForPaneReady(t, opts, "send-keys")

	if err := SendSessionKeys("send-keys", []string{"a", "b"}, opts); err == nil {
		t.Fatal("key 'a' is not in the closed vocabulary; the call must be refused")
	}
	if err := SendSessionKeys("send-keys", []string{"esc"}, opts); err == nil {
		t.Fatal("key 'esc' is not a tmux key name (tmux would type it as text); the call must be refused")
	}
	if err := SendSessionKeys("send-keys", []string{"left", "right", "cr"}, opts); err != nil {
		t.Fatalf("valid key sequence rejected: %v", err)
	}
}

// TestSendSessionTextRejectsUnknownSession pins the exact-match target
// failing loudly rather than prefix-matching onto a sibling session.
func TestSendSessionTextRejectsUnknownSession(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)
	createSession(t, opts, "send-x-1", "sleep 300")
	createSession(t, opts, "send-x-10", "sleep 300")
	// Exact name targets only; prefix siblings are untouched.
	if err := SendSessionText("send-x-1", "hi", opts); err != nil {
		t.Fatalf("exact target send: %v", err)
	}
	if err := SendSessionText("send-x-does-not-exist", "hi", opts); err == nil {
		t.Fatal("unknown session must fail")
	}
}

// waitForPaneReady blocks until the session's pane command is running (the
// shell has spawned the target program), so send-keys lands in the
// program and not the spawning shell.
func waitForPaneReady(t *testing.T, opts Options, session string) {
	t.Helper()
	if !eventually(5*time.Second, func() bool {
		cmd, cancel := tmuxCommand(opts, "list-panes", "-t", session, "-F", "#{pane_current_command}")
		out, err := runTmuxCmd(cmd)
		cancel()
		return err == nil && strings.TrimSpace(string(out)) == "cat"
	}) {
		t.Fatalf("pane in %s never became ready", session)
	}
}
