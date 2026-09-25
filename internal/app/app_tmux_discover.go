package app

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
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
	wsIDForms := workspacesvc.WorkspaceIDStrings(ws)
	assistant := strings.TrimSpace(ws.Assistant)
	if assistant == "" {
		assistant = a.defaultAssistantName()
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
		rows, err := sessionsWithWorkspaceTag(svc, wsIDForms, "@amux_type", "agent",
			[]string{"@amux_assistant", "@amux_created_at"}, opts)
		if err != nil {
			logging.Warn("tmux session discovery failed: %v", err)
			return nil
		}
		// One batched metadata call replaces a per-session SessionCreatedAt
		// probe per row — but only when a row actually lacks the
		// @amux_created_at tag, so steady state skips the fork entirely.
		var meta map[string]tmux.SessionMeta
		var metaFetched bool
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
				if !metaFetched {
					meta, _ = svc.AllSessionMeta(opts)
					metaFetched = true
				}
				if m, ok := meta[row.Name]; ok {
					createdAt = m.CreatedAt
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
	wsIDForms := workspacesvc.WorkspaceIDStrings(ws)
	if !a.tmuxAvailable {
		// tmux is a required dependency; return an empty result so the sidebar
		// can still attempt to initialize and surface a clear error if tmux is missing.
		return func() tea.Msg {
			return tmuxSidebarDiscoverResult{WorkspaceID: wsID}
		}
	}
	opts := a.tmuxOptions
	svc := a.tmuxService
	return func() tea.Msg {
		if svc == nil {
			return tmuxSidebarDiscoverResult{WorkspaceID: wsID}
		}
		rows, err := sessionsWithWorkspaceTag(svc, wsIDForms, "@amux_type", "terminal",
			[]string{"@amux_instance", "@amux_created_at"}, opts)
		if err != nil {
			logging.Warn("tmux sidebar discovery failed: %v", err)
			return tmuxSidebarDiscoverResult{WorkspaceID: wsID}
		}
		// Two batched calls replace per-row SessionStateFor / SessionHasClients /
		// SessionCreatedAt probes. A failed batch yields empty maps; rows then hit
		// the same conservative defaults their per-call error paths used.
		allStates, stateErr := svc.AllSessionStates(opts)
		if stateErr != nil {
			logging.Warn("tmux sidebar discovery: session states unavailable: %v", stateErr)
		}
		meta, metaErr := svc.AllSessionMeta(opts)
		if metaErr != nil {
			logging.Warn("tmux sidebar discovery: session meta unavailable: %v", metaErr)
		}
		sessions := make([]sidebarSessionInfo, 0, len(rows))
		for _, row := range rows {
			if row.Name == "" {
				continue
			}
			state, ok := allStates[row.Name]
			if !ok || !state.Exists || !state.HasLivePane {
				continue
			}
			// Assume clients exist when the batched listing lacks the session —
			// same fail-closed default the per-call error path used.
			attached := true
			if m, ok := meta[row.Name]; ok {
				attached = m.Attached > 0
			}
			rowInstanceID := strings.TrimSpace(row.Tags["@amux_instance"])
			var createdAt int64
			if raw := strings.TrimSpace(row.Tags["@amux_created_at"]); raw != "" {
				createdAt, _ = strconv.ParseInt(raw, 10, 64)
			}
			if createdAt == 0 {
				if m, ok := meta[row.Name]; ok {
					createdAt = m.CreatedAt
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
		out := buildSidebarSessionAttachInfos(sessions)
		return tmuxSidebarDiscoverResult{WorkspaceID: wsID, Sessions: out}
	}
}

// sessionsWithWorkspaceTag issues ONE SessionsWithTags call covering every
// workspace identity form, then keeps rows whose @amux_workspace tag is in
// the form set — the per-form queries it replaced were byte-identical
// list-sessions forks since matching happens client-side anyway. Sessions
// spawned before stable IDs carry whichever path-derived form ws.ID()
// returned at spawn — matching only the persisted store key would orphan
// them on restart. wsIDForms must be captured on the Update goroutine
// (ComputedID does filesystem work).
func sessionsWithWorkspaceTag(svc TmuxOps, wsIDForms []string, extraKey, extraValue string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
	forms := make(map[string]struct{}, len(wsIDForms))
	for _, form := range wsIDForms {
		forms[form] = struct{}{}
	}
	match := map[string]string{"@amux": "1"}
	if extraKey != "" {
		match[extraKey] = extraValue
	}
	if !slices.Contains(keys, "@amux_workspace") {
		keys = append(slices.Clone(keys), "@amux_workspace")
	}
	rows, err := svc.SessionsWithTags(match, keys, opts)
	if err != nil {
		return nil, err
	}
	var out []tmux.SessionTagValues
	for _, row := range rows {
		if _, ok := forms[row.Tags["@amux_workspace"]]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

func buildSidebarSessionAttachInfos(sessions []sidebarSessionInfo) []sidebar.SessionAttachInfo {
	sorted := make([]sidebarSessionInfo, len(sessions))
	copy(sorted, sessions)
	sort.SliceStable(sorted, func(i, j int) bool {
		ci, cj := sorted[i].createdAt, sorted[j].createdAt
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
		return sorted[i].name < sorted[j].name
	})
	out := make([]sidebar.SessionAttachInfo, 0, len(sorted))
	for _, session := range sorted {
		out = append(out, sidebar.SessionAttachInfo{
			Name:           session.name,
			Attach:         true,
			DetachExisting: !session.hasClients,
		})
	}
	return out
}

// discoveryResultBlocked reports whether a discovery result naming ws must be
// dropped: the workspace entered a lifecycle phase (delete/shelve/restore)
// between the async scan and the result landing, so attaching or filing tabs
// now would race the teardown and resurrect state under a tombstoned key. The
// check covers every identity form — in-flight marks, the root bridge, and
// deleted-until-load tombstones — plus the store's Archived/Shelved flags.
func (a *App) discoveryResultBlocked(ws *data.Workspace, wsID string) bool {
	if ws == nil {
		return true
	}
	if a.lifecycle.shouldFilterDeletedWorkspace(wsID, ws.Root, 0) {
		logging.Debug("dropping tmux discovery result for workspace %s mid-lifecycle", wsID)
		return true
	}
	if ws.Archived || ws.Shelved {
		logging.Debug("dropping tmux discovery result for archived/shelved workspace %s", wsID)
		return true
	}
	return false
}

func (a *App) handleTmuxTabsDiscoverResult(msg tmuxTabsDiscoverResult) []tea.Cmd {
	if msg.WorkspaceID == "" || len(msg.Tabs) == 0 {
		return nil
	}
	ws := a.findWorkspaceByID(msg.WorkspaceID)
	if a.discoveryResultBlocked(ws, msg.WorkspaceID) {
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
	if a.discoveryResultBlocked(ws, msg.WorkspaceID) {
		return nil
	}
	if a.activeWorkspace == nil || string(a.activeWorkspace.ID()) != msg.WorkspaceID {
		// Stale result: the user switched workspaces while discovery ran.
		// Attaching now would spend PTYs on a workspace the user just left
		// and put eviction pressure on the attached-terminal limit; the next
		// activation of that workspace re-runs discovery.
		return nil
	}
	if len(msg.Sessions) == 0 {
		if cmd := a.sidebarTerminal.SetWorkspace(ws); cmd != nil {
			return []tea.Cmd{cmd}
		}
		return nil
	}
	return a.sidebarTerminal.AddTabsFromSessionInfos(ws, msg.Sessions)
}
