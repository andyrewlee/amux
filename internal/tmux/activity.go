package tmux

import (
	"crypto/md5"
	"strconv"
	"strings"
	"time"
)

func activityWithinWindow(activitySeconds int64, window time.Duration, now time.Time) bool {
	if activitySeconds <= 0 || window <= 0 {
		return false
	}
	activityTime := time.Unix(activitySeconds, 0)
	// tmux reports window_activity with whole-second precision. Keep sessions
	// marked active for the remainder of the reported second so a recent update
	// near a second boundary is not misclassified as quiet.
	return now.Sub(activityTime) <= window+time.Second
}

func sessionLatestActivitySeconds(sessionName string, opts Options) (int64, error) {
	if sessionName == "" {
		return 0, nil
	}
	if err := EnsureAvailable(); err != nil {
		return 0, err
	}
	cmd, cancel := tmuxCommand(opts, "list-windows", "-t", sessionTarget(sessionName), "-F", "#{window_activity}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		if isExitCode1(err) {
			return 0, nil
		}
		return 0, err
	}
	var latest int64
	for _, line := range parseOutputLines(output) {
		activityRaw := strings.TrimSpace(line)
		if activityRaw == "" {
			continue
		}
		activitySeconds, err := strconv.ParseInt(activityRaw, 10, 64)
		if err != nil || activitySeconds <= 0 {
			continue
		}
		if activitySeconds > latest {
			latest = activitySeconds
		}
	}
	return latest, nil
}

// SessionActiveWithin reports whether any window in the session had tmux
// activity within the provided time window.
func SessionActiveWithin(sessionName string, window time.Duration, opts Options) (bool, error) {
	if sessionName == "" || window <= 0 {
		return false, nil
	}
	latest, err := sessionLatestActivitySeconds(sessionName, opts)
	if err != nil {
		return false, err
	}
	if latest == 0 {
		return false, nil
	}
	return activityWithinWindow(latest, window, time.Now()), nil
}

// SessionLatestActivity reports the most recent tmux window_activity timestamp
// for a session without applying any second-resolution slack.
func SessionLatestActivity(sessionName string, opts Options) (time.Time, bool, error) {
	latest, err := sessionLatestActivitySeconds(sessionName, opts)
	if err != nil {
		return time.Time{}, false, err
	}
	if latest == 0 {
		return time.Time{}, false, nil
	}
	return time.Unix(latest, 0), true, nil
}

// ActiveAgentSessionsByActivity returns tagged agent sessions with recent tmux activity.
// Activity is derived from tmux's window_activity timestamp.
// Note: monitor-activity is set once at startup and per-session at creation
// via SetMonitorActivityOn, not on every scan.
func ActiveAgentSessionsByActivity(window time.Duration, opts Options) ([]SessionActivity, error) {
	if err := EnsureAvailable(); err != nil {
		return nil, err
	}
	applyWindow := window > 0
	format := "#{session_name}\t#{window_activity}\t#{@amux}\t#{@amux_workspace}\t#{@amux_tab}\t#{@amux_type}"
	cmd, cancel := tmuxCommand(opts, "list-windows", "-a", "-F", format)
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		if isExitCode1(err) {
			return nil, nil
		}
		return nil, err
	}
	now := time.Now()
	latest := make(map[string]SessionActivity)
	for _, line := range parseOutputLines(output) {
		parts := strings.Split(line, "\t")
		if len(parts) < 6 {
			continue
		}
		sessionName := strings.TrimSpace(parts[0])
		amux := strings.TrimSpace(parts[2])
		tagged := amux != "" && amux != "0"
		if !tagged {
			if !strings.HasPrefix(sessionName, "amux-") {
				continue
			}
		}
		workspaceID := strings.TrimSpace(parts[3])
		tabID := strings.TrimSpace(parts[4])
		sessionType := strings.TrimSpace(parts[5])
		if sessionType != "" && sessionType != "agent" {
			continue
		}
		activityRaw := strings.TrimSpace(parts[1])
		if activityRaw == "" {
			continue
		}
		activitySeconds, err := strconv.ParseInt(activityRaw, 10, 64)
		if err != nil || activitySeconds <= 0 {
			continue
		}
		activityTime := time.Unix(activitySeconds, 0)
		if applyWindow && now.Sub(activityTime) > window {
			continue
		}
		if existing, ok := latest[sessionName]; ok {
			// Keep the most recent activity; window_activity already filtered.
			if existing.WorkspaceID == "" {
				existing.WorkspaceID = workspaceID
			}
			if existing.TabID == "" {
				existing.TabID = tabID
			}
			if existing.Type == "" {
				existing.Type = sessionType
			}
			if !existing.Tagged && tagged {
				existing.Tagged = true
			}
			latest[sessionName] = existing
			continue
		}
		latest[sessionName] = SessionActivity{
			Name:        sessionName,
			WorkspaceID: workspaceID,
			TabID:       tabID,
			Type:        sessionType,
			Tagged:      tagged,
		}
	}
	if len(latest) == 0 {
		return nil, nil
	}
	sessions := make([]SessionActivity, 0, len(latest))
	for _, session := range latest {
		sessions = append(sessions, session)
	}
	return sessions, nil
}

// SetMonitorActivityOn enables tmux monitor-activity globally.
// Called once at startup and when the tmux server name changes,
// rather than on every activity scan.
func SetMonitorActivityOn(opts Options) error {
	cmd, cancel := tmuxCommand(opts, "set-option", "-g", "monitor-activity", "on")
	defer cancel()
	if err := cmd.Run(); err != nil {
		if isExitCode1(err) {
			return nil
		}
		return err
	}
	return nil
}

// SetStatusOff disables the tmux status line globally for the server.
func SetStatusOff(opts Options) error {
	cmd, cancel := tmuxCommand(opts, "set-option", "-g", "status", "off")
	defer cancel()
	if err := cmd.Run(); err != nil {
		if isExitCode1(err) {
			return nil
		}
		return err
	}
	return nil
}

// ContentHash returns a fast hash of the content for change detection.
func ContentHash(content string) [16]byte {
	return md5.Sum([]byte(content))
}
