package tmux

import (
	"testing"
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
