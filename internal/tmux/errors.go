package tmux

import "strings"

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
