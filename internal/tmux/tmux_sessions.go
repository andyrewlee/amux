package tmux

import "strings"

// ListSessions returns all tmux session names for the configured server.
func ListSessions(opts Options) ([]string, error) {
	if err := EnsureAvailable(); err != nil {
		return nil, err
	}
	return listTmux(opts, "list-sessions", "-F", "#{session_name}")
}

// sessionIDName pairs a session's server-assigned id with its name.
type sessionIDName struct {
	id   string
	name string
}

// listSessionIDNames returns every session as an (id, name) pair. Kills and
// option reads go through the id — a "$N" token cannot prefix-match a sibling
// or rebind to a same-named session created after the listing, which a
// name target can.
func listSessionIDNames(opts Options) ([]sessionIDName, error) {
	lines, err := listTmux(opts, "list-sessions", "-F", "#{session_id}\t#{session_name}")
	if err != nil {
		return nil, err
	}
	rows := make([]sessionIDName, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		if !isSessionIDToken(id) {
			continue
		}
		rows = append(rows, sessionIDName{id: id, name: parts[1]})
	}
	return rows, nil
}

// KillSessionsWithPrefix kills all sessions with a matching name prefix.
func KillSessionsWithPrefix(prefix string, opts Options) error {
	if prefix == "" {
		return nil
	}
	if err := EnsureAvailable(); err != nil {
		return err
	}
	sessions, err := listSessionIDNames(opts)
	if err != nil {
		return err
	}
	var firstErr error
	for _, s := range sessions {
		if !strings.HasPrefix(s.name, prefix) {
			continue
		}
		if err := KillSessionByID(s.id, opts); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// KillSessionsWithPrefixMissingTag kills sessions with a matching name prefix
// only when the given session option is empty. This is used for legacy amux
// sessions created before @amux_instance existed; modern sessions owned by other
// app instances are left alone.
func KillSessionsWithPrefixMissingTag(prefix, tag string, opts Options) error {
	if prefix == "" || tag == "" {
		return nil
	}
	if err := EnsureAvailable(); err != nil {
		return err
	}
	sessions, err := listSessionIDNames(opts)
	if err != nil {
		return err
	}
	var firstErr error
	for _, s := range sessions {
		if !strings.HasPrefix(s.name, prefix) {
			continue
		}
		value, err := sessionTagValueByID(s.id, tag, opts)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if strings.TrimSpace(value) != "" {
			continue
		}
		if err := KillSessionByID(s.id, opts); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// KillWorkspaceSessions kills all sessions for a workspace ID.
func KillWorkspaceSessions(wsID string, opts Options) error {
	if wsID == "" {
		return nil
	}
	prefix := SessionName("amux", wsID) + "-"
	return KillSessionsWithPrefix(prefix, opts)
}

// ListSessionsMatchingTags returns sessions matching all provided tags.
func ListSessionsMatchingTags(tags map[string]string, opts Options) ([]string, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	rows, orderedKeys, err := listSessionsWithTags(tags, opts)
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, row := range rows {
		if matchesTags(row, tags, orderedKeys) {
			matches = append(matches, row.Name)
		}
	}
	return matches, nil
}

// KillSessionsMatchingTags kills sessions that match all provided tags. Kills
// target the parsed "#{session_id}" — not the name — so a foreign session
// whose name contains the field separator cannot redirect the kill onto a
// session whose name only resembles the parsed prefix.
func KillSessionsMatchingTags(tags map[string]string, opts Options) (bool, error) {
	if len(tags) == 0 {
		return false, nil
	}
	rows, orderedKeys, err := listSessionsWithTags(tags, opts)
	if err != nil {
		return false, err
	}
	matched := false
	var firstErr error
	for _, row := range rows {
		if !matchesTags(row, tags, orderedKeys) {
			continue
		}
		matched = true
		if err := KillSessionByID(row.ID, opts); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return matched, firstErr
}
