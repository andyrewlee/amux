package tmux

import (
	"strconv"
	"strings"
)

// sessionPaneID resolves the active pane ID for a session using exact name matching.
// Returns empty string if the session does not exist.
func sessionPaneID(sessionName string, opts Options) (string, error) {
	exists, err := hasSession(sessionName, opts)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}
	// Use list-panes instead of display-message. display-message may return an
	// empty pane_id for detached sessions on some tmux versions.
	cmd, cancel := tmuxCommand(opts, "list-panes", "-t", sessionTarget(sessionName), "-F", "#{pane_id}\t#{pane_active}\t#{pane_dead}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	var firstAlive string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		paneID := strings.TrimSpace(parts[0])
		paneActive := strings.TrimSpace(parts[1]) == "1"
		paneDead := strings.TrimSpace(parts[2]) == "1"
		if paneID == "" || paneID[0] != '%' || paneDead {
			continue
		}
		if paneActive {
			return paneID, nil
		}
		if firstAlive == "" {
			firstAlive = paneID
		}
	}
	return firstAlive, nil
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
	if paneID == "" {
		return nil, nil
	}
	// -p: output to stdout
	// -e: include escape sequences (ANSI styling)
	// -S -: start from beginning of history
	// -E -1: end at last scrollback line (excludes visible screen)
	// -t: target pane by globally unique pane ID
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

// CapturePaneTail captures the last N lines of a session's active pane.
// Returns the content and a success flag. Returns ("", false) on error
// (e.g., session doesn't exist or capture times out).
func CapturePaneTail(sessionName string, lines int, opts Options) (string, bool) {
	if sessionName == "" || lines <= 0 {
		return "", false
	}
	paneID, err := sessionPaneID(sessionName, opts)
	if err != nil || paneID == "" {
		return "", false
	}
	startLine := -lines
	cmd, cancel := tmuxCommand(opts, "capture-pane", "-p", "-t", paneID, "-S", strconv.Itoa(startLine))
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return "", false
	}
	// Normalize: trim trailing whitespace from each line and trailing empty lines
	text := strings.TrimRight(string(output), " \t\n\r")
	return text, true
}
