package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

type sessionRow struct {
	name      string
	tags      map[string]string
	attached  bool
	createdAt int64
}

// buildSessionList converts raw session rows into list entries.
func buildSessionList(rows []sessionRow, now time.Time) []sessionListEntry {
	var entries []sessionListEntry
	for _, row := range rows {
		wsID := row.tags["@amux_workspace"]
		if wsID == "" {
			wsID = inferWorkspaceID(row.name)
		}
		sessionType := row.tags["@amux_type"]
		if sessionType == "" {
			sessionType = inferSessionType(row.name)
		}
		var age int64
		if row.createdAt > 0 {
			age = int64(now.Sub(time.Unix(row.createdAt, 0)).Seconds())
			if age < 0 {
				age = 0
			}
		}
		entries = append(entries, sessionListEntry{
			SessionName: row.name,
			WorkspaceID: wsID,
			Type:        sessionType,
			Attached:    row.attached,
			CreatedAt:   row.createdAt,
			AgeSeconds:  age,
		})
	}
	return entries
}

// findPruneCandidates determines which sessions should be pruned.
func findPruneCandidates(rows []sessionRow, wsIDs []data.WorkspaceID, minAge time.Duration, now time.Time) []pruneEntry {
	validWS := make(map[string]bool, len(wsIDs))
	for _, id := range wsIDs {
		validWS[string(id)] = true
	}

	var candidates []pruneEntry
	for _, row := range rows {
		// Only consider amux-owned sessions for pruning.
		if !isAmuxSession(row) {
			continue
		}

		wsID := row.tags["@amux_workspace"]
		if wsID == "" {
			wsID = inferWorkspaceID(row.name)
		}
		sessionType := row.tags["@amux_type"]
		if sessionType == "" {
			sessionType = inferSessionType(row.name)
		}

		var age int64
		if row.createdAt > 0 {
			age = int64(now.Sub(time.Unix(row.createdAt, 0)).Seconds())
			if age < 0 {
				age = 0
			}
		}

		// Apply age filter. When --older-than is set, skip sessions whose age
		// is unknown — we can't prove they meet the threshold, so err on the
		// safe side and leave them alone.
		if minAge > 0 {
			if row.createdAt == 0 {
				continue
			}
			if now.Sub(time.Unix(row.createdAt, 0)) < minAge {
				continue
			}
		}

		reason := classifyForPrune(wsID, sessionType, row.attached, validWS)
		if reason == "" {
			continue
		}

		candidates = append(candidates, pruneEntry{
			Session:     row.name,
			WorkspaceID: wsID,
			Reason:      reason,
			AgeSeconds:  age,
		})
	}
	return candidates
}

// classifyForPrune returns the prune reason, or "" if the session should not be pruned.
func classifyForPrune(wsID, sessionType string, attached bool, validWS map[string]bool) string {
	// Never prune attached sessions.
	if attached {
		return ""
	}
	// Orphaned: workspace not in metadata store.
	if wsID != "" && !validWS[wsID] {
		return "orphaned_workspace"
	}
	// Detached term-tab sessions for existing workspaces.
	if isTermTabType(sessionType) {
		return "detached_terminal"
	}
	return ""
}

// isAmuxSession returns true if the session is owned by amux (tagged or name-prefixed).
func isAmuxSession(row sessionRow) bool {
	if row.tags["@amux_workspace"] != "" {
		return true
	}
	return strings.HasPrefix(row.name, "amux-")
}

// defaultQuerySessionRows queries tmux list-sessions with tags, attached state, and creation time.
func defaultQuerySessionRows(opts tmux.Options) ([]sessionRow, error) {
	if err := tmux.EnsureAvailable(); err != nil {
		return nil, err
	}
	// Query amux tags.
	rows, err := tmux.SessionsWithTags(
		nil,
		[]string{"@amux_workspace", "@amux_type", "@amux_created_at"},
		opts,
	)
	if err != nil {
		return nil, err
	}
	// Query attached state and tmux creation time.
	metaRows, err := tmux.SessionsWithTags(
		nil,
		[]string{"session_attached", "session_created"},
		opts,
	)
	if err != nil {
		return nil, err
	}
	attachedMap := make(map[string]bool, len(metaRows))
	createdMap := make(map[string]int64, len(metaRows))
	for _, r := range metaRows {
		if v := r.Tags["session_attached"]; v != "" && v != "0" {
			attachedMap[r.Name] = true
		}
		if v := r.Tags["session_created"]; v != "" {
			if ts, parseErr := strconv.ParseInt(v, 10, 64); parseErr == nil {
				createdMap[r.Name] = ts
			}
		}
	}

	var result []sessionRow
	for _, r := range rows {
		createdAt := parseTagCreatedAt(r.Tags["@amux_created_at"])
		if createdAt == 0 {
			createdAt = createdMap[r.Name]
		}
		result = append(result, sessionRow{
			name:      r.Name,
			tags:      r.Tags,
			attached:  attachedMap[r.Name],
			createdAt: createdAt,
		})
	}
	return result, nil
}

func parseTagCreatedAt(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// inferWorkspaceID extracts workspace ID from a session name like "amux-<wsID>-tab-1".
func inferWorkspaceID(name string) string {
	if !strings.HasPrefix(name, "amux-") {
		return ""
	}
	rest := name[len("amux-"):]
	if idx := strings.Index(rest, "-term-tab-"); idx > 0 {
		return rest[:idx]
	}
	if idx := strings.Index(rest, "-tab-"); idx > 0 {
		return rest[:idx]
	}
	return rest
}

// inferSessionType guesses session type from the session name.
func inferSessionType(name string) string {
	if strings.Contains(name, "-term-tab-") {
		return "term-tab"
	}
	if strings.Contains(name, "-tab-") {
		return "agent"
	}
	return "unknown"
}

func isTermTabType(sessionType string) bool {
	return sessionType == "term-tab" || sessionType == "terminal"
}

func formatAge(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	if seconds < 86400 {
		return fmt.Sprintf("%dh", seconds/3600)
	}
	return fmt.Sprintf("%dd", seconds/86400)
}

func humanReason(reason string) string {
	switch reason {
	case "orphaned_workspace":
		return "orphaned workspace"
	case "detached_terminal":
		return "detached terminal"
	default:
		return reason
	}
}
