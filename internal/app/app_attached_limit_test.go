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

func TestMaxAttachedTerminalTabsFromEnv_DefaultWhenUnset(t *testing.T) {
	t.Setenv("AMUX_MAX_ATTACHED_TERMINAL_TABS", "")
	got := maxAttachedTerminalTabsFromEnv()
	if got != defaultMaxAttachedTerminalTabs {
		t.Fatalf("expected default %d, got %d", defaultMaxAttachedTerminalTabs, got)
	}
}

func TestMaxAttachedTerminalTabsFromEnv_DefaultOnInvalid(t *testing.T) {
	t.Setenv("AMUX_MAX_ATTACHED_TERMINAL_TABS", "abc")
	got := maxAttachedTerminalTabsFromEnv()
	if got != defaultMaxAttachedTerminalTabs {
		t.Fatalf("expected default %d, got %d", defaultMaxAttachedTerminalTabs, got)
	}
}

func TestMaxAttachedTerminalTabsFromEnv_ZeroDisablesLimit(t *testing.T) {
	t.Setenv("AMUX_MAX_ATTACHED_TERMINAL_TABS", "0")
	got := maxAttachedTerminalTabsFromEnv()
	if got != 0 {
		t.Fatalf("expected 0 to disable limit, got %d", got)
	}
}

func TestMaxAttachedTerminalTabsFromEnv_UsesPositiveValue(t *testing.T) {
	t.Setenv("AMUX_MAX_ATTACHED_TERMINAL_TABS", "4")
	got := maxAttachedTerminalTabsFromEnv()
	if got != 4 {
		t.Fatalf("expected 4, got %d", got)
	}
}
