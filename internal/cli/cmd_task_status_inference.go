package cli

import (
	"strconv"
	"strings"
	"time"
)

const taskStatusCompletionIdleWindow = 45 * time.Second

func taskStatusLooksComplete(candidate taskAgentCandidate, snap taskAgentSnapshot) bool {
	if candidate.HasLastOutput {
		if time.Since(candidate.LastOutputAt) < taskStatusCompletionIdleWindow {
			return false
		}
	} else {
		if candidate.CreatedAt <= 0 {
			return false
		}
		if time.Since(time.Unix(candidate.CreatedAt, 0)) < taskStatusCompletionIdleWindow {
			return false
		}
	}
	if snap.NeedsInput {
		return false
	}
	return !taskSnapshotLooksProgressOnly(snap)
}

func taskSnapshotLooksProgressOnly(snap taskAgentSnapshot) bool {
	summary := strings.TrimSpace(snap.Summary)
	latest := strings.TrimSpace(snap.LatestLine)
	if summary == "" && latest == "" {
		return true
	}
	return isTaskProgressOnlyLine(summary) && isTaskProgressOnlyLine(latest)
}

func isTaskProgressOnlyLine(line string) bool {
	clean := strings.TrimSpace(stripANSIEscape(line))
	if clean == "" || clean == "(no visible output yet)" || clean == "(no output yet)" {
		return true
	}
	if shouldDropAgentChromeLine(clean) || isAgentProgressNoiseLine(clean) {
		return true
	}
	lower := strings.ToLower(clean)
	if strings.Contains(lower, "streaming...") && strings.Contains(lower, "press esc to stop") {
		return true
	}
	if strings.HasPrefix(lower, "still working (idle ") || strings.HasPrefix(lower, "idle ") {
		return true
	}
	return false
}

func parseSessionTagTime(raw string) (time.Time, bool) {
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
