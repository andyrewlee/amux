package center

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// The tab-strip echo of the reported-status badge: a blocked or error state
// for the current workspace recolors the chat-tab indicator (the dot the eye
// tracks across tabs), and the TabBarVersion fingerprint changes with it so
// the compose-time gate re-renders the bar when a report lands.

func TestTabBarIndicatorTakesAttentionColor(t *testing.T) {
	m := newTestModel()
	m.SetSize(80, 24)
	wsID := addWorkspaceWithTabs(t, m,
		"ws",
		&Tab{ID: TabID("a"), Assistant: "claude", Name: "alpha", Running: true},
		&Tab{ID: TabID("b"), Assistant: "claude", Name: "beta", Running: true},
	)

	// The inactive tab's indicator carries no background layer, so its
	// styled bytes are exactly the foreground render under test.
	plain := lipgloss.NewStyle().Foreground(common.AgentColor("claude")).Render(common.Icons.Running + " ")
	warning := lipgloss.NewStyle().Foreground(common.ColorWarning()).Render(common.Icons.Running + " ")
	errorDot := lipgloss.NewStyle().Foreground(common.ColorError()).Render(common.Icons.Running + " ")

	if got := m.TabBarView(); !strings.Contains(got, plain) {
		t.Fatalf("without a report the indicator keeps the assistant color")
	}
	if got := m.TabBarView(); strings.Contains(got, warning) || strings.Contains(got, errorDot) {
		t.Fatalf("no attention color before a report, got %q", got)
	}

	m.SetWorkspaceAgentStates(map[string]data.AgentState{wsID: data.StateBlocked})
	if got := m.TabBarView(); !strings.Contains(got, warning) {
		t.Fatal("a blocked report must take the warning color on the indicator")
	}
	if got := m.TabBarView(); strings.Contains(got, plain) {
		t.Fatal("the plain assistant-colored indicator must not remain")
	}

	m.SetWorkspaceAgentStates(map[string]data.AgentState{wsID: data.StateError})
	if got := m.TabBarView(); !strings.Contains(got, errorDot) {
		t.Fatal("an error report must take the error color on the indicator")
	}

	// A state for a different workspace must not touch this bar.
	m.SetWorkspaceAgentStates(map[string]data.AgentState{"other-ws": data.StateBlocked})
	if got := m.TabBarView(); !strings.Contains(got, plain) {
		t.Fatal("another workspace's report must not color this bar's indicator")
	}
}

func TestTabBarVersionTracksWorkspaceAgentState(t *testing.T) {
	m := newTestModel()
	m.SetSize(80, 24)
	wsID := addWorkspaceWithTabs(t, m,
		"ws",
		&Tab{ID: TabID("a"), Assistant: "claude", Name: "alpha", Running: true},
	)

	before := m.TabBarVersion()
	m.SetWorkspaceAgentStates(map[string]data.AgentState{wsID: data.StateBlocked})
	if m.TabBarVersion() == before {
		t.Fatal("a blocked publish must change the tab bar fingerprint")
	}
	// The same value again is stable — the fingerprint is computed, not a
	// counter, so repeated syncs do not invalidate the compose-time gate.
	at := m.TabBarVersion()
	m.SetWorkspaceAgentStates(map[string]data.AgentState{wsID: data.StateBlocked})
	if m.TabBarVersion() != at {
		t.Fatal("an unchanged state must keep the fingerprint stable")
	}
	m.SetWorkspaceAgentStates(nil)
	if m.TabBarVersion() != before {
		t.Fatal("clearing states must return the fingerprint to its no-report value")
	}
}
