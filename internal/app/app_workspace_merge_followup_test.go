package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// mergeAppWithWorkspace registers the workspace in the live model so the
// post-merge follow-up dialog's row-existence check passes.
func mergeAppWithWorkspace(headBranch string, ws *data.Workspace) *App {
	app := newMergeApp(headBranch)
	app.projects = []data.Project{{
		Name:       "proj",
		Path:       ws.Repo,
		Workspaces: []data.Workspace{*ws},
	}}
	return app
}

// TestHandleWorkspaceMerged_OffersFollowUp proves a successful merge whose
// workspace row is still live opens the keep/shelve/delete picker.
func TestHandleWorkspaceMerged_OffersFollowUp(t *testing.T) {
	ws := mergeWorkspace()
	app := mergeAppWithWorkspace("main", ws)

	app.handleWorkspaceMerged(messages.WorkspaceMerged{Workspace: ws, Base: "main"})
	if app.dialog == nil || !app.dialog.Visible() {
		t.Fatal("merge success opened no follow-up dialog")
	}
}

// TestHandleWorkspaceMerged_ForeignRowGetsNoDialog proves the offer is gated
// on the row existing in the live model — a merge completion for a workspace
// that is gone (or from another app instance) leaves no dangling dialog.
func TestHandleWorkspaceMerged_ForeignRowGetsNoDialog(t *testing.T) {
	ws := mergeWorkspace()
	app := newMergeApp("main") // no projects — the row is not in the model

	app.handleWorkspaceMerged(messages.WorkspaceMerged{Workspace: ws, Base: "main"})
	if app.dialog != nil {
		t.Fatal("follow-up dialog opened for a workspace not in the model")
	}
}

// TestDialogResultMergedWorkspace_RoutesEachPick drives the three follow-up
// outcomes: shelve emits the existing shelve message, delete re-opens the
// real delete confirm (its gate is not bypassed), keep does nothing.
func TestDialogResultMergedWorkspace_RoutesEachPick(t *testing.T) {
	ws := mergeWorkspace()
	app := mergeAppWithWorkspace("main", ws)
	dlg := dialogContext{project: &app.projects[0], workspace: ws}

	// Shelve: same ShelveWorkspace the dedicated confirm emits.
	cmd := dialogResultMergedWorkspace(app, common.DialogResult{
		ID: DialogMergedWorkspace, Confirmed: true, Index: 1,
	}, dlg)
	if cmd == nil {
		t.Fatal("shelve pick produced no command")
	}
	if _, ok := cmd().(messages.ShelveWorkspace); !ok {
		t.Fatalf("shelve pick emitted %T, want ShelveWorkspace", cmd())
	}

	// Delete: the existing single-delete confirm opens — not a direct delete.
	app.dialog = nil
	dialogResultMergedWorkspace(app, common.DialogResult{
		ID: DialogMergedWorkspace, Confirmed: true, Index: 2,
	}, dlg)
	if app.dialog == nil || !app.dialog.Visible() {
		t.Fatal("delete pick opened no confirm dialog")
	}

	// Keep / cancel: inert.
	app.dialog = nil
	if cmd := dialogResultMergedWorkspace(app, common.DialogResult{
		ID: DialogMergedWorkspace, Confirmed: true, Index: 0,
	}, dlg); cmd != nil {
		t.Fatal("keep pick produced a command")
	}
	if cmd := dialogResultMergedWorkspace(app, common.DialogResult{
		ID: DialogMergedWorkspace, Confirmed: false, Index: 1,
	}, dlg); cmd != nil {
		t.Fatal("cancel produced a command")
	}
}
