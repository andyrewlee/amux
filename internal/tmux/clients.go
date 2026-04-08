package tmux

import (
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
		if isExitCode1(err) {
			stderr := strings.ToLower(strings.TrimSpace(string(output)))
			// No attached clients should not fail detached-session GC.
			if stderr == "" || strings.Contains(stderr, "no client") || strings.Contains(stderr, "can't find client") {
				return attached, nil
			}
		}
		return attached, err
	}
	for _, name := range parseOutputLines(output) {
		attached[name] = true
	}
	return attached, nil
}

// SessionHasClients reports whether the tmux session has any attached clients.
func SessionHasClients(sessionName string, opts Options) (bool, error) {
	count, err := SessionClientCount(sessionName, opts)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// SessionClientCount reports how many tmux clients are currently attached to a
// session.
func SessionClientCount(sessionName string, opts Options) (int, error) {
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
	cmd, cancel := tmuxCommand(opts, "list-clients", "-t", sessionTarget(sessionName), "-F", "#{client_name}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		if isExitCode1(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, line := range parseOutputLines(output) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		count++
	}
	return count, nil
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
