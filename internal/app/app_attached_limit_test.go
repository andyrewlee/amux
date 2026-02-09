package app

import "testing"

func TestMaxAttachedAgentTabsFromEnv_DefaultWhenUnset(t *testing.T) {
	t.Setenv("AMUX_MAX_ATTACHED_AGENT_TABS", "")
	got := maxAttachedAgentTabsFromEnv()
	if got != defaultMaxAttachedAgentTabs {
		t.Fatalf("expected default %d, got %d", defaultMaxAttachedAgentTabs, got)
	}
}

func TestMaxAttachedAgentTabsFromEnv_DefaultOnInvalid(t *testing.T) {
	t.Setenv("AMUX_MAX_ATTACHED_AGENT_TABS", "abc")
	got := maxAttachedAgentTabsFromEnv()
	if got != defaultMaxAttachedAgentTabs {
		t.Fatalf("expected default %d, got %d", defaultMaxAttachedAgentTabs, got)
	}
}

func TestMaxAttachedAgentTabsFromEnv_DefaultOnNegative(t *testing.T) {
	t.Setenv("AMUX_MAX_ATTACHED_AGENT_TABS", "-1")
	got := maxAttachedAgentTabsFromEnv()
	if got != defaultMaxAttachedAgentTabs {
		t.Fatalf("expected default %d, got %d", defaultMaxAttachedAgentTabs, got)
	}
}

func TestMaxAttachedAgentTabsFromEnv_ZeroDisablesLimit(t *testing.T) {
	t.Setenv("AMUX_MAX_ATTACHED_AGENT_TABS", "0")
	got := maxAttachedAgentTabsFromEnv()
	if got != 0 {
		t.Fatalf("expected 0 to disable limit, got %d", got)
	}
}

func TestMaxAttachedAgentTabsFromEnv_UsesPositiveValue(t *testing.T) {
	t.Setenv("AMUX_MAX_ATTACHED_AGENT_TABS", "3")
	got := maxAttachedAgentTabsFromEnv()
	if got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
}
