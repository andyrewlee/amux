package tmux

import (
	"strconv"
	"strings"
)

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

// SessionMeta bundles the per-session facts scan loops need alongside pane
// state, so per-session probes collapse into one batched list-sessions call.
type SessionMeta struct {
	// Attached is the number of clients currently attached (session_attached).
	Attached int
	// CreatedAt is the session creation time in unix seconds (session_created).
	CreatedAt int64
}

// AllSessionMeta returns SessionMeta for every tmux session in a single
// subprocess call. Sessions absent from the map do not exist (or the listing
// raced their teardown) — treat a miss like a per-session probe failure.
func AllSessionMeta(opts Options) (map[string]SessionMeta, error) {
	if err := EnsureAvailable(); err != nil {
		return nil, err
	}
	lines, err := listTmux(opts, "list-sessions", "-F", "#{session_name}\t#{session_attached}\t#{session_created}")
	if err != nil {
		return nil, err
	}
	return parseSessionMeta(lines), nil
}

func parseSessionMeta(lines []string) map[string]SessionMeta {
	out := make(map[string]SessionMeta, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		attached, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		created, _ := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
		out[parts[0]] = SessionMeta{Attached: attached, CreatedAt: created}
	}
	return out
}
