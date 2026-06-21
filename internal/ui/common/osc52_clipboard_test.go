package common

import "testing"

func TestOSC52ClipboardTextRequiresOptIn(t *testing.T) {
	t.Setenv(OSC52ClipboardEnv, "")

	if got, ok := OSC52ClipboardText([]byte("copy me")); ok || got != "" {
		t.Fatalf("OSC52ClipboardText without opt-in = (%q, %v), want empty false", got, ok)
	}
}

func TestOSC52ClipboardTextAllowsOptedInPayload(t *testing.T) {
	t.Setenv(OSC52ClipboardEnv, "1")

	got, ok := OSC52ClipboardText([]byte("copy me"))
	if !ok || got != "copy me" {
		t.Fatalf("OSC52ClipboardText opted in = (%q, %v), want payload true", got, ok)
	}
}

func TestOSC52ClipboardTextRejectsOversizedPayload(t *testing.T) {
	t.Setenv(OSC52ClipboardEnv, "1")

	payload := make([]byte, OSC52ClipboardMaxBytes+1)
	if got, ok := OSC52ClipboardText(payload); ok || got != "" {
		t.Fatalf("OSC52ClipboardText oversized = (%q, %v), want empty false", got, ok)
	}
}
