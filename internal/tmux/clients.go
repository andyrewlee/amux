package tmux

import (
	"os/exec"
	"strconv"
	"strings"
)

// SessionNamesWithClients returns the set of session names that currently have
// at least one attached client.
func SessionNamesWithClients(opts Options) (map[string]bool, error) {
	attached := make(map[string]bool)
	if err := EnsureAvailable(); err != nil {
		return attached, err
	}
	cmd, cancel := tmuxCommand(opts, "list-clients", "-F", "#{session_name}")
	defer cancel()
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			stderr := strings.ToLower(strings.TrimSpace(string(output)))
			// No attached clients should not fail detached-session GC.
			if stderr == "" || strings.Contains(stderr, "no client") || strings.Contains(stderr, "can't find client") {
				return attached, nil
			}
		}
		return attached, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		attached[name] = true
	}
	return attached, nil
}

// SessionHasClients reports whether the tmux session has any attached clients.
func SessionHasClients(sessionName string, opts Options) (bool, error) {
	if sessionName == "" {
		return false, nil
	}
	exists, err := hasSession(sessionName, opts)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	cmd, cancel := tmuxCommand(opts, "list-clients", "-t", sessionTarget(sessionName), "-F", "#{client_name}")
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
	exists, err := hasSession(sessionName, opts)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	cmd, cancel := tmuxCommand(opts, "display-message", "-p", "-t", sessionTarget(sessionName), "#{session_created}")
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
