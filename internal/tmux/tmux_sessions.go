package tmux

import (
	"os/exec"
	"strings"
)

// ListSessions returns all tmux session names for the configured server.
func ListSessions(opts Options) ([]string, error) {
	if err := EnsureAvailable(); err != nil {
		return nil, err
	}
	cmd, cancel := tmuxCommand(opts, "list-sessions", "-F", "#{session_name}")
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
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var sessions []string
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		sessions = append(sessions, name)
	}
	return sessions, nil
}

// KillSessionsWithPrefix kills all sessions with a matching name prefix.
func KillSessionsWithPrefix(prefix string, opts Options) error {
	if prefix == "" {
		return nil
	}
	sessions, err := ListSessions(opts)
	if err != nil {
		return err
	}
	var matched []string
	for _, name := range sessions {
		if strings.HasPrefix(name, prefix) {
			matched = append(matched, name)
		}
	}
	if len(matched) == 0 {
		return nil
	}
	var firstErr error
	for _, name := range matched {
		if err := KillSession(name, opts); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// KillWorkspaceSessions kills all sessions for a workspace ID.
func KillWorkspaceSessions(wsID string, opts Options) error {
	if wsID == "" {
		return nil
	}
	prefix := SessionName("amux", wsID) + "-"
	return KillSessionsWithPrefix(prefix, opts)
}

// AmuxSessionsByWorkspace returns all @amux=1 sessions grouped by their
// @amux_workspace value. Sessions without a workspace tag are omitted.
func AmuxSessionsByWorkspace(opts Options) (map[string][]string, error) {
	rows, err := SessionsWithTags(
		map[string]string{"@amux": "1"},
		[]string{"@amux_workspace"},
		opts,
	)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string)
	for _, row := range rows {
		wsID := row.Tags["@amux_workspace"]
		if wsID == "" {
			continue
		}
		out[wsID] = append(out[wsID], row.Name)
	}
	return out, nil
}

// ListSessionsMatchingTags returns sessions matching all provided tags.
func ListSessionsMatchingTags(tags map[string]string, opts Options) ([]string, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	rows, orderedKeys, err := listSessionsWithTags(tags, opts)
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, row := range rows {
		if matchesTags(row, tags, orderedKeys) {
			matches = append(matches, row.Name)
		}
	}
	return matches, nil
}

// KillSessionsMatchingTags kills sessions that match all provided tags.
func KillSessionsMatchingTags(tags map[string]string, opts Options) (bool, error) {
	sessions, err := ListSessionsMatchingTags(tags, opts)
	if err != nil {
		return false, err
	}
	if len(sessions) == 0 {
		return false, nil
	}
	var firstErr error
	for _, name := range sessions {
		if err := KillSession(name, opts); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return true, firstErr
}
