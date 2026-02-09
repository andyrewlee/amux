package app

import (
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
)

// maxAttachedAgentTabsFromEnv parses AMUX_MAX_ATTACHED_AGENT_TABS.
// Empty or invalid values fall back to defaultMaxAttachedAgentTabs.
// A value of 0 explicitly disables auto-detach enforcement.
func maxAttachedAgentTabsFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("AMUX_MAX_ATTACHED_AGENT_TABS"))
	if raw == "" {
		return defaultMaxAttachedAgentTabs
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		logging.Warn("Invalid AMUX_MAX_ATTACHED_AGENT_TABS=%q; using default %d", raw, defaultMaxAttachedAgentTabs)
		return defaultMaxAttachedAgentTabs
	}
	if value < 0 {
		logging.Warn("Invalid AMUX_MAX_ATTACHED_AGENT_TABS=%q; must be >= 0; using default %d", raw, defaultMaxAttachedAgentTabs)
		return defaultMaxAttachedAgentTabs
	}
	if value == 0 {
		logging.Info("AMUX_MAX_ATTACHED_AGENT_TABS=0; auto-detach limit disabled")
	}
	return value
}

func (a *App) enforceAttachedAgentTabLimit() []tea.Cmd {
	// 0 means disabled (unlimited attached chat tabs).
	if a == nil || a.center == nil || a.maxAttachedAgentTabs <= 0 {
		return nil
	}
	detached := a.center.EnforceAttachedAgentTabLimit(a.maxAttachedAgentTabs)
	if len(detached) == 0 {
		return nil
	}
	logging.Info("Auto-detached %d chat tabs to enforce attached limit=%d", len(detached), a.maxAttachedAgentTabs)
	seen := make(map[string]struct{}, len(detached))
	cmds := make([]tea.Cmd, 0, len(detached))
	for _, tab := range detached {
		wsID := strings.TrimSpace(tab.WorkspaceID)
		if wsID == "" {
			continue
		}
		if _, ok := seen[wsID]; ok {
			continue
		}
		seen[wsID] = struct{}{}
		if cmd := a.persistWorkspaceTabs(wsID); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}
