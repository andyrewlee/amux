package tmux

import "strings"

type SessionState struct {
	Exists      bool
	HasLivePane bool
	// ActivePaneLive reports whether the session's current window has a live
	// active pane — the same question CapturePaneTail's display-message probe
	// asks, answered here for free by the batched list-panes call so callers
	// can skip the per-session probe.
	ActivePaneLive bool
}

// AllSessionStates returns the SessionState for every tmux session on the
// server in a single subprocess call.  It runs:
//
//	tmux list-panes -a -F "#{session_name}\t#{pane_dead}\t#{pane_active}\t#{window_active}"
//
// Sessions that appear in output have Exists=true.  Any session with at
// least one pane where pane_dead is "0" gets HasLivePane=true; the pane that
// is both window-active and pane-active with pane_dead=="0" marks the
// session's ActivePaneLive. If there are no sessions at all (exit code 1),
// an empty map is returned.
func AllSessionStates(opts Options) (map[string]SessionState, error) {
	if err := EnsureAvailable(); err != nil {
		return nil, err
	}
	lines, err := listTmux(opts, "list-panes", "-a", "-F", "#{session_name}\t#{pane_dead}\t#{pane_active}\t#{window_active}")
	if err != nil {
		return nil, err
	}
	return parseSessionStates(lines), nil
}

// parseSessionStates is the pure parse/aggregate half of AllSessionStates. It
// takes the raw `list-panes -a -F` output (session, pane_dead, pane_active,
// window_active — the last two optional for forward compatibility) and
// returns one SessionState per session: Exists is true for any session that
// appears, and HasLivePane is true once any of the session's panes reports
// pane_dead=="0". Aggregating across multiple panes per session is the
// genuinely bug-prone part, so it lives here to be unit-tested without a live
// tmux server.
func parseSessionStates(lines []string) map[string]SessionState {
	states := make(map[string]SessionState)
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		dead := parts[1]
		st := states[name]
		st.Exists = true
		if dead == "0" {
			st.HasLivePane = true
			// ActivePaneLive answers "is the session's current window's active
			// pane alive" — the pane both window_active and pane_active — which
			// is what `display-message -t <session> '#{pane_dead}'` probes.
			if len(parts) == 4 && parts[2] == "1" && parts[3] == "1" {
				st.ActivePaneLive = true
			}
		}
		states[name] = st
	}
	return states
}
