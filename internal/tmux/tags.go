package tmux

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type sessionTagRow struct {
	// ID is the server-assigned "#{session_id}" token ("$N"): unforgeable by
	// any other session and immune to name prefix matching and rename/death
	// races, so it is the preferred target for destructive operations.
	ID   string
	Name string
	Tags map[string]string
}

// SessionTagValues stores tag values for a tmux session.
type SessionTagValues struct {
	// ID is the server-assigned "#{session_id}" token ("$N") — see
	// sessionTagRow.ID for why it is the trusted target.
	ID   string
	Name string
	Tags map[string]string
}

const (
	tagFieldSeparator = "|"
	TagLastOutputAt   = "@amux_last_output_at"
	TagLastInputAt    = "@amux_last_input_at"
	TagSessionOwner   = "@amux_session_owner"
	TagSessionLeaseAt = "@amux_session_lease_ms"
	// TagSessionOwnerHeartbeatAt is refreshed by the owning amux process. It is
	// intentionally separate from TagSessionLeaseAt, which represents user/PTY
	// activity and drives inactivity GC.
	TagSessionOwnerHeartbeatAt = "@amux_owner_heartbeat_ms"
	// TagAgentState publishes the semantic per-session agent state
	// ("idle"/"working"/"done", see activity.AgentState.String()) computed by
	// the activity scan's hysteresis. Written best-effort on state transitions
	// only (see app_tmux_activity_result.go); read-only telemetry for external
	// orchestrators.
	TagAgentState = "@amux_agent_state"
)

// SessionsWithTags returns sessions matching the provided tags, plus values for requested tag keys.
func SessionsWithTags(match map[string]string, keys []string, opts Options) ([]SessionTagValues, error) {
	if len(match) == 0 && len(keys) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(match)+len(keys))
	for key, value := range match {
		tags[key] = value
	}
	for _, key := range keys {
		if _, exists := tags[key]; !exists {
			tags[key] = ""
		}
	}
	rows, _, err := listSessionsWithTags(tags, opts)
	if err != nil {
		return nil, err
	}
	matchKeys := make([]string, 0, len(match))
	for key := range match {
		matchKeys = append(matchKeys, key)
	}
	sort.Strings(matchKeys)
	var out []SessionTagValues
	for _, row := range rows {
		if len(matchKeys) > 0 && !matchesTags(row, match, matchKeys) {
			continue
		}
		out = append(out, SessionTagValues(row))
	}
	return out, nil
}

func listSessionsWithTags(tags map[string]string, opts Options) ([]sessionTagRow, []string, error) {
	if err := EnsureAvailable(); err != nil {
		return nil, nil, err
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	// Field order is deliberate: the server-assigned #{session_id} leads, the
	// fixed count of requested tag fields follows, and #{session_name} trails.
	// tmux accepts '|' in session names, so a name-first layout lets a foreign
	// session named "victim|1" shift later fields and forge tag values (which
	// previously redirected KillSessionsMatchingTags onto the innocent
	// "victim"). Name-last confines '|' bytes to the free-form tail field,
	// where SplitN preserves them verbatim.
	format := "#{session_id}"
	for _, key := range keys {
		format = fmt.Sprintf("%s%s#{%s}", format, tagFieldSeparator, key)
	}
	format = fmt.Sprintf("%s%s#{session_name}", format, tagFieldSeparator)
	cmd, cancel := tmuxCommand(opts, "list-sessions", "-F", format)
	defer cancel()
	output, err := runTmuxCmd(cmd)
	if err != nil {
		if isExitCode1(err) {
			return nil, keys, nil
		}
		return nil, keys, err
	}
	return parseSessionTagRows(parseOutputLines(output), keys), keys, nil
}

// parseSessionTagRows is the pure parse half of listSessionsWithTags. It takes
// the `list-sessions -F` output lines (session_id, then one
// tagFieldSeparator-joined field per requested key in keys order, then the
// free-form session_name tail) and returns one sessionTagRow per line.
//
// The name is emitted last because tmux accepts '|' in session names:
// SplitN(len(keys)+2) joins any separator bytes inside the name into the tail
// field, so name content can never shift the positional tag fields. Rows are
// dropped when they are malformed (fewer fields than the fixed prefix + name)
// or carry a non-"$N" id — a corrupt row must not attribute tags to the wrong
// session. Extracting it makes the split and the validation branches
// unit-testable without a live tmux server.
func parseSessionTagRows(lines, keys []string) []sessionTagRow {
	var rows []sessionTagRow
	want := len(keys) + 2 // id + one field per key + name
	for _, line := range lines {
		parts := strings.SplitN(line, tagFieldSeparator, want)
		if len(parts) != want {
			continue
		}
		id := strings.TrimSpace(parts[0])
		if !isSessionIDToken(id) {
			continue
		}
		row := sessionTagRow{
			ID:   id,
			Name: parts[want-1],
			Tags: make(map[string]string, len(keys)),
		}
		for i, key := range keys {
			row.Tags[key] = strings.TrimSpace(parts[i+1])
		}
		rows = append(rows, row)
	}
	return rows
}

// isSessionIDToken reports whether s is a well-formed tmux session id ("$N").
// Ids are assigned by the server and never reused for its lifetime, so they
// are the unforgeable anchor for reads and kills.
func isSessionIDToken(s string) bool {
	if len(s) < 2 || s[0] != '$' {
		return false
	}
	_, err := strconv.ParseUint(s[1:], 10, 64)
	return err == nil
}

func matchesTags(row sessionTagRow, tags map[string]string, orderedKeys []string) bool {
	for _, key := range orderedKeys {
		want := tags[key]
		value := strings.TrimSpace(row.Tags[key])
		// Empty means "tag must be present" vs. equal to empty.
		if want == "" {
			if value == "" {
				return false
			}
		} else if value != want {
			return false
		}
	}
	return true
}

// SetSessionTagValue sets a tmux session option for the given session.
// Returns nil if the session no longer exists (killed between create and tag).
func SetSessionTagValue(sessionName, key, value string, opts Options) error {
	if key == "" {
		return nil
	}
	return SetSessionTagValues(sessionName, []OptionValue{{Key: key, Value: value}}, opts)
}

// SetSessionTagValues sets multiple tmux session options for the given session
// in a single tmux command invocation. Returns nil if the session no longer
// exists (killed between create and tag).
func SetSessionTagValues(sessionName string, tags []OptionValue, opts Options) error {
	if sessionName == "" || len(tags) == 0 {
		return nil
	}
	if err := EnsureAvailable(); err != nil {
		return err
	}

	target := exactSessionOptionTarget(sessionName)
	args, added := buildMultiSetOptionArgs([]string{"-t", target}, tags)
	if added == 0 {
		return nil
	}
	// Guard travels inside the same invocation: has-session supports "=" exact
	// matching, and tmux aborts a ";"-chained command list when has-session
	// fails — so a dead session can't let set-option prefix-match a stranger,
	// and a live session costs one subprocess instead of two.
	args = append([]string{"has-session", "-t", sessionTarget(sessionName), ";"}, args...)

	cmd, cancel := tmuxCommand(opts, args...)
	defer cancel()
	output, err := runTmuxCmdCombined(cmd)
	if err != nil {
		if isExitCode1(err) {
			stderr := strings.TrimSpace(string(output))
			if isSessionNotFoundStderr(stderr) {
				return nil
			}
			return fmt.Errorf("set-option -t %s (multi): %s: %w", sessionName, stderr, err)
		}
		return err
	}
	return nil
}

// SetSessionTagValueForSessions sets one option on a snapshot of sessions in
// bounded batches. A single tmux client invocation handles each batch so owner
// heartbeat refresh does not fork one process per persistent tab.
func SetSessionTagValueForSessions(sessionNames []string, key, value string, opts Options) error {
	key = strings.TrimSpace(key)
	if len(sessionNames) == 0 || key == "" {
		return nil
	}
	if err := EnsureAvailable(); err != nil {
		return err
	}
	targets, err := sessionOptionTargets(sessionNames, opts)
	if err != nil {
		return err
	}
	const batchSize = 64
	for start := 0; start < len(targets); start += batchSize {
		end := min(start+batchSize, len(targets))
		args, added := buildSessionTagBatchArgs(targets[start:end], key, value)
		if added == 0 {
			continue
		}
		cmd, cancel := tmuxCommand(opts, args...)
		output, err := runTmuxCmdCombined(cmd)
		cancel()
		if err == nil {
			continue
		}
		if isExitCode1(err) {
			stderr := strings.TrimSpace(string(output))
			if isSessionNotFoundStderr(stderr) {
				continue
			}
			return fmt.Errorf("set-option on session batch: %s: %w", stderr, err)
		}
		return err
	}
	return nil
}

func sessionOptionTargets(sessionNames []string, opts Options) ([]string, error) {
	requested := make(map[string]struct{}, len(sessionNames))
	for _, rawName := range sessionNames {
		if name := strings.TrimSpace(rawName); name != "" {
			requested[name] = struct{}{}
		}
	}
	if len(requested) == 0 {
		return nil, nil
	}
	lines, err := listTmux(opts, "list-sessions", "-F", "#{session_name}\t#{session_id}")
	if err != nil {
		return nil, err
	}
	return parseSessionOptionTargets(requested, lines), nil
}

func parseSessionOptionTargets(requested map[string]struct{}, lines []string) []string {
	targets := make([]string, 0, len(requested))
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if _, ok := requested[name]; !ok {
			continue
		}
		id := strings.TrimSpace(parts[1])
		if !isSessionIDToken(id) {
			continue
		}
		targets = append(targets, id)
	}
	return targets
}

// sessionTagValueByID reads one session option against a "$N" session-id
// target. Id targets are exact by construction — tmux cannot prefix-match or
// rebind them — so unlike a bare-name read this cannot return a sibling's
// value when the named session died between resolution and read. A stale id
// surfaces as exit 1, which the read contract reports as an empty value.
func sessionTagValueByID(sessionID, key string, opts Options) (string, error) {
	cmd, cancel := tmuxCommand(opts, "show-options", "-t", sessionID, "-v", key)
	defer cancel()
	output, err := runTmuxCmd(cmd)
	if err != nil {
		if isExitCode1(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func buildSessionTagBatchArgs(sessionTargets []string, key, value string) ([]string, int) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, 0
	}
	args := make([]string, 0, len(sessionTargets)*7)
	added := 0
	for _, rawTarget := range sessionTargets {
		target := strings.TrimSpace(rawTarget)
		if target == "" {
			continue
		}
		if added > 0 {
			args = append(args, ";")
		}
		args = append(args, "set-option", "-t", target, key, value)
		added++
	}
	return args, added
}
