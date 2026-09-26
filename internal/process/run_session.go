package process

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// RunSessionHost hosts a workspace's `run` script in a persistent, inspectable
// session instead of a naked subprocess — scrollback, reattach semantics, and
// an exit status that survives the command ending.
//
// The interface lives in process but is implemented at the app layer over
// internal/tmux: process cannot import tmux (tmux already imports process for
// KillProcessGroup). When nil, the runner falls back to the subprocess path.
type RunSessionHost interface {
	// Ensure starts a detached session named name running cmd in workDir with
	// env; a no-op when the session already exists.
	Ensure(name, workDir, cmd string, env []string, meta RunSessionMeta) error
	// Status reports the named session: exists, pane-alive, and the recorded
	// exit code when it finished (-1 when unavailable).
	Status(name string) (exists, alive bool, exitCode int, err error)
	// Kill ends the named session; nil error when it is already gone.
	Kill(name string) error
	// Tail returns up to lines of the session's pane content; "" when the
	// session is gone or capture fails.
	Tail(name string, lines int) string
	// Find returns the run-session names owned by workspaceID (multiple under
	// concurrent mode).
	Find(workspaceID string) ([]string, error)
}

// RunSessionMeta carries the session metadata the host stamps on each run
// session (mapped to @amux_* tags by the tmux implementation).
type RunSessionMeta struct {
	WorkspaceID string
	CreatedAt   int64
	InstanceID  string
	// WorkspaceName/ProjectName are display-only labels for external
	// orchestrators (@amux_workspace_name/@amux_project) — never identity.
	WorkspaceName string
	ProjectName   string
}

// runSessionBaseName is the deterministic name for a workspace's primary run
// session; concurrent starts append -2, -3, …
func runSessionBaseName(ws *data.Workspace) string {
	return "amux-ws-" + string(ws.ID()) + "-run"
}

// SetRunHost installs the session host used by RunScript/Stop/IsRunning.
// Called once at app init; tests that leave it nil keep the subprocess path.
func (r *ScriptRunner) SetRunHost(host RunSessionHost) {
	r.mu.Lock()
	r.runHost = host
	r.mu.Unlock()
}

// RunHosted reports whether a session host is installed (run scripts ride
// tmux sessions rather than subprocesses).
func (r *ScriptRunner) RunHosted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runHost != nil
}

// findRunSessions returns the workspace's run-session names under both
// identity forms. ws.ID() drifts across worktree create/remove (NormalizePath
// resolves only existing paths), so sessions tagged under one form become
// invisible to lookups made under the other; MetadataID() is the stable key
// new sessions are tagged with, ID() covers sessions created before that
// change and any keyed under the resolved form.
func (r *ScriptRunner) findRunSessions(ws *data.Workspace) ([]string, error) {
	var out []string
	seen := make(map[string]struct{}, 4)
	// Dedup the ID forms themselves, not just the names: for a persisted
	// workspace MetadataID() == ID() and the same Find would run twice —
	// two identical tmux list-sessions subprocesses per 3s poll.
	for _, id := range data.WorkspaceIdentitySet(ws) {
		names, err := r.runHost.Find(string(id))
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	return out, nil
}

// runSessionSeen reports whether a find ever returned sessions under any of
// the workspace's current identity forms.
func (r *ScriptRunner) runSessionSeen(ws *data.Workspace) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range data.WorkspaceIdentitySet(ws) {
		if _, ok := r.runSessionsSeen[string(id)]; ok {
			return true
		}
	}
	return false
}

// markRunSessionSeen records every current identity form so a later identity
// drift still recognizes the workspace as having had sessions.
func (r *ScriptRunner) markRunSessionSeen(ws *data.Workspace) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range data.WorkspaceIdentitySet(ws) {
		r.runSessionsSeen[string(id)] = struct{}{}
	}
}

// nextRunSuffix picks the smallest free -N suffix (N ≥ 2) for base among the
// live session names. Smallest-free — not len+1 or max+1 — because a killed
// middle session leaves a gap (base + base-3 alive), and len+1 would collide
// on base-3: Ensure is create-unless-present, so a collision silently starts
// nothing. Reusing dead slots also keeps names dense, which keeps the lexical
// find order close to creation order for the "newest = last name" helpers.
func nextRunSuffix(base string, names []string) int {
	occupied := make(map[int]struct{}, len(names))
	for _, name := range names {
		rest, ok := strings.CutPrefix(name, base+"-")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(rest)
		if err != nil || n <= 0 {
			continue
		}
		occupied[n] = struct{}{}
	}
	for n := 2; ; n++ {
		if _, taken := occupied[n]; !taken {
			return n
		}
	}
}

// runScriptHosted is RunScript's session-host path. RunScript's nonconcurrent
// branch already routed through Stop (which kills all the workspace's run
// sessions under the host), so this only names the next session — the base
// name, or -N when concurrent mode left earlier sessions alive — and ensures
// it exists.
func (r *ScriptRunner) runScriptHosted(ws *data.Workspace, cmdStr string) error {
	names, err := r.findRunSessions(ws)
	if err != nil {
		return fmt.Errorf("list run sessions: %w", err)
	}
	name := runSessionBaseName(ws)
	if len(names) > 0 {
		name = fmt.Sprintf("%s-%d", name, nextRunSuffix(name, names))
	}
	env, err := r.buildScriptEnv(ws)
	if err != nil {
		return err
	}
	err = r.runHost.Ensure(name, ws.Root, cmdStr, env, RunSessionMeta{
		WorkspaceID:   string(ws.MetadataID()),
		CreatedAt:     time.Now().Unix(),
		WorkspaceName: ws.Name,
		ProjectName:   data.ProjectNameForRepo(ws.Repo),
	})
	if err == nil {
		// A session created under this instance counts as observed even if a
		// later status sweep is the first to run — keeps the gate from
		// hiding it when the config is removed in between.
		r.markRunSessionSeen(ws)
	}
	return err
}

// runSessionsHosted returns the workspace's live run-session statuses: any
// session that still exists, and whether any pane is still alive.
func (r *ScriptRunner) runSessionsHosted(ws *data.Workspace) (names []string, anyAlive bool, lastExit int) {
	lastExit = -1
	if r.runHost == nil {
		return nil, false, -1
	}
	// Cheap gate before the tmux sweep: a workspace with no configured run
	// script and no previously observed session can't have one. Config can be
	// removed while a session still lives, so the seen-set — populated on
	// every nonempty find — keeps the sweep running until the session is
	// actually gone. Only ErrNoScriptConfigured skips; an unreadable or
	// untrusted repo config still sweeps (fail open toward visibility).
	if _, err := r.resolveScriptCommand(ws, ScriptRun); errors.Is(err, ErrNoScriptConfigured) && !r.runSessionSeen(ws) {
		return nil, false, -1
	}
	found, err := r.findRunSessions(ws)
	if err != nil {
		return nil, false, -1
	}
	if len(found) > 0 {
		r.markRunSessionSeen(ws)
	}
	for _, name := range found {
		exists, alive, exitCode, err := r.runHost.Status(name)
		if err != nil || !exists {
			continue
		}
		names = append(names, name)
		if alive {
			anyAlive = true
		} else if exitCode != -1 {
			lastExit = exitCode
		}
	}
	return names, anyAlive, lastExit
}

// RunScriptStatus reports the workspace's hosted run state for UI surfacing:
// anyAlive mirrors IsRunning; lastExit is the most recent dead session's exit
// code (-1 when no dead session or no host). Callers use it to turn a
// running→stopped transition into a "exited with status N" notice.
func (r *ScriptRunner) RunScriptStatus(ws *data.Workspace) (anyAlive bool, lastExit int) {
	if !r.RunHosted() {
		return r.RunActive(ws), -1
	}
	_, alive, exit := r.runSessionsHosted(ws)
	return alive, exit
}

// RunScriptOutput returns up to lines of the workspace's run-session output —
// live pane content while running, the remain-on-exit tail after it stops.
// Empty when nothing has run or the output is unavailable.
func (r *ScriptRunner) RunScriptOutput(ws *data.Workspace, lines int) string {
	if !r.RunHosted() || ws == nil {
		return ""
	}
	names, _, _ := r.runSessionsHosted(ws)
	if len(names) == 0 {
		return ""
	}
	// The last-created session is the most relevant view; names arrive in
	// find order and the base session (no suffix) is oldest.
	return r.runHost.Tail(names[len(names)-1], lines)
}

// RunScriptOutputAndStatus is the combined fetch for callers that need both
// the tail and the status (the run-output viewer's open/refresh): it spends
// ONE hosted sweep instead of RunScriptOutput + RunScriptStatus each running
// their own full tmux scan.
func (r *ScriptRunner) RunScriptOutputAndStatus(ws *data.Workspace, lines int) (output string, anyAlive bool, lastExit int) {
	if ws == nil {
		return "", false, -1
	}
	if !r.RunHosted() {
		return "", r.RunActive(ws), -1
	}
	names, alive, exit := r.runSessionsHosted(ws)
	if len(names) > 0 {
		output = r.runHost.Tail(names[len(names)-1], lines)
	}
	return output, alive, exit
}

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
	base := runSessionBaseName(ws)
	entries := make([]RunSessionEntry, 0, len(found))
	for _, name := range found {
		exists, alive, exitCode, err := r.runHost.Status(name)
		if err != nil || !exists {
			continue
		}
		entries = append(entries, RunSessionEntry{Name: name, Ordinal: runSessionOrdinal(name, base), Alive: alive, ExitCode: exitCode})
	}
	sortRunSessionEntries(entries, base)
	return entries
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

// RunScriptAttachTarget returns the newest ALIVE hosted run-session name for
// interactive attach — the same newest-alive selection the output overlay
// uses (find order is oldest-first). False when no hosted session is alive
// or no session host is installed.
func (r *ScriptRunner) RunScriptAttachTarget(ws *data.Workspace) (string, bool) {
	r.mu.Lock()
	host := r.runHost
	r.mu.Unlock()
	if host == nil || ws == nil {
		return "", false
	}
	found, err := r.findRunSessions(ws)
	if err != nil {
		return "", false
	}
	for i := len(found) - 1; i >= 0; i-- {
		exists, alive, _, err := host.Status(found[i])
		if err == nil && exists && alive {
			return found[i], true
		}
	}
	return "", false
}
