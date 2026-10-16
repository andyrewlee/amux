package process

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

// RunSessionEntry is one hosted run session's row for picker enumeration —
// its tmux session name plus the liveness/exit state remain-on-exit records.
type RunSessionEntry struct {
	Name     string
	Ordinal  int // 1 = base session, N = -N suffix — the run's creation order
	Alive    bool
	ExitCode int // -1 when unavailable (alive, or no recorded status)
}

// RunSessionList enumerates the workspace's run sessions with liveness, in
// creation order — the base session first, then -2, -3, … numerically (the
// find order is lexical, which would place run-10 before run-2). Unlike
// runSessionsHosted it applies no seen/config gate: the caller is a
// user-triggered picker open, where a configured-then-removed script must
// still show its leftover sessions.
func (r *ScriptRunner) RunSessionList(ws *data.Workspace) []RunSessionEntry {
	if r.runHost == nil || ws == nil {
		return nil
	}
	found, err := r.findRunSessions(ws)
	if err != nil {
		return nil
	}
	states, err := runSessionStates(r.runHost, found)
	if err != nil {
		return nil
	}
	base := runSessionBaseName(ws)
	entries := make([]RunSessionEntry, 0, len(found))
	for _, name := range found {
		st := states[name]
		if !st.Exists {
			continue
		}
		entries = append(entries, RunSessionEntry{Name: name, Ordinal: runSessionOrdinal(name, base), Alive: st.Alive, ExitCode: st.ExitCode})
	}
	sortRunSessionEntries(entries, base)
	return entries
}

// runSessionStates answers Status for every named session in one batch when
// the host implements RunSessionStatusBatcher — one subprocess for the whole
// set, with the dead-pane exit-code probe inside the batcher — and falls back
// to the per-session Status loop for hosts without it. A name absent from
// the map means the session does not exist (or its status probe failed); a
// batch error fails the whole query, matching the "report empty, don't retry
// serially" failure mode runSessionsHosted established.
func runSessionStates(host RunSessionHost, names []string) (map[string]RunSessionState, error) {
	if batcher, ok := host.(RunSessionStatusBatcher); ok {
		return batcher.StatusAll(names)
	}
	states := make(map[string]RunSessionState, len(names))
	for _, name := range names {
		exists, alive, exitCode, err := host.Status(name)
		if err != nil || !exists {
			continue
		}
		states[name] = RunSessionState{Exists: true, Alive: alive, ExitCode: exitCode}
	}
	return states, nil
}

// runSessionOrdinal is the session's creation-order index: the base name is
// 1, base-N is N. Sessions tagged under a drifted workspace-ID form won't
// prefix-match base, so those fall back to parsing the trailing -run/-run-N
// pattern off the name itself; anything else sorts last (shouldn't occur —
// every tagged name comes from this scheme, but a hand-made session could be
// mis-stamped).
func runSessionOrdinal(name, base string) int {
	if name == base {
		return 1
	}
	if rest, ok := strings.CutPrefix(name, base+"-"); ok {
		if n, err := strconv.Atoi(rest); err == nil && n > 0 {
			return n
		}
		return math.MaxInt
	}
	i := strings.LastIndex(name, "-run")
	if i < 0 {
		return math.MaxInt
	}
	suffix := name[i+len("-run"):]
	if suffix == "" {
		return 1
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(suffix, "-")); err == nil && n > 0 && strings.HasPrefix(suffix, "-") {
		return n
	}
	return math.MaxInt
}

// sortRunSessionEntries orders entries by creation order: base first, then
// numeric suffix — the order runScriptHosted minted them in.
func sortRunSessionEntries(entries []RunSessionEntry, base string) {
	sort.Slice(entries, func(i, j int) bool {
		oi, oj := runSessionOrdinal(entries[i].Name, base), runSessionOrdinal(entries[j].Name, base)
		if oi == oj {
			return entries[i].Name < entries[j].Name
		}
		return oi < oj
	})
}
