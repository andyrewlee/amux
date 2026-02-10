package tmux

import (
	"crypto/md5"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

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
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return nil, nil
			}
		}
		return nil, err
	}
	now := time.Now()
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	latest := make(map[string]SessionActivity)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
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
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return nil
			}
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
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return nil
			}
		}
		return err
	}
	return nil
}

// CapturePaneTail captures the last N lines of a session's active pane.
// Returns the content and a success flag. Returns ("", false) on error
// (e.g., session doesn't exist or capture times out).
func CapturePaneTail(sessionName string, lines int, opts Options) (string, bool) {
	if sessionName == "" || lines <= 0 {
		return "", false
	}
	startLine := -lines
	// capture-pane does not accept "=session" directly as a pane target, but
	// "=session:" enforces exact session matching while targeting its active pane.
	target := exactTarget(sessionName) + ":"
	cmd, cancel := tmuxCommand(opts, "capture-pane", "-p", "-t", target, "-S", strconv.Itoa(startLine))
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return "", false
	}
	// Normalize: trim trailing whitespace from each line and trailing empty lines
	text := strings.TrimRight(string(output), " \t\n\r")
	return text, true
}

// ContentHash returns a fast hash of the content for change detection.
func ContentHash(content string) [16]byte {
	return md5.Sum([]byte(content))
}
