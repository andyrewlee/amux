package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// driveCreateDialog types name, tabs to the base field, types base, and
// confirms — mirroring how a user fills the two-field form.
func driveCreateDialog(t *testing.T, d *common.Dialog, name, base string) common.DialogResult {
	t.Helper()
	for _, r := range name {
		d, _ = d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	d, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	for _, r := range base {
		d, _ = d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected Enter to produce a DialogResult command")
	}
	res, ok := cmd().(common.DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", cmd())
	}
	return res
}

// pickerResult drives the assistant-picker leg of the create handoff and
// returns the emitted CreateWorkspace message.
func pickerResult(t *testing.T, h *Harness) messages.CreateWorkspace {
	t.Helper()
	handled, cmd := h.app.handleDialogResultMsg(boundDialogResultMsg{
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
	return cw
}

// TestCreateWorkspaceDialog_BaseFieldFlow proves the optional base field
// survives the full create flow: dialog → pending state → agent picker →
// messages.CreateWorkspace.Base.
func TestCreateWorkspaceDialog_BaseFieldFlow(t *testing.T) {
	h := newDialogHarness(t)
	proj := &data.Project{Path: "/tmp/repo", Name: "repo"}
	h.app.handleShowCreateWorkspaceDialog(messages.ShowCreateWorkspaceDialog{Project: proj})

	// Both labels render so the two fields stay identifiable after entry.
	view := ansi.Strip(h.app.dialog.View())
	if !strings.Contains(view, "Base") {
		t.Fatalf("base field label not rendered: %q", view)
	}

	res := driveCreateDialog(t, h.app.dialog, "ws-new", "release/1.2")
	if res.Value != "ws-new" || res.Value2 != "release/1.2" {
		t.Fatalf("dialog result = %+v", res)
	}

	handled, cmd := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    h.app.dialogOpenSeq,
		dlg:    dialogContext{project: proj},
		result: res,
	})
	if !handled || cmd == nil {
		t.Fatal("expected create result to produce the assistant-picker cmd")
	}
	if h.app.pendingWorkspaceCreate.base != "release/1.2" {
		t.Fatalf("base not staged: %+v", h.app.pendingWorkspaceCreate)
	}

	h.app.handleShowSelectAssistantDialog()
	cw := pickerResult(t, h)
	if cw.Base != "release/1.2" {
		t.Fatalf("CreateWorkspace.Base = %q", cw.Base)
	}
	if cw.Name != "ws-new" || cw.Project != proj {
		t.Fatalf("create context lost: %+v", cw)
	}
}

// TestCreateWorkspaceDialog_EmptyBasePreservesDefault proves leaving the base
// field empty keeps the existing default-branch resolution — "" flows all the
// way through to CreateWorkspace.Base for ResolveBase to interpret.
func TestCreateWorkspaceDialog_EmptyBasePreservesDefault(t *testing.T) {
	h := newDialogHarness(t)
	proj := &data.Project{Path: "/tmp/repo", Name: "repo"}
	h.app.handleShowCreateWorkspaceDialog(messages.ShowCreateWorkspaceDialog{Project: proj})

	res := driveCreateDialog(t, h.app.dialog, "ws-new", "")
	if res.Value2 != "" {
		t.Fatalf("expected empty Value2, got %q", res.Value2)
	}

	handled, cmd := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    h.app.dialogOpenSeq,
		dlg:    dialogContext{project: proj},
		result: res,
	})
	if !handled || cmd == nil {
		t.Fatal("expected assistant-picker cmd")
	}
	if h.app.pendingWorkspaceCreate.base != "" {
		t.Fatalf("expected empty base staged, got %+v", h.app.pendingWorkspaceCreate)
	}

	h.app.handleShowSelectAssistantDialog()
	cw := pickerResult(t, h)
	if cw.Base != "" {
		t.Fatalf("expected empty base to reach CreateWorkspace, got %q", cw.Base)
	}
}

// TestCreateWorkspaceDialog_InvalidBaseBlocksConfirm proves a malformed base
// ref keeps the dialog open, renders the inline error, and dispatches nothing.
func TestCreateWorkspaceDialog_InvalidBaseBlocksConfirm(t *testing.T) {
	h := newDialogHarness(t)
	proj := &data.Project{Path: "/tmp/repo", Name: "repo"}
	h.app.handleShowCreateWorkspaceDialog(messages.ShowCreateWorkspaceDialog{Project: proj})

	d := h.app.dialog
	for _, r := range "ws-new" {
		d, _ = d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	d, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	for _, r := range "bad..ref" {
		d, _ = d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	h.app.dialog = d

	if _, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("Enter confirmed with an invalid base")
	}
	if !d.Visible() {
		t.Fatal("dialog closed despite invalid base")
	}
	if view := ansi.Strip(d.View()); !strings.Contains(view, "..") {
		t.Fatalf("expected inline base error in view: %q", view)
	}
}

// TestDialogResultCreateWorkspace_InvalidValue2Rejected proves the result
// handler re-vets the base: a malformed Value2 arriving outside the dialog's
// validator still becomes an error, never a pending create.
func TestDialogResultCreateWorkspace_InvalidValue2Rejected(t *testing.T) {
	h := newDialogHarness(t)
	proj := &data.Project{Path: "/tmp/repo", Name: "repo"}

	handled, cmd := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    h.app.dialogOpenSeq,
		dlg:    dialogContext{project: proj},
		result: common.DialogResult{ID: DialogCreateWorkspace, Confirmed: true, Value: "ws-new", Value2: "bad..ref"},
	})
	if !handled || cmd == nil {
		t.Fatal("expected an error cmd for the invalid base")
	}
	if _, ok := cmd().(messages.Error); !ok {
		t.Fatalf("expected messages.Error, got %T", cmd())
	}
	if h.app.pendingWorkspaceCreate.name != "" || h.app.pendingWorkspaceCreate.base != "" {
		t.Fatalf("invalid base staged a pending create: %+v", h.app.pendingWorkspaceCreate)
	}
}
