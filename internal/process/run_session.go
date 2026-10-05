package process

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
)

// RunSessionHost hosts a workspace's `run` script in a persistent, inspectable
// session instead of a naked subprocess — scrollback, reattach semantics, and
// an exit status that survives the command ending.
//
// The interface lives in process but is implemented at the app layer over
// internal/tmux: process cannot import tmux (tmux already imports process for
// KillProcessGroup). When nil, the runner falls back to the subprocess path.
// ErrRunSessionNameTaken reports a create-only allocation losing the name —
// a concurrent starter already owns it. The caller retries with the next
// free suffix rather than adopting the stranger's session.
var ErrRunSessionNameTaken = errors.New("run session name already taken")

// RunSessionCreator is the optional create-only allocation capability a
// RunSessionHost may implement: unlike Ensure it fails on a same-named
// existing session instead of silently adopting it (and re-stamping the
// winner's tags). runScriptHosted prefers Create when the host offers it;
// Ensure-only hosts keep the adopt-by-name fallback.
type RunSessionCreator interface {
	Create(name, workDir, cmd string, env []string, meta RunSessionMeta) error
}

// RunSessionRef is one hosted run session's identity plus its stamped
// creation time — what "newest session" picks order on. CreatedAt is 0 when
// the host doesn't report it; selection then falls back to name order.
type RunSessionRef struct {
	Name      string
	CreatedAt int64
}

// RunSessionFinder is the optional detailed-find capability: hosts
// implementing it return creation stamps alongside names so newest-session
// selection orders chronologically instead of by smallest-free suffix reuse
// (a recreated -2 is newer than a surviving -3 despite sorting earlier).
type RunSessionFinder interface {
	FindDetailed(workspaceID string) ([]RunSessionRef, error)
}

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

// findRunSessionRefs returns the workspace's run sessions under both
// identity forms, with creation stamps when the host implements
// RunSessionFinder. ws.ID() drifts across worktree create/remove
// (NormalizePath resolves only existing paths), so sessions tagged under one
// form become invisible to lookups made under the other; MetadataID() is the
// stable key new sessions are tagged with, ID() covers sessions created
// before that change and any keyed under the resolved form.
func (r *ScriptRunner) findRunSessionRefs(ws *data.Workspace) ([]RunSessionRef, error) {
	var out []RunSessionRef
	seen := make(map[string]struct{}, 4)
	finder, detailed := r.runHost.(RunSessionFinder)
	// Dedup the ID forms themselves, not just the names: for a persisted
	// workspace MetadataID() == ID() and the same Find would run twice —
	// two identical tmux list-sessions subprocesses per 3s poll.
	for _, id := range data.WorkspaceIdentitySet(ws) {
		var refs []RunSessionRef
		var err error
		if detailed {
			refs, err = finder.FindDetailed(string(id))
		} else {
			var names []string
			names, err = r.runHost.Find(string(id))
			for _, name := range names {
				refs = append(refs, RunSessionRef{Name: name})
			}
		}
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			if _, ok := seen[ref.Name]; ok {
				continue
			}
			seen[ref.Name] = struct{}{}
			out = append(out, ref)
		}
	}
	return out, nil
}

// findRunSessions is the names-only view of findRunSessionRefs for callers
// that don't order on creation time.
func (r *ScriptRunner) findRunSessions(ws *data.Workspace) ([]string, error) {
	refs, err := r.findRunSessionRefs(ws)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Name)
	}
	return names, nil
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

// runSessionSwept reports whether the one-shot post-restart probe already ran
// for any of the workspace's current identity forms. The seen-set is
// process-local, so a restarted amux can't tell "never had sessions" from
// "had them before the restart" — the swept-set bounds the discovery sweep to
// once per process rather than skipping it forever.
func (r *ScriptRunner) runSessionSwept(ws *data.Workspace) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range data.WorkspaceIdentitySet(ws) {
		if _, ok := r.runSessionsSwept[string(id)]; ok {
			return true
		}
	}
	return false
}

// markRunSessionSwept records every current identity form as probed.
func (r *ScriptRunner) markRunSessionSwept(ws *data.Workspace) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range data.WorkspaceIdentitySet(ws) {
		r.runSessionsSwept[string(id)] = struct{}{}
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
// sessions under the host), so this only names and allocates the next session
// — the base name, or -N when concurrent mode left earlier sessions alive.
// maxRunSessionCreateAttempts bounds the collision-retry loop in
// runScriptHosted: each loss means another starter owns that name, so a few
// retries always lands a free suffix unless something is pathologically
// contending.
const maxRunSessionCreateAttempts = 4

func (r *ScriptRunner) runScriptHosted(ws *data.Workspace, cmdStr string) error {
	env, err := r.buildScriptEnv(ws)
	if err != nil {
		return err
	}
	creator, canCreate := r.runHost.(RunSessionCreator)
	base := runSessionBaseName(ws)
	for attempt := 0; ; attempt++ {
		names, err := r.findRunSessions(ws)
		if err != nil {
			return fmt.Errorf("list run sessions: %w", err)
		}
		name := base
		if len(names) > 0 {
			name = fmt.Sprintf("%s-%d", name, nextRunSuffix(base, names))
		}
		// Create is create-or-collide: two concurrent starts can pick the
		// same free suffix, and with Ensure's adopt-by-name contract both
		// would "succeed" while only one command ran (and the loser's meta
		// would re-stamp the winner's). A collision retries the find for the
		// next free suffix; Ensure-only hosts keep the old adopt behavior.
		meta := RunSessionMeta{
			WorkspaceID:   string(ws.MetadataID()),
			CreatedAt:     time.Now().Unix(),
			WorkspaceName: ws.Name,
			ProjectName:   data.ProjectNameForRepo(ws.Repo),
		}
		if canCreate {
			err = creator.Create(name, ws.Root, cmdStr, env, meta)
		} else {
			err = r.runHost.Ensure(name, ws.Root, cmdStr, env, meta)
		}
		if errors.Is(err, ErrRunSessionNameTaken) {
			if attempt+1 >= maxRunSessionCreateAttempts {
				return fmt.Errorf("allocate run session name: %w", err)
			}
			continue
		}
		if err != nil {
			return err
		}
		if r.postStartHook != nil {
			r.postStartHook(scriptWorkspaceKey(ws))
		}
		// Post-start admission, the hosted form of RunScript's re-check: a
		// teardown that began while Ensure ran could not have included this
		// session in its stop sweep — kill it ourselves rather than leaving a
		// live run session in a worktree whose removal is already decided.
		if admErr := r.lifecycle.checkAdmission(scriptWorkspaceKey(ws)); admErr != nil {
			if killErr := r.runHost.Kill(name); killErr != nil {
				logging.Warn("post-admission kill of run session %s failed: %v", name, killErr)
			}
			return admErr
		}
		// A session created under this instance counts as observed even if a
		// later status sweep is the first to run — keeps the gate from
		// hiding it when the config is removed in between.
		r.markRunSessionSeen(ws)
		return nil
	}
}

// runSessionsHosted returns the workspace's live run-session statuses: any
// session that still exists, and whether any pane is still alive.
func (r *ScriptRunner) runSessionsHosted(ws *data.Workspace) (sessions []RunSessionRef, anyAlive bool, lastExit int) {
	lastExit = -1
	if r.runHost == nil {
		return nil, false, -1
	}
	// Cheap gate before the tmux sweep: a workspace with no configured run
	// script and no previously observed session can't have one. Config can be
	// removed while a session still lives, so the seen-set — populated on
	// every nonempty find — keeps the sweep running until the session is
	// actually gone. The seen-set is process-local, though: a restarted amux
	// can't distinguish "never had sessions" from "had them before the
	// restart", so the config-gone path gets ONE bounded sweep per process
	// (the swept-set) rather than skipping forever. Only
	// ErrNoScriptConfigured gates; an unreadable or untrusted repo config
	// still sweeps (fail open toward visibility).
	if _, err := r.resolveScriptCommand(ws, ScriptRun); errors.Is(err, ErrNoScriptConfigured) && !r.runSessionSeen(ws) {
		if r.runSessionSwept(ws) {
			return nil, false, -1
		}
		r.markRunSessionSwept(ws)
	}
	found, err := r.findRunSessionRefs(ws)
	if err != nil {
		return nil, false, -1
	}
	if len(found) > 0 {
		r.markRunSessionSeen(ws)
	}
	for _, ref := range found {
		exists, alive, exitCode, err := r.runHost.Status(ref.Name)
		if err != nil || !exists {
			continue
		}
		sessions = append(sessions, ref)
		if alive {
			anyAlive = true
		} else if exitCode != -1 {
			lastExit = exitCode
		}
	}
	return sessions, anyAlive, lastExit
}

// newestRunSessionFirst orders run-session refs newest-first: creation stamp
// descending, then numeric suffix descending as the same-second tiebreak
// (runSessionOrdinal with an empty base parses the -run/-run-N pattern off
// any prefix), then name. Unstamped hosts get suffix order — the "largest -N
// is newest" approximation the old find-order scan made — which is strictly
// better than the lexical last it replaced (-10 no longer loses to -2).
func newestRunSessionFirst(refs []RunSessionRef) []RunSessionRef {
	sorted := make([]RunSessionRef, len(refs))
	copy(sorted, refs)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].CreatedAt != sorted[j].CreatedAt {
			return sorted[i].CreatedAt > sorted[j].CreatedAt
		}
		if oi, oj := runSessionOrdinal(sorted[i].Name, ""), runSessionOrdinal(sorted[j].Name, ""); oi != oj {
			return oi > oj
		}
		return sorted[i].Name > sorted[j].Name
	})
	return sorted
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
	sessions, _, _ := r.runSessionsHosted(ws)
	if len(sessions) == 0 {
		return ""
	}
	// The newest-created session is the most relevant view — creation order,
	// not suffix order: a gap-reused -2 can be newer than a surviving -3.
	return r.runHost.Tail(newestRunSessionFirst(sessions)[0].Name, lines)
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
	sessions, alive, exit := r.runSessionsHosted(ws)
	if len(sessions) > 0 {
		output = r.runHost.Tail(newestRunSessionFirst(sessions)[0].Name, lines)
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
	for _, ref := range newestRunSessionFirst(found) {
		exists, alive, _, err := host.Status(ref.Name)
		if err == nil && exists && alive {
			return ref.Name, true
		}
	}
	return "", false
}
