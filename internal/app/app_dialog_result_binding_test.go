package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// emitDialogResult feeds the open app dialog keys and returns the
// boundDialogResultMsg its produced cmd emits — the real emit path.
func emitDialogResult(t *testing.T, a *App, keys ...tea.KeyPressMsg) boundDialogResultMsg {
	t.Helper()
	var cmds []tea.Cmd
	for _, key := range keys {
		if !a.handleDialogInput(key, &cmds) {
			t.Fatalf("expected the open dialog to consume key %v", key)
		}
	}
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		if m, ok := cmd().(boundDialogResultMsg); ok {
			return m
		}
	}
	t.Fatal("dialog did not emit a bound DialogResult")
	return boundDialogResultMsg{}
}

// confirmYes moves the confirm dialog's cursor to "Yes" (it defaults to "No"
// for destructive safety) and confirms.
func confirmYes() []tea.KeyPressMsg {
	return []tea.KeyPressMsg{
		{Code: 'h', Text: "h"},
		{Code: tea.KeyEnter},
	}
}

func newDialogTestWorkspace(name string) *data.Workspace {
	return &data.Workspace{Name: name, Root: "/tmp/" + name}
}

func TestDialogResult_StaleAfterReplacement_NotApplied(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	wsA := newDialogTestWorkspace("ws-a")
	wsB := newDialogTestWorkspace("ws-b")

	// Dialog A opens (seq 1) and the user confirms it — the result cmd is
	// bound to instance 1 at emit time.
	h.app.handleShowDeleteWorkspaceDialog(messages.ShowDeleteWorkspaceDialog{
		Project:   &data.Project{Path: "/tmp/repo"},
		Workspace: wsA,
	})
	if h.app.dialogOpenSeq != 1 {
		t.Fatalf("expected open seq 1, got %d", h.app.dialogOpenSeq)
	}
	stale := emitDialogResult(t, h.app, confirmYes()...)
	if stale.result.ID != DialogDeleteWorkspace || !stale.result.Confirmed {
		t.Fatalf("expected confirmed delete result, got %+v", stale.result)
	}

	// Before that cmd's msg lands, the dialog is displaced (a guarded Show*
	// can't replace a visible dialog, so hide first — displacement happens
	// only once the open instance is gone).
	h.app.dialog.Hide()
	h.app.handleShowShelveWorkspaceDialog(messages.ShowShelveWorkspaceDialog{
		Project:   &data.Project{Path: "/tmp/repo"},
		Workspace: wsB,
	})
	if h.app.dialogOpenSeq == stale.seq {
		t.Fatalf("expected replacement to advance open seq past %d", stale.seq)
	}
	liveDialog := h.app.dialog

	handled, cmd := h.app.handleDialogResultMsg(stale)
	if !handled {
		t.Fatal("expected bound result msg to be handled")
	}
	if cmd != nil {
		t.Fatalf("stale result must not produce a cmd, got %v", cmd)
	}
	if h.app.dialog != liveDialog || !h.app.dialog.Visible() {
		t.Fatal("stale result must not clear or replace the live dialog")
	}
	if h.app.dlg.workspace != wsB {
		t.Fatalf("stale result must not disturb the live dialog's context, got %+v", h.app.dlg.workspace)
	}
}

func TestDialogResult_ContextBoundAtEmit(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	wsA := newDialogTestWorkspace("ws-a")
	wsB := newDialogTestWorkspace("ws-b")
	proj := &data.Project{Path: "/tmp/repo"}

	h.app.handleShowDeleteWorkspaceDialog(messages.ShowDeleteWorkspaceDialog{Project: proj, Workspace: wsA})
	bound := emitDialogResult(t, h.app, confirmYes()...)
	if bound.dlg.workspace != wsA {
		t.Fatalf("bound context must capture the producing dialog's workspace, got %+v", bound.dlg.workspace)
	}

	// A replacement overwrites a.dlg — the bound snapshot must still hold wsA.
	h.app.dialog.Hide()
	h.app.handleShowDeleteWorkspaceDialog(messages.ShowDeleteWorkspaceDialog{Project: proj, Workspace: wsB})
	if bound.dlg.workspace != wsA {
		t.Fatalf("bound context mutated after replacement: %+v", bound.dlg.workspace)
	}

	// The live dialog's own result applies with its own bound context.
	live := emitDialogResult(t, h.app, confirmYes()...)
	handled, cmd := h.app.handleDialogResultMsg(live)
	if !handled || cmd == nil {
		t.Fatal("expected live dialog result to be applied")
	}
	msg, ok := cmd().(messages.DeleteWorkspace)
	if !ok {
		t.Fatalf("expected messages.DeleteWorkspace, got %T", cmd())
	}
	if msg.Workspace != wsB {
		t.Fatalf("live result must act on its own context wsB, got %v", msg.Workspace)
	}
}

func TestDialogResult_ChainedPickerStillWorks(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	proj := &data.Project{Path: "/tmp/repo", Name: "repo"}

	// Create-workspace dialog is open; deliver its confirmed result bound to
	// the live instance.
	h.app.handleShowCreateWorkspaceDialog(messages.ShowCreateWorkspaceDialog{Project: proj})
	seq := h.app.dialogOpenSeq
	handled, cmd := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    seq,
		dlg:    dialogContext{project: proj},
		result: common.DialogResult{ID: DialogCreateWorkspace, Confirmed: true, Value: "ws-new"},
	})
	if !handled || cmd == nil {
		t.Fatal("expected create result to produce the assistant-picker cmd")
	}
	if _, ok := cmd().(messages.ShowSelectAssistantDialog); !ok {
		t.Fatalf("expected ShowSelectAssistantDialog, got %T", cmd())
	}

	// The chained picker presents as a NEW instance (seq 2) — its own result
	// must still be honored.
	h.app.handleShowSelectAssistantDialog()
	if h.app.dialogOpenSeq == seq {
		t.Fatal("expected picker presentation to advance the dialog seq")
	}
	if h.app.pendingWorkspaceCreate.name != "ws-new" {
		t.Fatalf("pending create not staged, got %+v", h.app.pendingWorkspaceCreate)
	}
	handled, cmd = h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    h.app.dialogOpenSeq,
		result: common.DialogResult{ID: common.AgentPickerDialogID, Confirmed: true, Value: h.app.assistantNames()[0]},
	})
	if !handled || cmd == nil {
		t.Fatal("expected picker result to produce a CreateWorkspace cmd")
	}
	cw, ok := cmd().(messages.CreateWorkspace)
	if !ok {
		t.Fatalf("expected messages.CreateWorkspace, got %T", cmd())
	}
	if cw.Name != "ws-new" || cw.Project != proj {
		t.Fatalf("picker result lost the pending create context: %+v", cw)
	}
	if h.app.pendingWorkspaceCreate.name != "" {
		t.Fatal("expected pending create to be consumed")
	}
}

func TestDialogResult_StaleCancelKeepsPendingAndDialog(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	proj := &data.Project{Path: "/tmp/repo", Name: "repo"}

	// Simulate a mid-chain state: the create dialog was confirmed and the
	// picker is the live instance.
	h.app.pendingWorkspaceCreate.project = proj
	h.app.pendingWorkspaceCreate.name = "ws-new"
	h.app.handleShowSelectAssistantDialog()
	liveDialog := h.app.dialog
	liveSeq := h.app.dialogOpenSeq

	// A stale cancel from a displaced non-picker dialog must not close the
	// live picker or clear the pending handoff it still owns.
	handled, _ := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    liveSeq - 1,
		result: common.DialogResult{ID: DialogCreateWorkspace, Confirmed: false},
	})
	if !handled {
		t.Fatal("expected bound result msg to be handled")
	}
	if h.app.dialog != liveDialog || !h.app.dialog.Visible() {
		t.Fatal("stale cancel closed the live dialog")
	}
	if h.app.pendingWorkspaceCreate.name != "ws-new" {
		t.Fatal("stale non-picker cancel cleared pendingWorkspaceCreate")
	}
}

func TestDialogResult_StalePickerResultClearsPendingCreate(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	proj := &data.Project{Path: "/tmp/repo", Name: "repo"}
	ws := newDialogTestWorkspace("ws-a")

	// The picker was displaced: pending create lingers, a newer dialog owns
	// the open seq. A stale picker result arriving now means the displaced
	// instance emitted late — the handoff must die with it. (Pending is set
	// after the newer dialog opens so the Show-time clear doesn't preempt
	// the stale-drop path under test.)
	h.app.handleShowDeleteWorkspaceDialog(messages.ShowDeleteWorkspaceDialog{Project: proj, Workspace: ws})
	liveSeq := h.app.dialogOpenSeq
	h.app.pendingWorkspaceCreate.project = proj
	h.app.pendingWorkspaceCreate.name = "ws-new"

	handled, _ := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    liveSeq - 1,
		result: common.DialogResult{ID: common.AgentPickerDialogID, Confirmed: false},
	})
	if !handled {
		t.Fatal("expected bound result msg to be handled")
	}
	if h.app.pendingWorkspaceCreate.name != "" {
		t.Fatal("displaced picker's late result must clear pendingWorkspaceCreate")
	}
}

func TestDialogResult_DuplicateDeliveryDropped(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	proj := &data.Project{Path: "/tmp/repo"}
	ws := newDialogTestWorkspace("ws-a")

	h.app.handleShowDeleteWorkspaceDialog(messages.ShowDeleteWorkspaceDialog{Project: proj, Workspace: ws})
	bound := emitDialogResult(t, h.app, confirmYes()...)

	if _, cmd := h.app.handleDialogResultMsg(bound); cmd == nil {
		t.Fatal("expected first delivery to apply")
	}
	// A second delivery of the same bound result (e.g. a duplicated cmd)
	// must not re-fire the handler.
	if handled, cmd := h.app.handleDialogResultMsg(bound); !handled || cmd != nil {
		t.Fatal("expected duplicate delivery of the consumed result to be dropped")
	}
}
