package tmux

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ActiveAgentSessionsByActivity returns tagged agent sessions with recent tmux activity.
// Activity is derived from tmux's window_activity timestamp.
func ActiveAgentSessionsByActivity(window time.Duration, opts Options) ([]SessionActivity, error) {
	if err := EnsureAvailable(); err != nil {
		return nil, err
	}
	applyWindow := window > 0
	// Ensure activity timestamps update for window_activity tracking.
	_ = setMonitorActivityOn(opts)
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
		if amux == "" || amux == "0" {
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
			latest[sessionName] = existing
			continue
		}
		latest[sessionName] = SessionActivity{
			Name:        sessionName,
			WorkspaceID: workspaceID,
			TabID:       tabID,
			Type:        sessionType,
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

func setMonitorActivityOn(opts Options) error {
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
