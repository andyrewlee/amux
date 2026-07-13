package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// TestHandleShowRenameWorkspaceDialog_PrefillsCurrentName guards the
// load-bearing "prefill AFTER presentDialog" ordering in
// handleShowRenameWorkspaceDialog: Show() resets the input to empty, so
// SetInputValue must run after it or the dialog would render blank instead
// of ready-to-edit. Uses the newDialogHarness/dialogView helpers defined in
// app_input_messages_dialogs_show_test.go (same package).
func TestHandleShowRenameWorkspaceDialog_PrefillsCurrentName(t *testing.T) {
	h := newDialogHarness(t)
	project := &data.Project{Name: "alpha", Path: "/repo/alpha"}
	ws := &data.Workspace{Name: "old-name", Repo: "/repo/alpha", Root: "/repo/alpha/ws"}

	h.app.handleShowRenameWorkspaceDialog(messages.ShowRenameWorkspaceDialog{
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
	if !strings.Contains(view, "Rename Workspace") {
		t.Fatalf("expected rename title in view, got %q", view)
	}
	if !strings.Contains(view, "old-name") {
		t.Fatalf("expected the rename dialog prefilled with the current workspace name, got %q", view)
	}
}

// TestHandleShowRenameWorkspaceDialog_NilWorkspaceIsNoop confirms the
// nil-workspace guard: no dialog is shown.
func TestHandleShowRenameWorkspaceDialog_NilWorkspaceIsNoop(t *testing.T) {
	h := newDialogHarness(t)

	h.app.handleShowRenameWorkspaceDialog(messages.ShowRenameWorkspaceDialog{
		Project:   &data.Project{Name: "alpha"},
		Workspace: nil,
	})

	if h.app.dialog != nil {
		t.Fatal("expected nil-workspace rename request to be a no-op")
	}
}

// TestHandleShowRenameWorkspaceDialog_ValidatesReplacementName exercises the
// validate closure (SanitizeInput then ValidateWorkspaceName): an invalid
// replacement blocks Enter and surfaces an error, a valid one confirms.
func TestHandleShowRenameWorkspaceDialog_ValidatesReplacementName(t *testing.T) {
	h := newDialogHarness(t)
	ws := &data.Workspace{Name: "old-name"}

	h.app.handleShowRenameWorkspaceDialog(messages.ShowRenameWorkspaceDialog{Workspace: ws})

	clearInput := func() {
		// 20 backspaces safely over-clears regardless of current content
		// length; backspacing an empty field is a no-op.
		for i := 0; i < 20; i++ {
			h.app.dialog, _ = h.app.dialog.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		}
	}
	typeInto := func(s string) {
		for _, r := range s {
			h.app.dialog, _ = h.app.dialog.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}

	clearInput()
	typeInto("bad..name")
	rendered := ansi.Strip(h.app.dialog.View())
	if !strings.Contains(rendered, "name") {
		t.Fatalf("expected validation error for invalid replacement name, got %q", rendered)
	}
	if _, cmd := h.app.dialog.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("expected Enter to be blocked while the replacement name is invalid")
	}

	clearInput()
	typeInto("feature-y")
	res := confirmResult(t, h.app.dialog)
	if !res.Confirmed || res.Value != "feature-y" {
		t.Fatalf("expected confirmed result with value %q, got %+v", "feature-y", res)
	}
}

// TestHandleShowCommitWorkspaceDialog_ValidatesLeadingDash covers the
// commit-message dialog's live guard: a '-'-prefixed message is refused
// (defense-in-depth against it being parsed as a `git commit -m` flag),
// while a normal or empty message is accepted at the validate-closure level
// (an empty message is refused later, by CommitAll on confirm).
func TestHandleShowCommitWorkspaceDialog_ValidatesLeadingDash(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErrText bool
	}{
		{name: "empty message shows no error", input: "", wantErrText: false},
		{name: "normal message shows no error", input: "fix bug", wantErrText: false},
		{name: "leading dash rejected", input: "-oops", wantErrText: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newDialogHarness(t)
			ws := &data.Workspace{Name: "feature-x", Repo: "/repo/alpha", Root: "/repo/alpha/ws"}

			h.app.handleShowCommitWorkspaceDialog(messages.ShowCommitWorkspaceDialog{Workspace: ws})

			if h.app.dialogWorkspace != ws {
				t.Fatal("expected dialogWorkspace to be stored")
			}
			view := dialogView(t, h.app.dialog)
			if !strings.Contains(view, "Commit changes") {
				t.Fatalf("expected commit title in view, got %q", view)
			}

			for _, r := range tc.input {
				h.app.dialog, _ = h.app.dialog.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
			}

			rendered := ansi.Strip(h.app.dialog.View())
			if tc.wantErrText {
				if !strings.Contains(rendered, "cannot start with '-'") {
					t.Fatalf("expected leading-dash validation error for %q, got %q", tc.input, rendered)
				}
				if _, cmd := h.app.dialog.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
					t.Fatalf("expected Enter to be blocked while invalid for %q", tc.input)
				}
				return
			}
			if strings.Contains(rendered, "cannot start with '-'") {
				t.Fatalf("expected no validation error for %q, got %q", tc.input, rendered)
			}
			res := confirmResult(t, h.app.dialog)
			if !res.Confirmed || res.Value != tc.input {
				t.Fatalf("expected confirmed result with value %q, got %+v", tc.input, res)
			}
		})
	}
}
