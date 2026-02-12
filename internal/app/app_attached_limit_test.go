package app

import (
	"os"
	"testing"
)

func TestMaxAttachedAgentTabsFromEnv_Default(t *testing.T) {
	os.Unsetenv("AMUX_MAX_ATTACHED_AGENT_TABS")
	got := maxAttachedAgentTabsFromEnv()
	if got != 6 {
		t.Errorf("expected 6, got %d", got)
	}
}

func TestMaxAttachedAgentTabsFromEnv_CustomValue(t *testing.T) {
	t.Setenv("AMUX_MAX_ATTACHED_AGENT_TABS", "10")
	got := maxAttachedAgentTabsFromEnv()
	if got != 10 {
		t.Errorf("expected 10, got %d", got)
	}
}

func TestMaxAttachedAgentTabsFromEnv_ZeroDisables(t *testing.T) {
	t.Setenv("AMUX_MAX_ATTACHED_AGENT_TABS", "0")
	got := maxAttachedAgentTabsFromEnv()
	if got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

func TestMaxAttachedAgentTabsFromEnv_InvalidFallsBack(t *testing.T) {
	t.Setenv("AMUX_MAX_ATTACHED_AGENT_TABS", "abc")
	got := maxAttachedAgentTabsFromEnv()
	if got != 6 {
		t.Errorf("expected 6 (default), got %d", got)
	}
}

func TestMaxAttachedAgentTabsFromEnv_NegativeFallsBack(t *testing.T) {
	t.Setenv("AMUX_MAX_ATTACHED_AGENT_TABS", "-1")
	got := maxAttachedAgentTabsFromEnv()
	if got != 6 {
		t.Errorf("expected 6 (default), got %d", got)
	}
}
