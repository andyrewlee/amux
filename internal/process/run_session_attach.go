package process

import "github.com/andyrewlee/amux/internal/data"

// RunSessionTail returns up to lines of the named run session's pane — the
// pinned-session variant of RunScriptOutput for the session picker.
func (r *ScriptRunner) RunSessionTail(name string, lines int) string {
	if r.runHost == nil || name == "" {
		return ""
	}
	return r.runHost.Tail(name, lines)
}

// RunSessionAlive reports whether the named run session's pane is still
// running — the attach path's recheck for a session that may have exited
// between enumeration and selection.
func (r *ScriptRunner) RunSessionAlive(name string) bool {
	if r.runHost == nil || name == "" {
		return false
	}
	exists, alive, _, err := r.runHost.Status(name)
	return err == nil && exists && alive
}

// RunScriptAttachTarget returns the newest ALIVE hosted run-session name for
// interactive attach — the same newest-alive selection the output overlay
// uses, ordered by creation stamp rather than suffix. False when no hosted
// session is alive or no session host is installed.
func (r *ScriptRunner) RunScriptAttachTarget(ws *data.Workspace) (string, bool) {
	r.mu.Lock()
	host := r.runHost
	r.mu.Unlock()
	if host == nil || ws == nil {
		return "", false
	}
	found, err := r.findRunSessionRefs(ws)
	if err != nil {
		return "", false
	}
	ordered := newestRunSessionFirst(found)
	names := make([]string, 0, len(ordered))
	for _, ref := range ordered {
		names = append(names, ref.Name)
	}
	states, err := runSessionStates(host, names)
	if err != nil {
		return "", false
	}
	for _, ref := range ordered {
		if st := states[ref.Name]; st.Exists && st.Alive {
			return ref.Name, true
		}
	}
	return "", false
}
