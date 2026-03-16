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
