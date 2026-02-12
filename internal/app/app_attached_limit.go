package app

import (
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func maxAttachedAgentTabsFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("AMUX_MAX_ATTACHED_AGENT_TABS"))
	if raw == "" {
		return maxAttachedAgentTabsDefault
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return maxAttachedAgentTabsDefault
	}
	return n // 0 = disabled
}

func (a *App) enforceAttachedTabLimit() tea.Cmd {
	if a.maxAttachedAgentTabs <= 0 || a.center == nil {
		return nil
	}
	return a.center.EnforceAttachedAgentTabLimit(a.maxAttachedAgentTabs)
}
