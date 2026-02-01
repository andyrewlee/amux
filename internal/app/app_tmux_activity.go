package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
)

type tmuxActivityTick struct {
	Token int
}

type tmuxActivityResult struct {
	Token              int
	ActiveWorkspaceIDs map[string]bool
	Err                error
}

const (
	tmuxActivityWindow   = 60 * time.Second
	tmuxActivityInterval = 2 * time.Second
)

func (a *App) startTmuxActivityTicker() tea.Cmd {
	a.tmuxActivityToken++
	return a.scheduleTmuxActivityTick()
}

func (a *App) scheduleTmuxActivityTick() tea.Cmd {
	token := a.tmuxActivityToken
	return common.SafeTick(tmuxActivityInterval, func(time.Time) tea.Msg {
		return tmuxActivityTick{Token: token}
	})
}

func (a *App) triggerTmuxActivityScan() tea.Cmd {
	token := a.tmuxActivityToken
	return func() tea.Msg {
		return tmuxActivityTick{Token: token}
	}
}

func (a *App) scanTmuxActivityNow() tea.Cmd {
	infoBySession := a.tabSessionInfoByName()
	opts := a.tmuxOptions
	if opts.CommandTimeout <= 0 || opts.CommandTimeout > 2*time.Second {
		opts.CommandTimeout = 2 * time.Second
	}
	return func() tea.Msg {
		sessions, err := tmux.ActiveAgentSessionsByActivity(tmuxActivityWindow, opts)
		if err != nil {
			return tmuxActivityResult{Token: 0, Err: err}
		}
		active := activeWorkspaceIDsFromSessionActivity(infoBySession, sessions)
		return tmuxActivityResult{Token: 0, ActiveWorkspaceIDs: active}
	}
}

func (a *App) handleTmuxActivityTick(msg tmuxActivityTick) []tea.Cmd {
	if msg.Token != a.tmuxActivityToken {
		return nil
	}
	if !a.tmuxAvailable {
		return []tea.Cmd{a.scheduleTmuxActivityTick()}
	}
	sessionInfo := a.tabSessionInfoByName()
	opts := a.tmuxOptions
	if opts.CommandTimeout <= 0 || opts.CommandTimeout > 2*time.Second {
		opts.CommandTimeout = 2 * time.Second
	}
	cmds := []tea.Cmd{a.scheduleTmuxActivityTick(), func() tea.Msg {
		sessions, err := tmux.ActiveAgentSessionsByActivity(tmuxActivityWindow, opts)
		if err != nil {
			return tmuxActivityResult{Token: msg.Token, Err: err}
		}
		active := activeWorkspaceIDsFromSessionActivity(sessionInfo, sessions)
		return tmuxActivityResult{Token: msg.Token, ActiveWorkspaceIDs: active}
	}}
	return cmds
}

func (a *App) handleTmuxActivityResult(msg tmuxActivityResult) []tea.Cmd {
	if msg.Token != 0 && msg.Token != a.tmuxActivityToken {
		return nil
	}
	var cmds []tea.Cmd
	if msg.Err != nil {
		logging.Warn("tmux activity scan failed: %v", msg.Err)
		return cmds
	}
	if msg.ActiveWorkspaceIDs == nil {
		msg.ActiveWorkspaceIDs = make(map[string]bool)
	}
	a.tmuxActiveWorkspaceIDs = msg.ActiveWorkspaceIDs
	a.syncActiveWorkspacesToDashboard()
	if cmd := a.dashboard.StartSpinnerIfNeeded(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

type tabSessionInfo struct {
	Status      string
	WorkspaceID string
	Assistant   string
	IsChat      bool
}

// Concurrency safety: builds the map synchronously in the Update loop.
// Goroutine closures capture only the returned map, never accessing
// a.projects or ws.OpenTabs directly.
func (a *App) tabSessionInfoByName() map[string]tabSessionInfo {
	infoBySession := make(map[string]tabSessionInfo)
	assistants := map[string]struct{}{}
	if a.config != nil {
		for name := range a.config.Assistants {
			assistants[name] = struct{}{}
		}
	}
	for _, project := range a.projects {
		for i := range project.Workspaces {
			ws := &project.Workspaces[i]
			for _, tab := range ws.OpenTabs {
				name := strings.TrimSpace(tab.SessionName)
				if name == "" {
					continue
				}
				status := strings.ToLower(strings.TrimSpace(tab.Status))
				if status == "" {
					status = "running"
				}
				assistant := strings.TrimSpace(tab.Assistant)
				_, isChat := assistants[assistant]
				infoBySession[name] = tabSessionInfo{
					Status:      status,
					WorkspaceID: string(ws.ID()),
					Assistant:   assistant,
					IsChat:      isChat,
				}
			}
		}
	}
	return infoBySession
}

func activeWorkspaceIDsFromSessionActivity(infoBySession map[string]tabSessionInfo, sessions []tmux.SessionActivity) map[string]bool {
	active := make(map[string]bool)
	for _, session := range sessions {
		info, ok := infoBySession[session.Name]
		if !isChatSession(session, info, ok) {
			continue
		}
		workspaceID := strings.TrimSpace(session.WorkspaceID)
		if workspaceID == "" && ok {
			workspaceID = strings.TrimSpace(info.WorkspaceID)
		}
		if workspaceID == "" {
			workspaceID = workspaceIDFromSessionName(session.Name)
		}
		if workspaceID != "" {
			active[workspaceID] = true
		}
	}
	return active
}

// isChatSession determines whether a tmux session represents an active AI agent.
//
// Detection priority:
//  1. Session tag (@amux_type == "agent") — authoritative, set at creation time.
//  2. Stored tab metadata (info.IsChat) — from assistant config lookup.
//  3. Name heuristic (legacy fallback) — matches "amux-*-tab-*" sessions,
//     excluding terminal tabs ("term-tab-"). Only used for sessions tagged
//     with @amux but missing @amux_type (older versions), to avoid false
//     positives from unrelated tmux sessions.
func isChatSession(session tmux.SessionActivity, info tabSessionInfo, hasInfo bool) bool {
	if session.Type != "" {
		return session.Type == "agent"
	}
	if hasInfo {
		return info.IsChat
	}
	if !session.Tagged {
		return false
	}
	// Legacy fallback for untagged sessions (pre-tagging era).
	name := session.Name
	if !strings.HasPrefix(name, "amux-") {
		return false
	}
	if strings.Contains(name, "term-tab-") {
		return false
	}
	return strings.Contains(name, "-tab-")
}

func (a *App) handleTmuxAvailableResult(msg tmuxAvailableResult) []tea.Cmd {
	a.tmuxCheckDone = true
	a.tmuxAvailable = msg.available
	a.tmuxInstallHint = msg.installHint
	if !msg.available {
		return []tea.Cmd{a.toast.ShowError("tmux not installed. " + msg.installHint)}
	}
	_ = tmux.SetMonitorActivityOn(a.tmuxOptions)
	_ = tmux.SetStatusOff(a.tmuxOptions)
	return []tea.Cmd{a.scanTmuxActivityNow()}
}

// resetAllTabStatuses marks all non-stopped tabs as stopped and schedules
// persistence for changed workspaces. Used when switching tmux servers so
// the UI doesn't show stale running/detached status.
func (a *App) resetAllTabStatuses() []tea.Cmd {
	var cmds []tea.Cmd
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			ws := &a.projects[i].Workspaces[j]
			changed := false
			for k := range ws.OpenTabs {
				if ws.OpenTabs[k].Status != "" && ws.OpenTabs[k].Status != "stopped" {
					ws.OpenTabs[k].Status = "stopped"
					changed = true
				}
			}
			if changed {
				cmds = append(cmds, a.persistWorkspaceTabs(string(ws.ID())))
			}
		}
	}
	return cmds
}

func workspaceIDFromSessionName(name string) string {
	const prefix = "amux-"
	if !strings.HasPrefix(name, prefix) {
		return ""
	}
	trimmed := strings.TrimPrefix(name, prefix)
	parts := strings.Split(trimmed, "-")
	if len(parts) < 1 {
		return ""
	}
	return parts[0]
}
