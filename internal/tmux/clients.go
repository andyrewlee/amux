package tmux

import (
	"os/exec"
	"strconv"
	"strings"
)

// SessionHasClients reports whether the tmux session has any attached clients.
func SessionHasClients(sessionName string, opts Options) (bool, error) {
	if sessionName == "" {
		return false, nil
	}
	exists, err := hasSession(sessionName, opts)
	if err != nil || !exists {
		return false, err
	}
	cmd, cancel := tmuxCommand(opts, "list-clients", "-t", sessionTarget(sessionName), "-F", "#{client_session}\t#{client_name}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return false, nil
			}
		}
		return false, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) == sessionName {
			return true, nil
		}
	}
	return false, nil
}

// SessionCreatedAt returns the tmux session creation timestamp (unix seconds).
// Returns (0, nil) if the session does not exist.
func SessionCreatedAt(sessionName string, opts Options) (int64, error) {
	if sessionName == "" {
		return 0, nil
	}
	exists, err := hasSession(sessionName, opts)
	if err != nil || !exists {
		return 0, err
	}
	cmd, cancel := tmuxCommand(opts, "display-message", "-p", "-t", sessionTarget(sessionName),
		"#{session_name}\t#{session_created}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	raw := strings.TrimRight(string(output), "\r\n")
	if raw == "" {
		return 0, nil
	}
	parts := strings.SplitN(raw, "\t", 2)
	if len(parts) != 2 {
		return 0, nil
	}
	// Post-validate: reject prefix-match collisions.
	if strings.TrimSpace(parts[0]) != sessionName {
		return 0, nil
	}
	return strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
}
