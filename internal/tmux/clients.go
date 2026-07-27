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
//
// A missing session needs no has-session pre-check: list-clients exits 1 for it,
// which listTmux already maps to "no lines", and a session that does not exist
// has no clients either way. Skipping the pre-check halves the tmux round-trips,
// which matters because the reattach guards call this repeatedly.
func SessionClientCount(sessionName string, opts Options) (int, error) {
	if sessionName == "" {
		return 0, nil
	}
	lines, err := listTmux(opts, "list-clients", "-t", sessionTarget(sessionName), "-F", "#{client_name}")
	if err != nil {
		return 0, err
	}
	return len(lines), nil
}

// SessionCreatedAt returns the tmux session creation timestamp (unix seconds).
// A session that does not exist yields 0 with no error: the name simply does not
// appear in the listing, so no has-session pre-check is needed.
func SessionCreatedAt(sessionName string, opts Options) (int64, error) {
	if sessionName == "" {
		return 0, nil
	}
	lines, err := listTmux(opts, "list-sessions", "-F", "#{session_name}\t#{session_created}")
	if err != nil {
		return 0, err
	}
	for _, line := range lines {
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
