package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// handleShowRunScriptOutput opens the read-only viewer with the workspace's
// current run output — the live pane tail while running, or the
// remain-on-exit tail after it exits. Concurrent run mode mints one session
// per launch, so the open enumerates first: zero sessions toasts "no output",
// one opens directly (the common fast path), and several open the run-session
// picker so earlier runs stay reachable. While the chosen session is alive
// the viewer re-captures its tail on runOutputTickInterval — a poll-follow
// pair to the dialog's `f` toggle.
func (a *App) handleShowRunScriptOutput(msg messages.ShowRunScriptOutput) tea.Cmd {
	if msg.Workspace == nil || a.workspaceService == nil {
		return nil
	}
	// The reads are tmux subprocess calls — never run them on the Update
	// loop (a wedged tmux would freeze the whole TUI for ~2 timeouts, the
	// defect class the status-indicator fix addressed). Bump the token
	// at request time so a second R press invalidates an in-flight open.
	a.overlays.runOutputToken++
	token, ws, svc := a.overlays.runOutputToken, msg.Workspace, a.workspaceService
	return func() tea.Msg {
		return runSessionsEnumeratedMsg{token: token, ws: ws, entries: svc.RunSessionList(ws)}
	}
}

// runSessionsEnumeratedMsg delivers the R-open enumeration: the workspace's
// hosted run sessions in creation order with liveness at sweep time.
type runSessionsEnumeratedMsg struct {
	token   int
	ws      *data.Workspace
	entries []process.RunSessionEntry
}

// handleRunSessionsEnumerated routes the enumeration under the token guard:
// empty keeps the historical "no output" toast, a single session opens the
// viewer directly, and a multi-session workspace raises the picker — carrying
// the rows in dlg.runSessions so the result handler maps Index → entry.
func (a *App) handleRunSessionsEnumerated(msg runSessionsEnumeratedMsg) tea.Cmd {
	if msg.token != a.overlays.runOutputToken || msg.ws == nil {
		return nil
	}
	switch len(msg.entries) {
	case 0:
		return a.toast.ShowInfo("No run output for " + msg.ws.Name)
	case 1:
		return a.fetchRunOutputCmd(msg.token, msg.ws, msg.entries[0])
	default:
		rows := make([]common.SessionPickerRow, len(msg.entries))
		for i, e := range msg.entries {
			rows[i] = common.SessionPickerRow{Label: runSessionRowLabel(e), Live: e.Alive}
		}
		a.requestOverlayOpen(func() {
			a.dlg.workspace = msg.ws
			a.dlg.runSessions = msg.entries
			a.dialog = common.NewRunSessionPicker("Run sessions — "+msg.ws.Name, "Pick a run to view (a attaches once open):", rows)
			a.presentDialog(a.dialog)
		})
		return nil
	}
}

// runSessionRowLabel is the picker's per-session text: creation ordinal, the
// session name (the row's raw tmux target — also what `tmux attach` would
// take), plus remain-on-exit status ("running", "exited 7", "exited 0").
func runSessionRowLabel(e process.RunSessionEntry) string {
	label := fmt.Sprintf("#%d %s", e.Ordinal, e.Name)
	if e.Alive {
		return label + " — running"
	}
	if e.ExitCode >= 0 {
		return fmt.Sprintf("%s — exited %d", label, e.ExitCode)
	}
	return label + " — exited"
}

// fetchRunOutputCmd tails one pinned session off-loop and reports it for the
// viewer-open path. A session that died between enumeration and selection
// still yields its remain-on-exit tail; one that vanished reports as gone.
func (a *App) fetchRunOutputCmd(token int, ws *data.Workspace, entry process.RunSessionEntry) tea.Cmd {
	svc := a.workspaceService
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		content := svc.RunScriptSessionTail(entry.Name, 400)
		alive := svc.RunScriptSessionAlive(entry.Name)
		return runOutputOpenedMsg{token: token, ws: ws, session: entry, content: content, alive: alive}
	}
}

// runOutputOpenedMsg delivers an R-open fetch result: the tail content, the
// pinned session it came from, and whether that session was alive at fetch
// time.
type runOutputOpenedMsg struct {
	token   int
	ws      *data.Workspace
	session process.RunSessionEntry
	content string
	alive   bool
}

// handleRunOutputOpened applies an open fetch under the token guard — a
// stale open (superseded by a later R/O press or close) is dropped, not
// applied to the newer dialog state.
func (a *App) handleRunOutputOpened(msg runOutputOpenedMsg) tea.Cmd {
	if msg.token != a.overlays.runOutputToken || msg.ws == nil {
		return nil
	}
	if msg.content == "" {
		return a.toast.ShowInfo("No run output for " + msg.ws.Name)
	}
	a.requestRunOutputOpen(func() {
		a.overlays.runOutputWorkspace = msg.ws
		a.overlays.runOutputSession = msg.session.Name
		a.overlays.runOutputAttachable = true
		a.overlays.runOutput = common.NewOutputDialog("Run output — "+msg.ws.Name+runSessionTitleSuffix(msg.session), msg.content)
		a.overlays.runOutput.SetAttachHint(true)
		a.overlays.runOutput.SetSize(a.width, a.height)
		a.overlays.runOutput.Show()
	})
	if msg.alive {
		return a.scheduleRunOutputTick(a.overlays.runOutputToken)
	}
	return nil
}

// runSessionTitleSuffix distinguishes which session a multi-run viewer is
// tailing (" #3"); the single-session fast path keeps the plain title.
func runSessionTitleSuffix(e process.RunSessionEntry) string {
	if e.Ordinal <= 1 {
		return ""
	}
	return fmt.Sprintf(" #%d", e.Ordinal)
}
