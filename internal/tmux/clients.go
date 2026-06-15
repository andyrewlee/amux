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
	output, err := runTmuxCmdCombined(cmd)
	if err != nil {
		if isExitCode1(err) {
			stderr := strings.TrimSpace(string(output))
			// No attached clients should not fail detached-session GC.
			if stderr == "" || isNoClientStderr(stderr) {
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
	lines, err := listTmux(opts, "list-clients", "-t", sessionTarget(sessionName), "-F", "#{client_name}")
	if err != nil {
		return 0, err
	}
	return len(lines), nil
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
	cmd, cancel := tmuxCommand(opts, "list-sessions", "-F", "#{session_name}\t#{session_created}")
	defer cancel()
	output, err := runTmuxCmd(cmd)
	if err != nil {
		return 0, err
	}
	for _, line := range parseOutputLines(output) {
		name, raw, ok := strings.Cut(line, "\t")
		if !ok || name != sessionName {
			continue
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return 0, nil
		}
		return strconv.ParseInt(raw, 10, 64)
	}
	return 0, nil
}
