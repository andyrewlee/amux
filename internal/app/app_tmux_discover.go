package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
)

type tmuxTabsDiscoverResult struct {
	WorkspaceID string
	Tabs        []data.TabInfo
}

// discoverWorkspaceTabsFromTmux populates missing tabs from live tmux sessions.
func (a *App) discoverWorkspaceTabsFromTmux(ws *data.Workspace) tea.Cmd {
	if ws == nil || !a.tmuxAvailable {
		return nil
	}
	wsID := string(ws.ID())
	assistant := strings.TrimSpace(ws.Assistant)
	if assistant == "" {
		assistant = "claude"
	}
	existing := make(map[string]struct{}, len(ws.OpenTabs))
	for _, tab := range ws.OpenTabs {
		if tab.SessionName == "" {
			continue
		}
		existing[tab.SessionName] = struct{}{}
	}
	opts := a.tmuxOptions
	return func() tea.Msg {
		match := map[string]string{
			"@amux":           "1",
			"@amux_workspace": wsID,
			"@amux_type":      "agent",
		}
		rows, err := tmux.SessionsWithTags(match, []string{"@amux_assistant"}, opts)
		if err != nil {
			logging.Warn("tmux session discovery failed: %v", err)
			return nil
		}
		var tabs []data.TabInfo
		for _, row := range rows {
			if row.Name == "" {
				continue
			}
			if _, ok := existing[row.Name]; ok {
				continue
			}
			assistantName := strings.TrimSpace(row.Tags["@amux_assistant"])
			if assistantName == "" {
				assistantName = assistant
			}
			name := strings.TrimSpace(assistantName)
			if name == "" {
				name = "agent"
			}
			tabs = append(tabs, data.TabInfo{
				Assistant:   assistantName,
				Name:        name,
				SessionName: row.Name,
				Status:      "running",
			})
		}
		if len(tabs) == 0 {
			return nil
		}
		return tmuxTabsDiscoverResult{WorkspaceID: wsID, Tabs: tabs}
	}
}

func (a *App) handleTmuxTabsDiscoverResult(msg tmuxTabsDiscoverResult) []tea.Cmd {
	if msg.WorkspaceID == "" || len(msg.Tabs) == 0 {
		return nil
	}
	ws := a.findWorkspaceByID(msg.WorkspaceID)
	if ws == nil {
		return nil
	}
	existing := make(map[string]struct{}, len(ws.OpenTabs))
	for _, tab := range ws.OpenTabs {
		if tab.SessionName == "" {
			continue
		}
		existing[tab.SessionName] = struct{}{}
	}
	added := false
	for _, tab := range msg.Tabs {
		if tab.SessionName == "" {
			continue
		}
		if _, ok := existing[tab.SessionName]; ok {
			continue
		}
		ws.OpenTabs = append(ws.OpenTabs, tab)
		added = true
	}
	if !added {
		return nil
	}
	cmds := []tea.Cmd{a.persistWorkspaceTabs(msg.WorkspaceID)}
	if a.activeWorkspace != nil && string(a.activeWorkspace.ID()) == msg.WorkspaceID {
		if restoreCmd := a.center.RestoreTabsFromWorkspace(ws); restoreCmd != nil {
			cmds = append(cmds, restoreCmd)
		}
	}
	return cmds
}
