package tmux

import (
	"errors"
	"os/exec"
	"strings"
)

// isExitCode1 reports whether err is an exec.ExitError with exit code 1.
// tmux returns exit code 1 for "not found" conditions (no session, no server, etc.).
func isExitCode1(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1
}

// IsNoServerError reports whether err indicates that no tmux server is
// currently running for the selected socket/server name.
func IsNoServerError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	return strings.Contains(msg, "no server running on") ||
		strings.Contains(msg, "error connecting to") ||
		strings.Contains(msg, "failed to connect to server") ||
		strings.Contains(msg, "connection refused")
}

// isSessionNotFoundStderr reports whether tmux stderr indicates the target
// session does not exist. "no current target" is what has-session reports on
// a server with zero sessions (e.g. kept alive by exit-empty off).
func isSessionNotFoundStderr(stderr string) bool {
	s := strings.ToLower(strings.TrimSpace(stderr))
	return strings.Contains(s, "session not found") ||
		strings.Contains(s, "no such session") ||
		strings.Contains(s, "can't find session") ||
		strings.Contains(s, "no current target")
}

// isOptionMissingStderr reports whether tmux stderr indicates an unknown or
// invalid option, the "missing option" signal for show-options/set-option.
func isOptionMissingStderr(stderr string) bool {
	s := strings.ToLower(strings.TrimSpace(stderr))
	return strings.Contains(s, "invalid option") || strings.Contains(s, "unknown option")
}
