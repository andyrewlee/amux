package app

import (
	"sort"
	"strconv"
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

type tmuxSidebarDiscoverResult struct {
	WorkspaceID string
	Sessions    []string
}

type sidebarSessionInfo struct {
	name       string
	instanceID string
	createdAt  int64
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
		rows, err := tmux.SessionsWithTags(match, []string{"@amux_assistant", "@amux_created_at"}, opts)
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
			var createdAt int64
			if raw := strings.TrimSpace(row.Tags["@amux_created_at"]); raw != "" {
				createdAt, _ = strconv.ParseInt(raw, 10, 64)
			}
			if createdAt == 0 {
				if fallback, err := tmux.SessionCreatedAt(row.Name, opts); err == nil {
					createdAt = fallback
				}
			}
			tabs = append(tabs, data.TabInfo{
				Assistant:   assistantName,
				Name:        name,
				SessionName: row.Name,
				Status:      "running",
				CreatedAt:   createdAt,
			})
		}
		sort.Slice(tabs, func(i, j int) bool {
			ci, cj := tabs[i].CreatedAt, tabs[j].CreatedAt
			if ci == 0 && cj == 0 {
				return false
			}
			if ci == 0 {
				return false // zero sorts last
			}
			if cj == 0 {
				return true
			}
			return ci < cj
		})
		if len(tabs) == 0 {
			return nil
		}
		return tmuxTabsDiscoverResult{WorkspaceID: wsID, Tabs: tabs}
	}
}

// discoverSidebarTerminalsFromTmux finds terminal sessions for the workspace.
func (a *App) discoverSidebarTerminalsFromTmux(ws *data.Workspace) tea.Cmd {
	if ws == nil || !a.tmuxAvailable {
		return nil
	}
	wsID := string(ws.ID())
	opts := a.tmuxOptions
	instanceID := a.instanceID
	return func() tea.Msg {
		match := map[string]string{
			"@amux":           "1",
			"@amux_workspace": wsID,
			"@amux_type":      "terminal",
		}
		rows, err := tmux.SessionsWithTags(match, []string{"@amux_instance", "@amux_created_at"}, opts)
		if err != nil {
			logging.Warn("tmux sidebar discovery failed: %v", err)
			return tmuxSidebarDiscoverResult{WorkspaceID: wsID}
		}
		sessions := make([]sidebarSessionInfo, 0, len(rows))
		latestByInstance := make(map[string]int64)
		hasClients := make(map[string]bool, len(rows))
		for _, row := range rows {
			if row.Name == "" {
				continue
			}
			state, err := tmux.SessionStateFor(row.Name, opts)
			if err != nil || !state.Exists || !state.HasLivePane {
				continue
			}
			if attached, err := tmux.SessionHasClients(row.Name, opts); err == nil {
				hasClients[row.Name] = attached
			}
			instanceID := strings.TrimSpace(row.Tags["@amux_instance"])
			var createdAt int64
			if raw := strings.TrimSpace(row.Tags["@amux_created_at"]); raw != "" {
				createdAt, _ = strconv.ParseInt(raw, 10, 64)
			}
			if createdAt == 0 {
				if fallback, err := tmux.SessionCreatedAt(row.Name, opts); err == nil {
					createdAt = fallback
				}
			}
			if createdAt > latestByInstance[instanceID] {
				latestByInstance[instanceID] = createdAt
			}
			sessions = append(sessions, sidebarSessionInfo{
				name:       row.Name,
				instanceID: instanceID,
				createdAt:  createdAt,
			})
		}
		sessions = filterSessionsWithoutClients(sessions, hasClients)
		if len(sessions) == 0 {
			return tmuxSidebarDiscoverResult{WorkspaceID: wsID}
		}
		latestByInstance = make(map[string]int64, len(sessions))
		for _, session := range sessions {
			if session.createdAt > latestByInstance[session.instanceID] {
				latestByInstance[session.instanceID] = session.createdAt
			}
		}
		out := selectSidebarSessions(sessions, latestByInstance, instanceID)
		return tmuxSidebarDiscoverResult{WorkspaceID: wsID, Sessions: out}
	}
}

func selectSidebarSessions(sessions []sidebarSessionInfo, latestByInstance map[string]int64, currentInstance string) []string {
	chosenInstance := ""
	if currentInstance != "" {
		if _, ok := latestByInstance[currentInstance]; ok {
			chosenInstance = currentInstance
		}
	}
	if chosenInstance == "" {
		var chosenAt int64
		for instanceID, createdAt := range latestByInstance {
			if instanceID == "" {
				continue
			}
			if createdAt > chosenAt {
				chosenAt = createdAt
				chosenInstance = instanceID
			}
		}
	}
	out := make([]string, 0, len(sessions))
	for _, session := range sessions {
		if chosenInstance != "" && session.instanceID != chosenInstance {
			continue
		}
		out = append(out, session.name)
	}
	return out
}

func filterSessionsWithoutClients(sessions []sidebarSessionInfo, hasClients map[string]bool) []sidebarSessionInfo {
	if len(sessions) == 0 {
		return nil
	}
	out := make([]sidebarSessionInfo, 0, len(sessions))
	for _, session := range sessions {
		if session.name == "" {
			continue
		}
		if hasClients[session.name] {
			continue
		}
		out = append(out, session)
	}
	return out
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
	var addedTabs []data.TabInfo
	for _, tab := range msg.Tabs {
		if tab.SessionName == "" {
			continue
		}
		if _, ok := existing[tab.SessionName]; ok {
			continue
		}
		ws.OpenTabs = append(ws.OpenTabs, tab)
		addedTabs = append(addedTabs, tab)
		added = true
	}
	if !added {
		return nil
	}
	cmds := []tea.Cmd{a.persistWorkspaceTabs(msg.WorkspaceID)}
	if a.activeWorkspace != nil && string(a.activeWorkspace.ID()) == msg.WorkspaceID {
		if restoreCmd := a.center.AddTabsFromWorkspace(ws, addedTabs); restoreCmd != nil {
			cmds = append(cmds, restoreCmd)
		}
	}
	return cmds
}

func (a *App) handleTmuxSidebarDiscoverResult(msg tmuxSidebarDiscoverResult) []tea.Cmd {
	if msg.WorkspaceID == "" {
		return nil
	}
	ws := a.findWorkspaceByID(msg.WorkspaceID)
	if ws == nil {
		return nil
	}
	if len(msg.Sessions) == 0 {
		if cmd := a.sidebarTerminal.SetWorkspace(ws); cmd != nil {
			return []tea.Cmd{cmd}
		}
		return nil
	}
	return a.sidebarTerminal.AddTabsFromSessions(ws, msg.Sessions)
}
