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

// UnavailableError marks a permanent tmux-availability failure: no usable
// tmux binary (missing on PATH, `-V` failing, or older than 3.2). Unlike a
// missing server, waiting cannot resolve it, so polling callers such as
// `amux wait` exit immediately instead of retrying forever.
type UnavailableError struct{ err error }

func (e *UnavailableError) Error() string { return e.err.Error() }
func (e *UnavailableError) Unwrap() error { return e.err }

// IsUnavailableError reports whether err is (or wraps) a permanent
// availability failure.
func IsUnavailableError(err error) bool {
	var unavailable *UnavailableError
	return errors.As(err, &unavailable)
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

// HasServer reports whether a tmux server is currently reachable for
// the selected socket/server name: the probe is simply whether
// list-sessions answers. This is the distinction the agent verbs'
// exit-code contract rests on — a server with nothing hosted is a valid
// "empty fleet" (exit 0 for `agent ls`, exit 3 for `send`'s selector),
// while an unreachable transport is exit 4. The tag readers cannot tell
// the two apart (their exit-1 swallowing turns both into empty results),
// so this is the explicit probe. Any probe failure counts as no server:
// a transport that cannot be reached is unreachable regardless of why,
// and matching tmux's error text would let a session NAME containing a
// no-server phrase flip the answer.
func HasServer(opts Options) bool {
	if err := EnsureAvailable(); err != nil {
		return false
	}
	cmd, cancel := tmuxCommand(opts, "list-sessions", "-F", "#{session_name}")
	defer cancel()
	_, err := runTmuxCmdCombined(cmd)
	return err == nil
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
