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

	// cat echoes each character as typed, then on Enter it reads the line and
	// writes it back. Poll for the round-trip rather than sleeping a fixed
	// window: we expect "ping" at least twice (typed input line + cat's echo).
	var text string
	ok := eventually(5*time.Second, func() bool {
		out, captured := CapturePaneTail("echo-test", 10, opts)
		if captured {
			text = out
		}
		return captured && strings.Count(text, "ping") >= 2
	})
	if !ok {
		t.Fatalf("expected 'ping' at least twice (typed + echo), got %d in:\n%s", strings.Count(text, "ping"), text)
	}
}

func TestSendTextArgs(t *testing.T) {
	tests := []struct {
		name    string
		session string
		text    string
	}{
		{"plain", "amux-ws-tab", "hello"},
		{"leading dash payload", "amux-ws-tab", "-x"},
		{"empty text", "amux-ws-tab", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := sendTextArgs(tt.session, tt.text)
			if len(args) == 0 || args[0] != "send-keys" {
				t.Fatalf("args[0] = %q, want send-keys (%v)", args, args)
			}
			if !contains(args, "-l") {
				t.Fatalf("missing -l (literal) flag: %v", args)
			}
			// The user text must be the final element, immediately preceded by --,
			// so a leading-dash payload is never parsed as a flag.
			if args[len(args)-1] != tt.text {
				t.Fatalf("text not last element: %v", args)
			}
			if args[len(args)-2] != "--" {
				t.Fatalf("-- must immediately precede the text: %v", args)
			}
			// Raw session name, no sessionTarget '=' prefix.
			if !contains(args, tt.session) {
				t.Fatalf("raw session name %q not present: %v", tt.session, args)
			}
		})
	}
}

func TestSendEnterArgs(t *testing.T) {
	args := sendEnterArgs("amux-ws-tab")
	if len(args) == 0 || args[0] != "send-keys" {
		t.Fatalf("args[0] = %q, want send-keys (%v)", args, args)
	}
	if !contains(args, "-H") {
		t.Fatalf("enter must use hex mode -H: %v", args)
	}
	if args[len(args)-1] != "0D" {
		t.Fatalf("enter payload must be 0D (hex CR), got: %v", args)
	}
	// Must not use a named "Enter" key.
	if contains(args, "Enter") {
		t.Fatalf("enter must be a raw 0D byte, not the named Enter key: %v", args)
	}
}

func contains(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
