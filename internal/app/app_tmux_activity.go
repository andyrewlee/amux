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

func isChatSession(session tmux.SessionActivity, info tabSessionInfo, hasInfo bool) bool {
	if session.Type != "" {
		return session.Type == "agent"
	}
	if hasInfo {
		return info.IsChat
	}
	name := session.Name
	if strings.Contains(name, "term-tab-") {
		return false
	}
	return strings.Contains(name, "-tab-")
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
