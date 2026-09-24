package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestShowDialog_RejectsWhileVisible pins the plan-082 displacement guard:
// an async Show* (trust prompt racing a user-opened confirm) must not
// replace the visible dialog or overwrite its context.
func TestShowDialog_RejectsWhileVisible(t *testing.T) {
	h := newDialogHarness(t)
	wsA := &data.Workspace{Name: "ws-a", Root: "/tmp/ws-a"}
	wsB := &data.Workspace{Name: "ws-b", Root: "/tmp/ws-b", Repo: t.TempDir()}

	h.app.handleShowDeleteWorkspaceDialog(messages.ShowDeleteWorkspaceDialog{
		Project:   &data.Project{Path: "/tmp/repo"},
		Workspace: wsA,
	})
	live := h.app.dialog
	if live == nil || !live.Visible() {
		t.Fatal("expected delete dialog to be visible")
	}

	h.app.handleShowTrustScriptsDialog(messages.ShowTrustScriptsDialog{Workspace: wsB})

	if h.app.dialog != live {
		t.Fatal("async Show replaced the visible dialog")
	}
	if h.app.dlg.workspace != wsA {
		t.Fatalf("rejected Show overwrote the open dialog's context: %+v", h.app.dlg.workspace)
	}
}

// TestShowDialog_PickerChainStillWorks proves the guard doesn't break the
// legit back-to-back case: create-workspace result consumes the dialog
// (a.dialog nil'd) before the picker's Show lands.
func TestShowDialog_PickerChainStillWorks(t *testing.T) {
	h := newDialogHarness(t)
	proj := &data.Project{Path: "/tmp/repo", Name: "repo"}

	h.app.handleShowCreateWorkspaceDialog(messages.ShowCreateWorkspaceDialog{Project: proj})
	handled, cmd := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    h.app.dialogOpenSeq,
		dlg:    dialogContext{project: proj},
		result: common.DialogResult{ID: DialogCreateWorkspace, Confirmed: true, Value: "ws-new"},
	})
	if !handled || cmd == nil {
		t.Fatal("expected create result to emit the picker Show cmd")
	}
	if _, ok := cmd().(messages.ShowSelectAssistantDialog); !ok {
		t.Fatal("create result did not emit ShowSelectAssistantDialog")
	}
	if h.app.dialog != nil {
		t.Fatal("consumed create dialog must be gone before the picker shows")
	}

	h.app.handleShowSelectAssistantDialog()
	if h.app.dialog == nil || !h.app.dialog.Visible() {
		t.Fatal("picker must still open on the legit create→picker chain")
	}
}

// TestPendingWorkspaceCreate_ClearedOnDisplacement proves a picker displaced
// without a result can't leave a live create handoff: the next plain picker
// pick emits LaunchAgent, never a stale CreateWorkspace.
func TestPendingWorkspaceCreate_ClearedOnDisplacement(t *testing.T) {
	h := newDialogHarness(t)
	proj := &data.Project{Path: "/tmp/repo", Name: "repo"}
	ws := &data.Workspace{Name: "ws-a", Root: "/tmp/ws-a"}

	h.app.pendingWorkspaceCreate.project = proj
	h.app.pendingWorkspaceCreate.name = "ws-new"
	h.app.handleShowSelectAssistantDialog()
	if h.app.dialog == nil || !h.app.dialog.Visible() {
		t.Fatal("expected the picker to open")
	}

	// The picker is displaced (its result never arrives) and another dialog
	// opens — the stale handoff must die.
	h.app.dialog.Hide()
	h.app.handleShowDeleteWorkspaceDialog(messages.ShowDeleteWorkspaceDialog{
		Project:   proj,
		Workspace: ws,
	})
	if h.app.pendingWorkspaceCreate.name != "" {
		t.Fatal("displacement left pendingWorkspaceCreate alive")
	}

	// Dismiss the delete dialog and open a plain picker: the pick must
	// produce LaunchAgent for the active workspace, not CreateWorkspace.
	h.app.dialog.Hide()
	h.app.dialog = nil
	h.app.activeWorkspace = ws
	h.app.handleShowSelectAssistantDialog()
	if h.app.dialog == nil || !h.app.dialog.Visible() {
		t.Fatal("expected the picker to open")
	}
	handled, cmd := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    h.app.dialogOpenSeq,
		result: common.DialogResult{ID: common.AgentPickerDialogID, Confirmed: true, Value: h.app.assistantNames()[0]},
	})
	if !handled || cmd == nil {
		t.Fatal("expected picker result to emit a cmd")
	}
	msg := cmd()
	if _, bad := msg.(messages.CreateWorkspace); bad {
		t.Fatalf("stale handoff fired CreateWorkspace: %+v", msg)
	}
	launch, ok := msg.(messages.LaunchAgent)
	if !ok {
		t.Fatalf("expected messages.LaunchAgent, got %T", msg)
	}
	if launch.Workspace != ws {
		t.Fatalf("LaunchAgent targeted %v, want active workspace", launch.Workspace)
	}
}
