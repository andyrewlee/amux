package app

import (
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

type tmuxTabsDiscoverResult struct {
	WorkspaceID string
	Tabs        []data.TabInfo
}

type tmuxSidebarDiscoverResult struct {
	WorkspaceID string
	Sessions    []sidebar.SessionAttachInfo
}

type sidebarSessionInfo struct {
	name       string
	instanceID string
	createdAt  int64
	hasClients bool
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
	svc := a.tmuxService
	return func() tea.Msg {
		if svc == nil {
			return nil
		}
		match := map[string]string{
			"@amux":           "1",
			"@amux_workspace": wsID,
			"@amux_type":      "agent",
		}
		rows, err := svc.SessionsWithTags(match, []string{"@amux_assistant", "@amux_created_at"}, opts)
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
				if fallback, err := svc.SessionCreatedAt(row.Name, opts); err == nil {
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
	if ws == nil {
		return nil
	}
	wsID := string(ws.ID())
	if !a.tmuxAvailable {
		// tmux is a required dependency; return an empty result so the sidebar
		// can still attempt to initialize and surface a clear error if tmux is missing.
		return func() tea.Msg {
			return tmuxSidebarDiscoverResult{WorkspaceID: wsID}
		}
	}
	opts := a.tmuxOptions
	instanceID := a.instanceID
	svc := a.tmuxService
	return func() tea.Msg {
		if svc == nil {
			return tmuxSidebarDiscoverResult{WorkspaceID: wsID}
		}
		match := map[string]string{
			"@amux":           "1",
			"@amux_workspace": wsID,
			"@amux_type":      "terminal",
		}
		rows, err := svc.SessionsWithTags(match, []string{"@amux_instance", "@amux_created_at"}, opts)
		if err != nil {
			logging.Warn("tmux sidebar discovery failed: %v", err)
			return tmuxSidebarDiscoverResult{WorkspaceID: wsID}
		}
		sessions := make([]sidebarSessionInfo, 0, len(rows))
		for _, row := range rows {
			if row.Name == "" {
				continue
			}
			state, err := svc.SessionStateFor(row.Name, opts)
			if err != nil || !state.Exists || !state.HasLivePane {
				continue
			}
			// Assume clients exist on error to avoid detaching other sessions.
			attached := true
			if value, err := svc.SessionHasClients(row.Name, opts); err == nil {
				attached = value
			}
			rowInstanceID := strings.TrimSpace(row.Tags["@amux_instance"])
			var createdAt int64
			if raw := strings.TrimSpace(row.Tags["@amux_created_at"]); raw != "" {
				createdAt, _ = strconv.ParseInt(raw, 10, 64)
			}
			if createdAt == 0 {
				if fallback, err := svc.SessionCreatedAt(row.Name, opts); err == nil {
					createdAt = fallback
				}
			}
			sessions = append(sessions, sidebarSessionInfo{
				name:       row.Name,
				instanceID: rowInstanceID,
				createdAt:  createdAt,
				hasClients: attached,
			})
		}
		if len(sessions) == 0 {
			return tmuxSidebarDiscoverResult{WorkspaceID: wsID}
		}
		chosen := selectSidebarInstance(sessions, instanceID)
		out := buildSidebarSessionAttachInfos(sessions, chosen)
		return tmuxSidebarDiscoverResult{WorkspaceID: wsID, Sessions: out}
	}
}

type sidebarInstanceSelection struct {
	ID string
	OK bool
}

func selectSidebarInstance(sessions []sidebarSessionInfo, currentInstance string) sidebarInstanceSelection {
	if len(sessions) == 0 {
		return sidebarInstanceSelection{}
	}
	if currentInstance != "" {
		for _, session := range sessions {
			if session.instanceID == currentInstance {
				return sidebarInstanceSelection{ID: currentInstance, OK: true}
			}
		}
	}
	type instanceStats struct {
		count     int
		createdAt int64
		isEmpty   bool
	}
	stats := make(map[string]*instanceStats)
	for _, session := range sessions {
		stat := stats[session.instanceID]
		if stat == nil {
			stat = &instanceStats{isEmpty: session.instanceID == ""}
			stats[session.instanceID] = stat
		}
		stat.count++
		if session.createdAt > stat.createdAt {
			stat.createdAt = session.createdAt
		}
	}
	var chosenID string
	var chosen instanceStats
	hasChoice := false
	for id, stat := range stats {
		if !hasChoice {
			chosenID = id
			chosen = *stat
			hasChoice = true
			continue
		}
		if stat.count > chosen.count {
			chosenID = id
			chosen = *stat
			continue
		}
		if stat.count == chosen.count {
			if stat.createdAt > chosen.createdAt {
				chosenID = id
				chosen = *stat
				continue
			}
			if stat.createdAt == chosen.createdAt && chosen.isEmpty && !stat.isEmpty {
				chosenID = id
				chosen = *stat
			}
		}
	}
	if !hasChoice {
		return sidebarInstanceSelection{}
	}
	return sidebarInstanceSelection{ID: chosenID, OK: true}
}

func buildSidebarSessionAttachInfos(sessions []sidebarSessionInfo, chosen sidebarInstanceSelection) []sidebar.SessionAttachInfo {
	chosenSessions := make([]sidebarSessionInfo, 0, len(sessions))
	for _, session := range sessions {
		if chosen.OK && session.instanceID != chosen.ID {
			continue
		}
		chosenSessions = append(chosenSessions, session)
	}
	sort.SliceStable(chosenSessions, func(i, j int) bool {
		ci, cj := chosenSessions[i].createdAt, chosenSessions[j].createdAt
		if ci != 0 || cj != 0 {
			if ci == 0 {
				return false
			}
			if cj == 0 {
				return true
			}
			if ci != cj {
				return ci < cj
			}
		}
		return chosenSessions[i].name < chosenSessions[j].name
	})
	out := make([]sidebar.SessionAttachInfo, 0, len(chosenSessions))
	for _, session := range chosenSessions {
		out = append(out, sidebar.SessionAttachInfo{
			Name:           session.name,
			Attach:         true,
			DetachExisting: !session.hasClients,
		})
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
		if a.activeWorkspace != nil && string(a.activeWorkspace.ID()) == msg.WorkspaceID {
			if cmd := a.sidebarTerminal.SetWorkspace(ws); cmd != nil {
				return []tea.Cmd{cmd}
			}
		}
		return nil
	}
	return a.sidebarTerminal.AddTabsFromSessionInfos(ws, msg.Sessions)
}
