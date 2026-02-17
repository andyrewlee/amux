package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

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

func fetchTaggedSessions(svc *tmuxService, infoBySession map[string]tabSessionInfo, opts tmux.Options) ([]taggedSessionActivity, error) {
	if svc == nil {
		return nil, errTmuxUnavailable
	}
	keys := []string{
		"@amux",
		"@amux_workspace",
		"@amux_tab",
		"@amux_type",
		tmux.TagLastOutputAt,
		tmux.TagLastInputAt,
	}
	rows, err := svc.SessionsWithTags(nil, keys, opts)
	if err != nil {
		return nil, err
	}
	sessions := make([]taggedSessionActivity, 0, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		_, knownSession := infoBySession[name]
		amuxTag := strings.TrimSpace(row.Tags["@amux"])
		tagged := amuxTag != "" && amuxTag != "0"
		if !tagged && !knownSession {
			continue
		}
		session := tmux.SessionActivity{
			Name:        name,
			WorkspaceID: strings.TrimSpace(row.Tags["@amux_workspace"]),
			TabID:       strings.TrimSpace(row.Tags["@amux_tab"]),
			Type:        strings.TrimSpace(row.Tags["@amux_type"]),
			Tagged:      tagged,
		}
		lastOutputAt, ok := parseLastOutputAtTag(row.Tags[tmux.TagLastOutputAt])
		lastInputAt, hasInput := parseLastOutputAtTag(row.Tags[tmux.TagLastInputAt])
		sessions = append(sessions, taggedSessionActivity{
			session:       session,
			lastOutputAt:  lastOutputAt,
			hasLastOutput: ok,
			lastInputAt:   lastInputAt,
			hasLastInput:  hasInput,
		})
	}
	return sessions, nil
}

func fetchRecentlyActiveAgentSessionsByWindow(svc *tmuxService, opts tmux.Options) (map[string]bool, error) {
	if svc == nil {
		return nil, errTmuxUnavailable
	}
	sessions, err := svc.ActiveAgentSessionsByActivity(tmuxActivityPrefilter, opts)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		name := strings.TrimSpace(session.Name)
		if name == "" {
			continue
		}
		byName[name] = true
	}
	return byName, nil
}

func parseLastOutputAtTag(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return time.Time{}, false
	}
	switch {
	case parsed < 1_000_000_000_000:
		return time.Unix(parsed, 0), true
	case parsed < 1_000_000_000_000_000:
		return time.UnixMilli(parsed), true
	default:
		return time.Unix(0, parsed), true
	}
}

func workspaceIDForSession(session tmux.SessionActivity, info tabSessionInfo, hasInfo bool) string {
	workspaceID := ""
	if hasInfo {
		workspaceID = strings.TrimSpace(info.WorkspaceID)
	}
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(session.WorkspaceID)
	}
	if workspaceID == "" {
		workspaceID = workspaceIDFromSessionName(session.Name)
	}
	return workspaceID
}

// isChatSession determines whether a tmux session represents an active AI agent.
//
// Detection priority:
//  1. Known-tab metadata marks chat sessions active even if tmux type is stale.
//  2. Session tag (@amux_type == "agent") is authoritative for agent sessions.
//  3. For known sessions with no explicit type, fall back to tab metadata.
func isChatSession(session tmux.SessionActivity, info tabSessionInfo, hasInfo bool) bool {
	if hasInfo && info.IsChat {
		return true
	}
	if session.Type != "" {
		return session.Type == "agent"
	}
	if hasInfo {
		return info.IsChat
	}
	return false
}

func isLikelyUserEcho(snapshot taggedSessionActivity) bool {
	if !snapshot.hasLastInput || !snapshot.hasLastOutput {
		return false
	}
	if snapshot.lastOutputAt.Before(snapshot.lastInputAt) {
		return false
	}
	return snapshot.lastOutputAt.Sub(snapshot.lastInputAt) <= activityInputEchoWindow
}

func hasRecentUserInput(snapshot taggedSessionActivity, now time.Time) bool {
	if !snapshot.hasLastInput {
		return false
	}
	age := now.Sub(snapshot.lastInputAt)
	return age >= 0 && age <= activityInputSuppressWindow
}

func shouldFallbackForStaleTag(sessionName string, recentActivityBySession map[string]bool) bool {
	name := strings.TrimSpace(sessionName)
	if name == "" {
		return false
	}
	// If prefilter data is unavailable, preserve behavior accuracy by allowing fallback.
	if recentActivityBySession == nil {
		return true
	}
	return recentActivityBySession[name]
}
