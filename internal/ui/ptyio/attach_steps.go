package ptyio

import (
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

// Shared attach/reattach orchestration steps. Center (agent tabs) and sidebar
// (terminal tabs) diverged here once already — a dead-session policy drift —
// so the policy points below are the single home for both: a semantics change
// lands here or nowhere. Tab/agent types and result messages stay per-package.

// SessionAttachable reports whether a session can be reattached to: it must
// exist AND have a live pane. This is THE dead-session policy for reattach —
// both panes refuse a missing or dead session with a Stopped result rather
// than silently killing it and recreating; restart is the explicit recreate
// path. (Center previously killed the dead session and recreated on reattach,
// which masked the distinction and lost the "session ended" signal.)
func SessionAttachable(state tmux.SessionState) bool {
	return state.Exists && state.HasLivePane
}

// SessionsWithTagsFn is the seam over tmux.SessionsWithTags that SessionOwned
// issues; tests stub it to script tag rows without a live tmux server.
var SessionsWithTagsFn = tmux.SessionsWithTags

// SessionOwned reports whether sessionName is claimed by amux for one of the
// given workspace ID forms: the session must carry @amux=1, and its
// @amux_workspace must equal a listed ID — an empty workspace tag is accepted
// as a pre-tagging-era session, mirroring FindRunSessions' empty-instance
// acceptance. It is the attach-path counterpart to the tag filters the
// discovery paths already apply: has-session only proves a NAME exists, and a
// foreign session squatting an amux session name (predictable on a shared
// tmux server) would otherwise be attached to and then re-tagged by the
// attach flow — laundering it into an amux session. This is namespace
// hygiene, not authentication: any actor on the same tmux server can read and
// forge session options, so the shared-server trust boundary stays where the
// docs put it (same-server actors are trusted).
func SessionOwned(sessionName string, wsIDs []string, opts tmux.Options) (bool, error) {
	rows, err := SessionsWithTagsFn(
		map[string]string{"@amux": "1"},
		[]string{"@amux_workspace"},
		opts,
	)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.Name != sessionName {
			continue
		}
		tagged := row.Tags["@amux_workspace"]
		if tagged == "" {
			return true, nil
		}
		for _, id := range wsIDs {
			if tagged == id {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}

// AttachSessionTags builds the tags an attach path stamps on its session.
// fresh (create/restart) stamps CreatedAt; reattach leaves it zero so the
// original creation time on the live session is preserved — SessionTags's
// CreatedAt contract is "zero for reattach". The LeaseAtMS heartbeat always
// refreshes: attaching is ownership activity.
func AttachSessionTags(ws *data.Workspace, tabID, sessionType, assistant, instanceID string, fresh bool) tmux.SessionTags {
	tags := tmux.SessionTags{
		WorkspaceID:   string(ws.ID()),
		TabID:         tabID,
		Type:          sessionType,
		Assistant:     assistant,
		InstanceID:    instanceID,
		SessionOwner:  instanceID,
		LeaseAtMS:     time.Now().UnixMilli(),
		WorkspaceName: ws.Name,
		ProjectName:   data.ProjectNameForRepo(ws.Repo),
	}
	if fresh {
		tags.CreatedAt = time.Now().Unix()
	}
	return tags
}

// FinalizeAttachScrollback performs the post-attach scrollback decision every
// attach path shares: when the bootstrap's full-pane snapshot still matches
// the pane (nothing new rendered while we attached), reuse it and take a
// post-attach capture for the delta; otherwise drop the snapshot and fall back
// to a bounded history capture. capturePane is the caller's pane-capture seam
// (each package keeps its own test seam).
//
// Returns scrollback bytes, the post-attach capture, whether the result
// carries a full-pane snapshot, the snapshot (zeroed on fallback), and the
// capture dimensions.
func FinalizeAttachScrollback(
	sessionName string,
	bootstrap SessionBootstrapCapture,
	attachWidth, attachHeight int,
	opts tmux.Options,
	capturePane func(string, tmux.Options) ([]byte, error),
) (scrollback, postAttach []byte, captureFullPane bool, snapshot tmux.PaneSnapshot, cols, rows int) {
	snapshot = bootstrap.Snapshot
	captureFullPane = bootstrap.CaptureFullPane
	cols, rows = attachWidth, attachHeight
	if captureFullPane && DefaultBootstrap().SnapshotStillMatches(sessionName, bootstrap, opts) {
		scrollback = snapshot.Data
		if capturePane != nil {
			postAttach, _ = capturePane(sessionName, opts)
		}
		return scrollback, postAttach, captureFullPane, snapshot, cols, rows
	}
	if captureFullPane {
		captureFullPane = false
		snapshot = tmux.PaneSnapshot{}
	}
	scrollback, cols, rows = DefaultBootstrap().CaptureHistory(sessionName, attachWidth, attachHeight, opts)
	return scrollback, postAttach, captureFullPane, snapshot, cols, rows
}
