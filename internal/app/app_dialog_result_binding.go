package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// boundDialogResultMsg is a DialogResult emitted by a specific app-dialog
// instance, bound at emit time to that instance's seq and to the dialogContext
// that was current when the dialog produced the result. Because several Show*
// messages arrive asynchronously, a replacement dialog can open between a
// result's emit and its delivery — the seq is what detects that, and the bound
// dlg is what the result is allowed to act on.
type boundDialogResultMsg struct {
	seq    int
	dlg    dialogContext
	result common.DialogResult
}

// bindDialogResultCmd wraps a cmd the open app dialog produced so that a
// DialogResult it emits is delivered as boundDialogResultMsg carrying the
// producing instance's seq + context. Other message types pass through.
func bindDialogResultCmd(cmd tea.Cmd, seq int, dlg dialogContext) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		switch m := cmd().(type) {
		case common.DialogResult:
			return boundDialogResultMsg{seq: seq, dlg: dlg, result: m}
		case tea.BatchMsg:
			for i, inner := range m {
				m[i] = bindDialogResultCmd(inner, seq, dlg)
			}
			return m
		default:
			return m
		}
	}
}

// dialogOpen reports whether an app dialog is currently visible — the guard
// every handleShow* handler runs before touching a.dlg or a.dialog, so an
// async Show* can't silently replace a dialog the user is still reading.
func (a *App) dialogOpen() bool {
	return a.dialog != nil && a.dialog.Visible()
}

// clearPendingWorkspaceCreate drops the create→agent-picker handoff state.
// The picker owns it: set by dialogResultCreateWorkspace, consumed by
// dialogResultAgentPicker, cleared by the picker's cancel. Anything that
// displaces or supersedes the picker without a result must clear it too —
// otherwise a later plain assistant pick fires a stale CreateWorkspace.
func (a *App) clearPendingWorkspaceCreate() {
	a.pendingWorkspaceCreate.project = nil
	a.pendingWorkspaceCreate.name = ""
	a.pendingWorkspaceCreate.base = ""
}

func (a *App) handleDialogResultMsg(msg tea.Msg) (bool, tea.Cmd) {
	switch m := msg.(type) {
	case boundDialogResultMsg:
		if m.seq != a.dialogOpenSeq {
			// The dialog that produced this result has been replaced by a
			// newer instance. Applying it would act on the replacement's
			// context, and clearing a.dialog would destroy the live dialog —
			// drop it instead.
			if m.result.ID == common.AgentPickerDialogID {
				// The displaced picker never resolves — its create handoff
				// must die with it.
				a.clearPendingWorkspaceCreate()
			}
			logging.Debug("Dropping stale dialog result: id=%s seq=%d openSeq=%d", m.result.ID, m.seq, a.dialogOpenSeq)
			return true, nil
		}
		logging.Info("Received DialogResult: id=%s confirmed=%v", m.result.ID, m.result.Confirmed)
		a.dialog = nil
		a.dlg = dialogContext{}
		a.dialogOpenSeq = 0
		return true, common.SafeCmd(a.handleDialogResult(m.result, m.dlg))
	case common.DialogResult:
		logging.Info("Received DialogResult: id=%s confirmed=%v", m.ID, m.Confirmed)
		if isAppDialogID(m.ID) {
			// The only legitimate unbound producer of an app dialog ID is the
			// FilePicker (DialogAddProject): it is not a.dialog, so this path
			// must not clear a.dialog — and its handler reads result.Value,
			// never the dialog context.
			return true, common.SafeCmd(a.handleDialogResult(m, a.dlg))
		}
		// If not an App-level dialog, let it fall through to components.
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		return true, common.SafeCmd(cmd)
	}
	return false, nil
}
