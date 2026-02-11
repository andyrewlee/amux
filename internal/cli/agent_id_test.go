package cli

import "testing"

func TestFormatAgentID(t *testing.T) {
	if got := formatAgentID("ws1", "tab1"); got != "ws1:tab1" {
		t.Fatalf("formatAgentID() = %q, want %q", got, "ws1:tab1")
	}
	if got := formatAgentID("ws1", ""); got != "" {
		t.Fatalf("formatAgentID() with missing tab should be empty, got %q", got)
	}
}

func TestParseAgentID(t *testing.T) {
	ws, tab, err := parseAgentID("ws1:tab1")
	if err != nil {
		t.Fatalf("parseAgentID() error = %v", err)
	}
	if ws != "ws1" || tab != "tab1" {
		t.Fatalf("parseAgentID() = (%q,%q), want (%q,%q)", ws, tab, "ws1", "tab1")
	}

	if _, _, err := parseAgentID("invalid"); err == nil {
		t.Fatalf("expected invalid agent id to fail")
	}
}
