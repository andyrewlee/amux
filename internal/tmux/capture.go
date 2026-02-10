package tmux

import (
	"fmt"
	"os/exec"
	"strings"
)

// sessionPaneID resolves the globally-unique pane ID (%<number>) for the given
// session. It validates that the session_name in the output matches exactly,
// preventing prefix-match collisions (e.g. "amux-ws-tab-1" resolving to
// "amux-ws-tab-10"). Returns an error if no matching pane is found.
func sessionPaneID(sessionName string, opts Options) (string, error) {
	exists, err := hasSession(sessionName, opts)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("session not found: %s", sessionName)
	}
	cmd, cancel := tmuxCommand(opts, "list-panes", "-t", sessionTarget(sessionName), "-F", "#{session_name}\t#{pane_id}\t#{pane_active}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return "", fmt.Errorf("session not found: %s", sessionName)
			}
		}
		return "", err
	}
	firstMatch := ""
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		if strings.TrimSpace(parts[0]) != sessionName {
			continue
		}
		paneID := strings.TrimSpace(parts[1])
		if paneID == "" {
			continue
		}
		if firstMatch == "" {
			firstMatch = paneID
		}
		if strings.TrimSpace(parts[2]) == "1" {
			return paneID, nil
		}
	}
	if firstMatch != "" {
		return firstMatch, nil
	}
	return "", fmt.Errorf("session not found: %s", sessionName)
}

// CapturePane captures the scrollback history of a tmux pane (excluding the
// visible screen area) with ANSI escape codes preserved. Returns nil if the
// session has no scrollback or does not exist.
func CapturePane(sessionName string, opts Options) ([]byte, error) {
	if sessionName == "" {
		return nil, nil
	}
	paneID, err := sessionPaneID(sessionName, opts)
	if err != nil {
		return nil, err
	}
	// -p: output to stdout
	// -e: include escape sequences (ANSI styling)
	// -S -: start from beginning of history
	// -E -1: end at last scrollback line (excludes visible screen)
	// -t: target pane ID (immune to prefix matching)
	cmd, cancel := tmuxCommand(opts, "capture-pane", "-p", "-e", "-S", "-", "-E", "-1", "-t", paneID)
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if len(output) == 0 {
		return nil, nil
	}
	return output, nil
}
