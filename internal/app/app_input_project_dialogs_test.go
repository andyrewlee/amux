package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// The project-rename dialog glue — the sibling of the workspace-rename
// tests in app_input_messages_dialogs_rename_commit_test.go, covering the
// prefill ordering, the subject guards, the validate closure (including the
// case-only rule the service enforces), and the result dispatch.

func TestHandleShowRenameProjectDialog_PrefillsCurrentName(t *testing.T) {
	h := newDialogHarness(t)
	project := &data.Project{Name: "checkfu", Path: "/repo/checkfu"}
	h.app.projects = []data.Project{*project}

	h.app.handleShowRenameProjectDialog(messages.ShowRenameProjectDialog{Project: project})

	if h.app.dlg.project == nil || h.app.dlg.project.Name != "checkfu" {
		t.Fatalf("expected dlg.project to carry the re-resolved project, got %+v", h.app.dlg.project)
	}
	view := dialogView(t, h.app.dialog)
	if !strings.Contains(view, "Rename Project") {
		t.Fatalf("expected rename-project title, got %q", view)
	}
	if !strings.Contains(view, "checkfu") {
		t.Fatalf("expected the dialog prefilled with the current project name, got %q", view)
	}
}

func TestHandleShowRenameProjectDialog_NilProjectIsNoop(t *testing.T) {
	h := newDialogHarness(t)

	h.app.handleShowRenameProjectDialog(messages.ShowRenameProjectDialog{Project: nil})

	if h.app.dialog != nil {
		t.Fatal("expected nil-project rename request to be a no-op")
	}
}

func TestHandleShowRenameProjectDialog_ValidatesReplacementName(t *testing.T) {
	h := newDialogHarness(t)
	project := &data.Project{Name: "checkfu", Path: "/repo/checkfu"}
	other := &data.Project{Name: "taken", Path: "/repo/taken"}
	h.app.projects = []data.Project{*project, *other}
	h.app.handleShowRenameProjectDialog(messages.ShowRenameProjectDialog{Project: project})

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
	typeInto("bad/name")
	rendered := ansi.Strip(h.app.dialog.View())
	if !strings.Contains(rendered, "single path segment") {
		t.Fatalf("expected validation error for a name with a separator, got %q", rendered)
	}
	if _, cmd := h.app.dialog.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("expected Enter to be blocked while the replacement name is invalid")
	}

	clearInput()
	typeInto("taken")
	rendered = ansi.Strip(h.app.dialog.View())
	if !strings.Contains(rendered, "name already used by") {
		t.Fatalf("expected the taken-name collision error, got %q", rendered)
	}

	// The current name itself is the no-op case: valid, Enter confirms.
	clearInput()
	typeInto("checkfu")
	res := confirmResult(t, h.app.dialog)
	if !res.Confirmed || res.Value != "checkfu" {
		t.Fatalf("expected the current name to confirm as a no-op, got %+v", res)
	}

	// A distinct valid name confirms too.
	h.app.handleShowRenameProjectDialog(messages.ShowRenameProjectDialog{Project: project})
	clearInput()
	typeInto("checkfu-old")
	res = confirmResult(t, h.app.dialog)
	if !res.Confirmed || res.Value != "checkfu-old" {
		t.Fatalf("expected a valid replacement to confirm, got %+v", res)
	}
}

// TestHandleShowRenameProjectDialog_RefusesCaseOnlyRenameOnCaseInsensitiveFS
// pins the validator's agreement with the service: on a filesystem where
// both spellings resolve to one container directory, the case-only rename
// is refused inline (the service would refuse it on dispatch); where the
// filesystem distinguishes them it is a legitimate rename.
func TestHandleShowRenameProjectDialog_RefusesCaseOnlyRenameOnCaseInsensitiveFS(t *testing.T) {
	h := newDialogHarness(t)
	root := filepath.Join(t.TempDir(), "workspaces")
	if err := os.MkdirAll(filepath.Join(root, "Checkfu"), 0o755); err != nil {
		t.Fatalf("mkdir container: %v", err)
	}
	// The validator mirrors the service through the configured root.
	h.app.config = &config.Config{Paths: &config.Paths{WorkspacesRoot: root}}
	project := &data.Project{Name: "Checkfu", Path: "/repo/checkfu"}
	h.app.projects = []data.Project{*project}
	h.app.handleShowRenameProjectDialog(messages.ShowRenameProjectDialog{Project: project})

	clearInput := func() {
		for i := 0; i < 20; i++ {
			h.app.dialog, _ = h.app.dialog.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		}
	}
	clearInput()
	for _, r := range "checkfu" {
		h.app.dialog, _ = h.app.dialog.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	same, ok := data.SamePhysicalDir(filepath.Join(root, "Checkfu"), filepath.Join(root, "checkfu"))
	if ok && same {
		rendered := ansi.Strip(h.app.dialog.View())
		if !strings.Contains(rendered, "only letter case changes") {
			t.Fatalf("expected the case-only refusal inline, got %q", rendered)
		}
		if _, cmd := h.app.dialog.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
			t.Fatal("expected Enter blocked while the rename only changes letter case")
		}
		return
	}
	res := confirmResult(t, h.app.dialog)
	if !res.Confirmed || res.Value != "checkfu" {
		t.Fatalf("distinct case-variant directories are a legitimate rename, got %+v", res)
	}
}

// TestDialogResultRenameProjectDispatch covers the result handler: refusals
// must not dispatch a rename, the valid case must carry the project and the
// new name.
func TestDialogResultRenameProjectDispatch(t *testing.T) {
	h := newDialogHarness(t)
	project := &data.Project{Name: "checkfu", Path: "/repo/checkfu"}

	// Empty value: refused with a warning toast, no dispatch.
	msg := runDialogResultCmd(t, dialogResultRenameProject(h.app, common.DialogResult{}, dialogContext{project: project}))
	if _, isRename := msg.(messages.RenameProject); isRename {
		t.Fatalf("empty value must not dispatch a rename, got %T", msg)
	}

	// Invalid name: a validation error surfaces, no dispatch.
	msg = runDialogResultCmd(t, dialogResultRenameProject(h.app, common.DialogResult{Value: "bad/name"}, dialogContext{project: project}))
	errMsg, isErr := msg.(messages.Error)
	if !isErr {
		t.Fatalf("invalid name must surface a validation error, got %T", msg)
	}
	if !strings.Contains(errMsg.Err.Error(), "single path segment") {
		t.Fatalf("unexpected validation error: %v", errMsg.Err)
	}

	// Lost context: refused, no dispatch.
	msg = runDialogResultCmd(t, dialogResultRenameProject(h.app, common.DialogResult{Value: "new-name"}, dialogContext{}))
	if _, isRename := msg.(messages.RenameProject); isRename {
		t.Fatalf("nil project context must not dispatch a rename, got %T", msg)
	}

	// Valid: dispatches the rename request with the project and new name.
	msg = runDialogResultCmd(t, dialogResultRenameProject(h.app, common.DialogResult{Value: "checkfu-old"}, dialogContext{project: project}))
	rename, isRename := msg.(messages.RenameProject)
	if !isRename {
		t.Fatalf("expected a RenameProject dispatch, got %T", msg)
	}
	if rename.Project != project || rename.NewName != "checkfu-old" {
		t.Fatalf("unexpected dispatch payload: %+v", rename)
	}
}

// runDialogResultCmd runs a dialog-result cmd (nil-safe) and returns the
// produced message, so refusal paths can assert nothing was dispatched.
func runDialogResultCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}
