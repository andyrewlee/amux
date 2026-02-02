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
	cmd, cancel := tmuxCommand(opts, "list-clients", "-t", sessionName, "-F", "#{client_name}")
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
	return strings.TrimSpace(string(output)) != "", nil
}

// SessionCreatedAt returns the tmux session creation timestamp (unix seconds).
func SessionCreatedAt(sessionName string, opts Options) (int64, error) {
	if sessionName == "" {
		return 0, nil
	}
	cmd, cancel := tmuxCommand(opts, "display-message", "-p", "-t", sessionName, "#{session_created}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}
