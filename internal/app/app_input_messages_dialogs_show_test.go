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

// newDialogHarness builds a center-mode harness used by the dialog-show tests.
func newDialogHarness(t *testing.T) *Harness {
	t.Helper()
	h, err := NewHarness(HarnessOptions{
		Mode:   HarnessCenter,
		Width:  120,
		Height: 40,
		Tabs:   1,
	})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	return h
}

// dialogView returns the ansi-stripped rendered view for the active dialog.
func dialogView(t *testing.T, d *common.Dialog) string {
	t.Helper()
	if d == nil {
		t.Fatal("expected a non-nil dialog")
	}
	if !d.Visible() {
		t.Fatal("expected dialog to be visible")
	}
	return ansi.Strip(d.View())
}

// confirmResult drives the dialog's Enter handler and returns the emitted
// DialogResult, which lets us assert on the dialog ID/Confirmed wiring even
// though those fields are unexported on common.Dialog.
func confirmResult(t *testing.T, d *common.Dialog) common.DialogResult {
	t.Helper()
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

func TestHandleShowAddProjectDialog_ShowsFilePicker(t *testing.T) {
	h := newDialogHarness(t)

	if h.app.filePicker != nil && h.app.filePicker.Visible() {
		t.Fatal("file picker should start hidden")
	}

	h.app.handleShowAddProjectDialog()

	if h.app.filePicker == nil {
		t.Fatal("expected file picker to be created")
	}
	if !h.app.filePicker.Visible() {
		t.Fatal("expected file picker to be visible after show")
	}

	view := ansi.Strip(h.app.filePicker.View())
	if !strings.Contains(view, "Add Project") {
		t.Fatalf("expected file picker title in view, got %q", view)
	}
	if !strings.Contains(view, "Add as project") {
		t.Fatalf("expected primary action label in view, got %q", view)
	}
}

func TestHandleShowCreateWorkspaceDialog_StoresProjectAndValidates(t *testing.T) {
	project := &data.Project{Name: "alpha", Path: "/repo/alpha"}

	tests := []struct {
		name        string
		input       string
		wantErrText bool
	}{
		{name: "empty input shows no error", input: "", wantErrText: false},
		{name: "valid name shows no error", input: "feature-x", wantErrText: false},
		{name: "consecutive dots rejected", input: "bad..name", wantErrText: true},
		{name: "lock suffix rejected", input: "branch.lock", wantErrText: true},
		{name: "reserved HEAD rejected", input: "HEAD", wantErrText: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newDialogHarness(t)
			h.app.handleShowCreateWorkspaceDialog(messages.ShowCreateWorkspaceDialog{Project: project})

			if h.app.dialogProject != project {
				t.Fatalf("expected dialogProject to be stored, got %v", h.app.dialogProject)
			}
			if h.app.dialog == nil || !h.app.dialog.Visible() {
				t.Fatal("expected create-workspace dialog to be visible")
			}

			view := dialogView(t, h.app.dialog)
			if !strings.Contains(view, "Create Workspace") {
				t.Fatalf("expected dialog title in view, got %q", view)
			}

			// Type the test input one rune at a time so the validator runs.
			for _, r := range tc.input {
				h.app.dialog, _ = h.app.dialog.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
			}

			rendered := ansi.Strip(h.app.dialog.View())
			if tc.wantErrText {
				// Invalid names keep the dialog open (Enter is blocked) and the
				// validation error is surfaced in the rendered view.
				if !strings.Contains(rendered, "name") {
					t.Fatalf("expected validation error text for %q, got %q", tc.input, rendered)
				}
				_, cmd := h.app.dialog.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				if cmd != nil {
					t.Fatalf("expected Enter to be blocked while validation fails for %q", tc.input)
				}
				if !h.app.dialog.Visible() {
					t.Fatalf("expected dialog to stay open while invalid for %q", tc.input)
				}
			}
		})
	}
}

func TestHandleShowCreateWorkspaceDialog_ValidInputConfirms(t *testing.T) {
	h := newDialogHarness(t)
	project := &data.Project{Name: "alpha", Path: "/repo/alpha"}
	h.app.handleShowCreateWorkspaceDialog(messages.ShowCreateWorkspaceDialog{Project: project})

	for _, r := range "feature-x" {
		h.app.dialog, _ = h.app.dialog.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	res := confirmResult(t, h.app.dialog)
	if res.ID != DialogCreateWorkspace {
		t.Fatalf("expected result ID %q, got %q", DialogCreateWorkspace, res.ID)
	}
	if !res.Confirmed {
		t.Fatal("expected confirmed result for valid workspace name")
	}
	if res.Value != "feature-x" {
		t.Fatalf("expected value %q, got %q", "feature-x", res.Value)
	}
}

func TestHandleShowDeleteWorkspaceDialog_StoresTargetsAndConfirms(t *testing.T) {
	h := newDialogHarness(t)
	project := &data.Project{Name: "alpha", Path: "/repo/alpha"}
	ws := &data.Workspace{Name: "feature-x", Repo: "/repo/alpha", Root: "/repo/alpha/ws"}

	h.app.handleShowDeleteWorkspaceDialog(messages.ShowDeleteWorkspaceDialog{
		Project:   project,
		Workspace: ws,
	})

	if h.app.dialogProject != project {
		t.Fatal("expected dialogProject to be stored")
	}
	if h.app.dialogWorkspace != ws {
		t.Fatal("expected dialogWorkspace to be stored")
	}

	view := dialogView(t, h.app.dialog)
	if !strings.Contains(view, "Delete Workspace") {
		t.Fatalf("expected delete title in view, got %q", view)
	}
	if !strings.Contains(view, "feature-x") {
		t.Fatalf("expected workspace name in view, got %q", view)
	}

	// A confirm dialog defaults to the "No" option (cursor 1), so Enter
	// produces an unconfirmed result targeting the delete-workspace ID.
	res := confirmResult(t, h.app.dialog)
	if res.ID != DialogDeleteWorkspace {
		t.Fatalf("expected result ID %q, got %q", DialogDeleteWorkspace, res.ID)
	}
	if res.Confirmed {
		t.Fatal("expected default selection to be the cancel/No option")
	}
}

func TestHandleShowTrustScriptsDialog_StoresHashAndDefaultsToNo(t *testing.T) {
	tests := []struct {
		name          string
		workspace     *data.Workspace
		hash          string
		wantInMessage string
	}{
		{
			name:          "named workspace appears in prompt",
			workspace:     &data.Workspace{Name: "feature-x"},
			hash:          "abc123",
			wantInMessage: "feature-x",
		},
		{
			name:          "nil workspace renders empty name without panic",
			workspace:     nil,
			hash:          "",
			wantInMessage: "Trust",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newDialogHarness(t)
			h.app.handleShowTrustScriptsDialog(messages.ShowTrustScriptsDialog{
				Workspace:  tc.workspace,
				ConfigHash: tc.hash,
			})

			if h.app.dialogWorkspace != tc.workspace {
				t.Fatal("expected dialogWorkspace to be stored")
			}
			if h.app.dialogTrustScriptsHash != tc.hash {
				t.Fatalf("expected stored hash %q, got %q", tc.hash, h.app.dialogTrustScriptsHash)
			}

			view := dialogView(t, h.app.dialog)
			if !strings.Contains(view, "Trust Project Scripts") {
				t.Fatalf("expected trust title in view, got %q", view)
			}
			if !strings.Contains(view, tc.wantInMessage) {
				t.Fatalf("expected %q in view, got %q", tc.wantInMessage, view)
			}

			// trust-scripts pins the default option to index 1 ("No") via
			// SetDefaultOption(1), so Enter on the freshly shown dialog yields an
			// unconfirmed result. Moving left ("h") first selects "Yes".
			yes, _ := h.app.dialog.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
			if confirmed := confirmResult(t, yes); !confirmed.Confirmed {
				t.Fatal("expected moving to the Yes option to confirm")
			}

			// Re-show to assert the untouched default lands on "No".
			h.app.handleShowTrustScriptsDialog(messages.ShowTrustScriptsDialog{
				Workspace:  tc.workspace,
				ConfigHash: tc.hash,
			})
			res := confirmResult(t, h.app.dialog)
			if res.ID != DialogTrustScripts {
				t.Fatalf("expected result ID %q, got %q", DialogTrustScripts, res.ID)
			}
			if res.Confirmed {
				t.Fatal("expected trust-scripts dialog to default to the No option")
			}
		})
	}
}

func TestHandleShowRemoveProjectDialog_RendersProjectName(t *testing.T) {
	tests := []struct {
		name     string
		project  *data.Project
		wantName string
	}{
		{
			name:     "named project appears in prompt",
			project:  &data.Project{Name: "alpha", Path: "/repo/alpha"},
			wantName: "alpha",
		},
		{
			name:     "nil project renders empty name without panic",
			project:  nil,
			wantName: "Remove project",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newDialogHarness(t)
			h.app.handleShowRemoveProjectDialog(messages.ShowRemoveProjectDialog{Project: tc.project})

			if h.app.dialogProject != tc.project {
				t.Fatal("expected dialogProject to be stored")
			}

			view := dialogView(t, h.app.dialog)
			if !strings.Contains(view, "Remove Project") {
				t.Fatalf("expected remove title in view, got %q", view)
			}
			if !strings.Contains(view, tc.wantName) {
				t.Fatalf("expected %q in view, got %q", tc.wantName, view)
			}

			res := confirmResult(t, h.app.dialog)
			if res.ID != DialogRemoveProject {
				t.Fatalf("expected result ID %q, got %q", DialogRemoveProject, res.ID)
			}
			if res.Confirmed {
				t.Fatal("expected remove-project dialog to default to the No option")
			}
		})
	}
}

func TestHandleShowSelectAssistantDialog_NoActiveWorkspaceIsNoop(t *testing.T) {
	h := newDialogHarness(t)
	h.app.activeWorkspace = nil
	h.app.pendingWorkspaceProject = nil
	h.app.dialog = nil

	h.app.handleShowSelectAssistantDialog()

	if h.app.dialog != nil {
		t.Fatal("expected no dialog when there is no active or pending workspace")
	}
}

func TestHandleShowSelectAssistantDialog_ShowsAgentPicker(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(a *App)
	}{
		{
			name:    "active workspace present",
			prepare: func(a *App) { a.activeWorkspace = harnessWorkspace() },
		},
		{
			name:    "pending workspace project present",
			prepare: func(a *App) { a.pendingWorkspaceProject = &data.Project{Name: "alpha"} },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newDialogHarness(t)
			h.app.activeWorkspace = nil
			h.app.pendingWorkspaceProject = nil
			tc.prepare(h.app)

			h.app.handleShowSelectAssistantDialog()

			if h.app.dialog == nil || !h.app.dialog.Visible() {
				t.Fatal("expected agent picker dialog to be visible")
			}

			view := dialogView(t, h.app.dialog)
			if !strings.Contains(view, "New Agent") {
				t.Fatalf("expected agent picker title in view, got %q", view)
			}
			// The picker is seeded from the configured assistant names.
			for _, name := range h.app.assistantNames() {
				if !strings.Contains(view, name) {
					t.Fatalf("expected assistant %q in picker view, got %q", name, view)
				}
			}
		})
	}
}

func TestHandleShowCleanupTmuxDialog_ShowsServerNameAndGuardsReentry(t *testing.T) {
	h := newDialogHarness(t)
	h.app.tmuxOptions.ServerName = "amux-test-server"

	h.app.handleShowCleanupTmuxDialog()

	if h.app.dialog == nil || !h.app.dialog.Visible() {
		t.Fatal("expected cleanup dialog to be visible")
	}

	view := dialogView(t, h.app.dialog)
	if !strings.Contains(view, "Cleanup tmux sessions") {
		t.Fatalf("expected cleanup title in view, got %q", view)
	}
	if !strings.Contains(view, "amux-test-server") {
		t.Fatalf("expected server name in cleanup prompt, got %q", view)
	}

	// Re-invoking while the dialog is already visible must not replace it.
	existing := h.app.dialog
	h.app.handleShowCleanupTmuxDialog()
	if h.app.dialog != existing {
		t.Fatal("expected re-entrant show to keep the existing visible dialog")
	}
}

func TestHandleShowCleanupTmuxDialog_ReshowsAfterDismiss(t *testing.T) {
	h := newDialogHarness(t)
	h.app.tmuxOptions.ServerName = "amux-test-server"

	h.app.handleShowCleanupTmuxDialog()
	first := h.app.dialog
	if first == nil {
		t.Fatal("expected first cleanup dialog")
	}

	// Once hidden, the guard no longer blocks a fresh dialog.
	first.Hide()
	h.app.handleShowCleanupTmuxDialog()
	if h.app.dialog == nil || !h.app.dialog.Visible() {
		t.Fatal("expected a fresh cleanup dialog after the previous one was dismissed")
	}
	if h.app.dialog == first {
		t.Fatal("expected a newly constructed dialog after dismissal")
	}
}
