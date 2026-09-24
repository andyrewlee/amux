package app

import (
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

var _ process.RunSessionHost = (*tmuxRunSessionHost)(nil)

func (h *tmuxRunSessionHost) Ensure(name, workDir, cmd string, env []string, meta process.RunSessionMeta) error {
	return tmux.EnsureDetachedSession(name, workDir, cmd, env, h.opts, tmux.SessionTags{
		WorkspaceID:   meta.WorkspaceID,
		Type:          "run",
		Assistant:     "run",
		CreatedAt:     meta.CreatedAt,
		InstanceID:    h.instanceID,
		SessionOwner:  h.instanceID,
		WorkspaceName: meta.WorkspaceName,
		ProjectName:   meta.ProjectName,
	})
}

func (h *tmuxRunSessionHost) Status(name string) (exists, alive bool, exitCode int, err error) {
	return tmux.RunSessionStatus(name, h.opts)
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
