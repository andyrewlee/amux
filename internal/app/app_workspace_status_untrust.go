package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// untrustConfirmWord is the typed-confirm token for revoking a repo's script
// trust — long enough that it cannot be typed accidentally, short enough to
// be legible intent. The dialog validator and the result handler both gate
// on it (intent enforcement on both sides, same as the count check on the
// reservation-release flow).
const untrustConfirmWord = "untrust"

// untrustResultMsg carries the revoke outcome back onto the Update loop: the
// store write runs off-loop inside the result cmd, and a failure must surface
// (the grant stays in place — silence would read as a successful revoke).
type untrustResultMsg struct {
	workspace string
	err       error
}

// openUntrustScriptsDialog swaps the workspace-status viewer for the
// typed-confirm revoke gate. The viewer closes first — overlay arbitration
// forbids stacking a dialog over it — and the workspace is bound into the
// dialog context so the result handler revokes the repo it was opened for.
func (a *App) openUntrustScriptsDialog(ws *data.Workspace) {
	if ws == nil {
		return
	}
	a.closeRunOutputDialog()
	a.dialog = common.NewInputDialog(
		DialogUntrustScripts,
		"Revoke Script Trust",
		fmt.Sprintf("Type %q to confirm", untrustConfirmWord),
	)
	a.dialog.SetInputValidate(func(s string) string {
		if strings.TrimSpace(s) != untrustConfirmWord {
			return fmt.Sprintf("Type %q to confirm the revocation", untrustConfirmWord)
		}
		return ""
	})
	a.dlg = dialogContext{workspace: ws}
	a.presentDialog(a.dialog)
}

// dialogResultUntrustScripts runs the revoke on confirm. The typed value is
// re-gated against the confirm word (the validator only blocks Enter for the
// interactive path; a result arriving otherwise still cannot skip the gate),
// and the store write itself runs inside the cmd — off the Update loop, like
// every other filesystem touch in this flow. The store is fail-closed: an
// unreadable registry returns an error and the grant stays.
func dialogResultUntrustScripts(a *App, result common.DialogResult, dlg dialogContext) tea.Cmd {
	if !result.Confirmed || a.workspaceService == nil || dlg.workspace == nil {
		return nil
	}
	if strings.TrimSpace(result.Value) != untrustConfirmWord {
		return nil
	}
	svc := a.workspaceService
	ws := dlg.workspace
	return func() tea.Msg {
		return untrustResultMsg{workspace: ws.Name, err: svc.UntrustRepoScripts(ws.Repo)}
	}
}

// handleUntrustResult reports the revoke outcome. Success reads as "the next
// run re-prompts" — the gate re-arms on its own; failure is an Error toast
// because the user-initiated revoke did not happen and the grant remains.
func (a *App) handleUntrustResult(msg untrustResultMsg) tea.Cmd {
	if msg.err != nil {
		logging.Error("script trust revocation failed: %v", msg.err)
		return a.toast.ShowError("Revocation failed — trust unchanged: " + msg.err.Error())
	}
	return a.toast.ShowInfo("Repo script trust revoked for " + msg.workspace + " — the next run re-prompts")
}
