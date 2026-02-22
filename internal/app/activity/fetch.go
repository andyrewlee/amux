package activity

import (
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// FetchTaggedSessions retrieves tmux sessions with amux tags and known-tab metadata.
func FetchTaggedSessions(svc SessionFetcher, infoBySession map[string]SessionInfo, opts tmux.Options) ([]TaggedSession, error) {
	if isNilSessionFetcher(svc) {
		return nil, ErrTmuxUnavailable
	}
	keys := []string{
		"@amux",
		"@amux_workspace",
		"@amux_tab",
		"@amux_type",
		tmux.TagLastOutputAt,
		tmux.TagLastInputAt,
		tmux.TagSessionLeaseAt,
	}
	rows, err := svc.SessionsWithTags(nil, keys, opts)
	if err != nil {
		return nil, err
	}
	sessions := make([]TaggedSession, 0, len(rows))
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
		lastOutputAt, ok := ParseLastOutputAtTag(row.Tags[tmux.TagLastOutputAt])
		lastInputAt, hasInput := ParseLastOutputAtTag(row.Tags[tmux.TagLastInputAt])
		if !ok {
			// Lease is refreshed on both input and output events; treat it as a
			// compatibility fallback when explicit output tags are absent.
			if leaseAt, leaseOK := ParseLastOutputAtTag(row.Tags[tmux.TagSessionLeaseAt]); leaseOK {
				lastOutputAt = leaseAt
				ok = true
			}
		}
		sessions = append(sessions, TaggedSession{
			Session:       session,
			LastOutputAt:  lastOutputAt,
			HasLastOutput: ok,
			LastInputAt:   lastInputAt,
			HasLastInput:  hasInput,
		})
	}
	return sessions, nil
}

// FetchRecentlyActiveByWindow returns session names with recent tmux window activity.
func FetchRecentlyActiveByWindow(svc SessionFetcher, window time.Duration, opts tmux.Options) (map[string]bool, error) {
	if isNilSessionFetcher(svc) {
		return nil, ErrTmuxUnavailable
	}
	sessions, err := svc.ActiveAgentSessionsByActivity(window, opts)
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

func isNilSessionFetcher(svc SessionFetcher) bool {
	if svc == nil {
		return true
	}
	v := reflect.ValueOf(svc)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// ParseLastOutputAtTag parses a unix timestamp tag value in seconds, millis, or nanos.
func ParseLastOutputAtTag(raw string) (time.Time, bool) {
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

// WorkspaceIDForSession resolves a workspace ID from known tab info, session tags, or session name.
func WorkspaceIDForSession(session tmux.SessionActivity, info SessionInfo, hasInfo bool) string {
	workspaceID := ""
	if hasInfo {
		workspaceID = strings.TrimSpace(info.WorkspaceID)
	}
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(session.WorkspaceID)
	}
	if workspaceID == "" {
		workspaceID = WorkspaceIDFromSessionName(session.Name)
	}
	return workspaceID
}

// IsChatSession determines whether a tmux session represents an active AI agent.
//
// Detection priority:
//  1. Known-tab metadata marks chat sessions active even if tmux type is stale.
//  2. Session tag (@amux_type == "agent") is authoritative for agent sessions.
//  3. For known sessions with no explicit type, fall back to tab metadata.
func IsChatSession(session tmux.SessionActivity, info SessionInfo, hasInfo bool) bool {
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

// IsLikelyUserEcho returns true if the output timestamp is within the echo window
// after the input timestamp, suggesting the output is local echo rather than agent work.
func IsLikelyUserEcho(snapshot TaggedSession) bool {
	if !snapshot.HasLastInput || !snapshot.HasLastOutput {
		return false
	}
	if snapshot.LastOutputAt.Before(snapshot.LastInputAt) {
		return false
	}
	return snapshot.LastOutputAt.Sub(snapshot.LastInputAt) <= InputEchoWindow
}

// HasRecentUserInput returns true if the session has had user input within the suppress window.
func HasRecentUserInput(snapshot TaggedSession, now time.Time) bool {
	if !snapshot.HasLastInput {
		return false
	}
	age := now.Sub(snapshot.LastInputAt)
	return age >= 0 && age <= InputSuppressWindow
}

// HasRecentWindowActivity reports whether a session has recent tmux window
// activity. When the prefilter map is nil (data unavailable), it returns true
// to preserve accuracy by allowing the caller to proceed.
func HasRecentWindowActivity(sessionName string, recentActivityBySession map[string]bool) bool {
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

// WorkspaceIDFromSessionName extracts a workspace ID from an amux session name pattern.
func WorkspaceIDFromSessionName(name string) string {
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
