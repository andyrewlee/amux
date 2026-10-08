package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// runOutputTickMsg fires the periodic re-capture while the run-output viewer
// is open. Token matches the dialog instance it was scheduled for — a stale
// tick (dialog closed or reopened since) is dropped, not re-issued.
type runOutputTickMsg struct{ token int }

// runOutputRefreshedMsg carries one fetch's result back to the loop: the new
// tail content plus whether the session was still alive at fetch time (the
// last alive→dead fetch captures the remain-on-exit tail, then the loop
// stops).
type runOutputRefreshedMsg struct {
	token   int
	content string
	alive   bool
}

// scheduleRunOutputTick arms the next refresh for this dialog instance.
func (a *App) scheduleRunOutputTick(token int) tea.Cmd {
	return common.SafeTick(runOutputTickInterval, func(time.Time) tea.Msg {
		return runOutputTickMsg{token: token}
	})
}

// handleRunOutputTick turns a live tick into an off-loop fetch: tmux capture
// is a subprocess, so it must not run inside Update.
func (a *App) handleRunOutputTick(msg runOutputTickMsg) tea.Cmd {
	if msg.token != a.overlays.runOutputToken || a.overlays.runOutput == nil || !a.overlays.runOutput.Visible() {
		return nil
	}
	ws := a.overlays.runOutputWorkspace
	if ws == nil || a.workspaceService == nil {
		return nil
	}
	token, svc := a.overlays.runOutputToken, a.workspaceService
	session := a.overlays.runOutputSession
	snap := cloneForCmd(ws)
	return func() tea.Msg {
		var content string
		var alive bool
		if session != "" {
			content, alive = svc.RunScriptSessionTail(session, 400), svc.RunScriptSessionAlive(session)
		} else {
			content, alive, _ = svc.RunScriptOutputAndStatus(snap, 400)
		}
		return runOutputRefreshedMsg{token: token, content: content, alive: alive}
	}
}

// handleRunOutputRefreshed applies a fetch result: update the snapshot, then
// re-arm the tick only while the session is alive. A dead session's fetch is
// the final one — it already captured the remain-on-exit tail.
func (a *App) handleRunOutputRefreshed(msg runOutputRefreshedMsg) tea.Cmd {
	if msg.token != a.overlays.runOutputToken || a.overlays.runOutput == nil || !a.overlays.runOutput.Visible() {
		return nil
	}
	a.overlays.runOutput.SetContent(msg.content)
	if msg.alive {
		return a.scheduleRunOutputTick(a.overlays.runOutputToken)
	}
	return nil
}

// closeRunOutputDialog clears the viewer state and kills its refresh loop by
// bumping the token — an in-flight fetch or a scheduled tick then drops as
// stale instead of mutating a closed (or next) dialog.
func (a *App) closeRunOutputDialog() {
	a.overlays.runOutput = nil
	a.overlays.runOutputWorkspace = nil
	a.overlays.runOutputSession = ""
	a.overlays.runOutputAttachable = false
	a.overlays.runOutputReleaseCount = 0
	a.overlays.runOutputUntrustable = false
	a.overlays.runOutputToken++
}
