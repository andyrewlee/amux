package app

import (
	"errors"
	"strconv"

	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/tmux"
)

// tmuxRunSessionHost is the process.RunSessionHost implementation over tmux:
// `run` scripts ride detached @amux_type=run sessions — scrollback survives
// the command, remain-on-exit preserves a crashed run's tail, and the session
// set maps cleanly onto Stop/IsRunning. process can't import tmux itself
// (tmux already imports process), so the wiring lives here at the app layer.
type tmuxRunSessionHost struct {
	opts       tmux.Options
	instanceID string
}

func newTmuxRunSessionHost(opts tmux.Options, instanceID string) *tmuxRunSessionHost {
	return &tmuxRunSessionHost{opts: opts, instanceID: instanceID}
}

var (
	_ process.RunSessionHost          = (*tmuxRunSessionHost)(nil)
	_ process.RunSessionCreator       = (*tmuxRunSessionHost)(nil)
	_ process.RunSessionFinder        = (*tmuxRunSessionHost)(nil)
	_ process.RunSessionStatusBatcher = (*tmuxRunSessionHost)(nil)
)

func (h *tmuxRunSessionHost) Ensure(name, workDir, cmd string, env []string, meta process.RunSessionMeta) error {
	return tmux.EnsureDetachedSession(name, workDir, cmd, env, h.opts, h.sessionTags(meta))
}

// Create is the create-only allocation path: tmux.CreateDetachedSession
// reports ErrSessionNameTaken on collision rather than adopting the existing
// session; the runner maps that to process.ErrRunSessionNameTaken and retries
// the next free suffix.
func (h *tmuxRunSessionHost) Create(name, workDir, cmd string, env []string, meta process.RunSessionMeta) error {
	err := tmux.CreateDetachedSession(name, workDir, cmd, env, h.opts, h.sessionTags(meta))
	if errors.Is(err, tmux.ErrSessionNameTaken) {
		return process.ErrRunSessionNameTaken
	}
	return err
}

func (h *tmuxRunSessionHost) sessionTags(meta process.RunSessionMeta) tmux.SessionTags {
	return tmux.SessionTags{
		WorkspaceID:   meta.WorkspaceID,
		Type:          "run",
		Assistant:     "run",
		CreatedAt:     meta.CreatedAt,
		InstanceID:    h.instanceID,
		SessionOwner:  h.instanceID,
		WorkspaceName: meta.WorkspaceName,
		ProjectName:   meta.ProjectName,
	}
}

func (h *tmuxRunSessionHost) Status(name string) (exists, alive bool, exitCode int, err error) {
	return tmux.RunSessionStatus(name, h.opts)
}

// StatusAll answers Status for every named session with one batched
// list-panes enumeration (tmux.AllSessionStates) instead of one
// display-message probe per name. Live sessions resolve entirely from the
// batch. A dead session's exit code is the one datum the enumeration does
// not carry — pane_dead_status and the pre-3.3 remain-on-exit banner both
// live behind RunSessionStatus — so only sessions whose active pane is dead
// pay a per-session probe, keeping the all-live poll at one subprocess.
func (h *tmuxRunSessionHost) StatusAll(names []string) (map[string]process.RunSessionState, error) {
	states, err := tmux.AllSessionStates(h.opts)
	if err != nil {
		return nil, err
	}
	out := make(map[string]process.RunSessionState, len(names))
	for _, name := range names {
		st, ok := states[name]
		if !ok {
			continue
		}
		if !st.ActivePaneLive {
			// Dead active pane — or a false negative on pane flags either way
			// corrected by the probe: RunSessionStatus asks the same question
			// of the session's current window and adds the exit code.
			exists, alive, exitCode, serr := tmux.RunSessionStatus(name, h.opts)
			if serr != nil || !exists {
				continue
			}
			out[name] = process.RunSessionState{Exists: true, Alive: alive, ExitCode: exitCode}
			continue
		}
		out[name] = process.RunSessionState{Exists: true, Alive: true, ExitCode: -1}
	}
	return out, nil
}

func (h *tmuxRunSessionHost) Kill(name string) error {
	return tmux.KillSession(name, h.opts)
}

func (h *tmuxRunSessionHost) Tail(name string, lines int) string {
	// RunSessionTail, not CapturePaneTail: the latter deliberately skips dead
	// panes, and a run's forensics live exactly in its remain-on-exit pane.
	out, ok := tmux.RunSessionTail(name, lines, h.opts)
	if !ok {
		return ""
	}
	return out
}

func (h *tmuxRunSessionHost) Find(workspaceID string) ([]string, error) {
	return tmux.FindRunSessions(workspaceID, h.instanceID, h.opts)
}

// FindDetailed is the creation-stamped find the newest-session picks order
// on — same ownership/instance filter as Find, plus each session's
// @amux_created_at (0 when the tag is absent, e.g. pre-tagging sessions).
func (h *tmuxRunSessionHost) FindDetailed(workspaceID string) ([]process.RunSessionRef, error) {
	rows, err := tmux.FindRunSessionsDetailed(workspaceID, h.instanceID, []string{"@amux_created_at"}, h.opts)
	if err != nil {
		return nil, err
	}
	refs := make([]process.RunSessionRef, 0, len(rows))
	for _, row := range rows {
		created, _ := strconv.ParseInt(row.Tags["@amux_created_at"], 10, 64)
		refs = append(refs, process.RunSessionRef{Name: row.Name, CreatedAt: created})
	}
	return refs, nil
}
