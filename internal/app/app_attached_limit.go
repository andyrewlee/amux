package app

import (
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
)

// attachedTabLimitFromEnv parses an attached-PTY limit from the named env var.
// Empty or invalid values fall back to defaultLimit.
// A value of 0 explicitly disables auto-detach enforcement.
func attachedTabLimitFromEnv(envName string, defaultLimit int) int {
	raw := strings.TrimSpace(os.Getenv(envName))
	if raw == "" {
		return defaultLimit
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		logging.Warn("Invalid %s=%q; using default %d", envName, raw, defaultLimit)
		return defaultLimit
	}
	if value < 0 {
		logging.Warn("Invalid %s=%q; must be >= 0; using default %d", envName, raw, defaultLimit)
		return defaultLimit
	}
	if value == 0 {
		logging.Info("%s=0; auto-detach limit disabled", envName)
	}
	return value
}

func maxAttachedAgentTabsFromEnv() int {
	return attachedTabLimitFromEnv("AMUX_MAX_ATTACHED_AGENT_TABS", defaultMaxAttachedAgentTabs)
}

func maxAttachedTerminalTabsFromEnv() int {
	return attachedTabLimitFromEnv("AMUX_MAX_ATTACHED_TERMINAL_TABS", defaultMaxAttachedTerminalTabs)
}

func (a *App) enforceAttachedAgentTabLimit() []tea.Cmd {
	// 0 means disabled (unlimited attached chat tabs).
	if a == nil || a.center == nil || a.maxAttachedAgentTabs <= 0 {
		return nil
	}
	detached, detachCmds := a.center.EnforceAttachedAgentTabLimit(a.maxAttachedAgentTabs)
	if len(detached) == 0 && len(detachCmds) == 0 {
		return nil
	}
	logging.Info("Auto-detached %d chat tabs to enforce attached limit=%d", len(detached), a.maxAttachedAgentTabs)
	seen := make(map[string]struct{}, len(detached))
	cmds := make([]tea.Cmd, 0, len(detachCmds)+len(detached))
	cmds = append(cmds, detachCmds...)
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

// enforceAttachedTerminalTabLimit auto-detaches least-recently-used sidebar
// terminal PTYs beyond the configured limit. Unlike agent tabs, terminal tabs
// are not persisted per workspace: their tmux sessions are rediscovered by
// tag on workspace activation and re-attached transparently, so no
// persistence follow-up is needed here.
func (a *App) enforceAttachedTerminalTabLimit() {
	// 0 means disabled (unlimited attached terminal PTYs).
	if a == nil || a.sidebarTerminal == nil || a.maxAttachedTerminalTabs <= 0 {
		return
	}
	detached := a.sidebarTerminal.EnforceAttachedTerminalTabLimit(a.maxAttachedTerminalTabs)
	if len(detached) == 0 {
		return
	}
	logging.Info("Auto-detached %d sidebar terminals to enforce attached limit=%d", len(detached), a.maxAttachedTerminalTabs)
}
