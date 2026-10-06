package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// pickWithCtrlT delivers a RequestTask pick result through the dialog-result
// path, as if the picker had emitted it for a ctrl+t selection.
func pickWithCtrlT(t *testing.T, h *Harness, assistant string) tea.Cmd {
	t.Helper()
	handled, cmd := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    h.app.dialogOpenSeq,
		result: common.DialogResult{ID: common.AgentPickerDialogID, Confirmed: true, Value: assistant, RequestTask: true},
	})
	if !handled || cmd == nil {
		t.Fatal("expected ctrl+t pick to produce the task-dialog cmd")
	}
	return cmd
}

// TestAgentPickerCtrlT_ArmsTaskDialog pins the picker→task-dialog handoff:
// ctrl+t on a launch-mode pick stages pendingLaunchTask and requests the
// task dialog instead of dispatching LaunchAgent directly.
func TestAgentPickerCtrlT_ArmsTaskDialog(t *testing.T) {
	h := newDialogHarness(t)
	h.app.activeWorkspace = harnessWorkspace()
	h.app.handleShowSelectAssistantDialog()

	assistant := h.app.assistantNames()[0]
	cmd := pickWithCtrlT(t, h, assistant)
	if _, ok := cmd().(messages.ShowLaunchTaskDialog); !ok {
		t.Fatalf("ctrl+t pick produced %T, want ShowLaunchTaskDialog", cmd())
	}
	pending := h.app.pendingLaunchTask
	if pending.assistant != assistant || pending.workspace != h.app.activeWorkspace {
		t.Fatalf("pendingLaunchTask = %+v, want assistant %q on active workspace", pending, assistant)
	}
}

// TestAgentPickerCtrlT_RequestTaskGating proves the key is only armed on the
// launch path: the same dialog type in create-handoff mode leaves ctrl+t
// inert (no RequestTask result can be emitted).
func TestAgentPickerCtrlT_RequestTaskGating(t *testing.T) {
	t.Run("launch mode emits RequestTask", func(t *testing.T) {
		h := newDialogHarness(t)
		h.app.activeWorkspace = harnessWorkspace()
		h.app.handleShowSelectAssistantDialog()

		var cmd tea.Cmd
		h.app.dialog, cmd = h.app.dialog.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
		if cmd == nil {
			t.Fatal("ctrl+t produced no result on an armed picker")
		}
		res, ok := cmd().(common.DialogResult)
		if !ok || !res.RequestTask {
			t.Fatalf("result = %+v, want RequestTask pick", res)
		}
	})

	t.Run("create handoff keeps ctrl+t inert", func(t *testing.T) {
		h := newDialogHarness(t)
		h.app.pendingWorkspaceCreate.project = &data.Project{Name: "alpha"}
		h.app.handleShowSelectAssistantDialog()

		var cmd tea.Cmd
		h.app.dialog, cmd = h.app.dialog.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
		if cmd != nil {
			if res, ok := cmd().(common.DialogResult); ok && res.RequestTask {
				t.Fatalf("create-handoff picker emitted RequestTask: %+v", res)
			}
		}
		if h.app.dialog == nil || !h.app.dialog.Visible() {
			t.Fatal("inert ctrl+t closed the picker")
		}
	})
}

// driveLaunchTaskDialog types task into the open task dialog and confirms.
func driveLaunchTaskDialog(t *testing.T, d *common.Dialog, task string) common.DialogResult {
	t.Helper()
	for _, r := range task {
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

// TestLaunchTaskDialog_DispatchesTask proves the full field→message path:
// ctrl+t pick → task dialog → LaunchAgent carrying the typed text as Task.
func TestLaunchTaskDialog_DispatchesTask(t *testing.T) {
	h := newDialogHarness(t)
	h.app.activeWorkspace = harnessWorkspace()
	h.app.handleShowSelectAssistantDialog()
	assistant := h.app.assistantNames()[0]
	pickWithCtrlT(t, h, assistant)

	h.app.handleShowLaunchTaskDialog()
	dlg := h.app.dialog
	if dlg == nil || !dlg.Visible() {
		t.Fatal("expected first-task dialog to be visible")
	}
	res := driveLaunchTaskDialog(t, dlg, "fix the flaky test")
	if res.ID != DialogLaunchTask || res.Value != "fix the flaky test" {
		t.Fatalf("task dialog result = %+v", res)
	}

	handled, cmd := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    h.app.dialogOpenSeq,
		result: res,
	})
	if !handled || cmd == nil {
		t.Fatal("expected task result to produce a launch cmd")
	}
	launch, ok := cmd().(messages.LaunchAgent)
	if !ok {
		t.Fatalf("expected messages.LaunchAgent, got %T", cmd())
	}
	if launch.Assistant != assistant || launch.Workspace != h.app.activeWorkspace || launch.Task != "fix the flaky test" {
		t.Fatalf("launch = %+v", launch)
	}
	if h.app.pendingLaunchTask.assistant != "" {
		t.Fatal("pending handoff not consumed by the task result")
	}
}

// TestLaunchTaskDialog_EmptyPreservesNoTask guards today's behavior: an
// empty field launches exactly as a plain pick would — Task stays "".
func TestLaunchTaskDialog_EmptyPreservesNoTask(t *testing.T) {
	h := newDialogHarness(t)
	h.app.activeWorkspace = harnessWorkspace()
	h.app.pendingLaunchTask = pendingLaunchTaskState{
		assistant: h.app.assistantNames()[0],
		workspace: h.app.activeWorkspace,
	}

	handled, cmd := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    h.app.dialogOpenSeq,
		result: common.DialogResult{ID: DialogLaunchTask, Confirmed: true, Value: "   "},
	})
	if !handled || cmd == nil {
		t.Fatal("expected empty task result to produce a launch cmd")
	}
	launch, ok := cmd().(messages.LaunchAgent)
	if !ok || launch.Task != "" {
		t.Fatalf("launch = %+v, want Task empty", launch)
	}
}

// TestLaunchTaskDialog_SanitizesPastedLines pins the field-boundary line
// policy: a pasted multi-line payload keeps only its first printable line —
// a raw newline inside Task would act as an early Enter in the agent TUI.
func TestLaunchTaskDialog_SanitizesPastedLines(t *testing.T) {
	h := newDialogHarness(t)
	h.app.activeWorkspace = harnessWorkspace()
	h.app.pendingLaunchTask = pendingLaunchTaskState{
		assistant: h.app.assistantNames()[0],
		workspace: h.app.activeWorkspace,
	}

	handled, cmd := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    h.app.dialogOpenSeq,
		result: common.DialogResult{ID: DialogLaunchTask, Confirmed: true, Value: "first line\nsecond\r\nthird"},
	})
	if !handled || cmd == nil {
		t.Fatal("expected task result to produce a launch cmd")
	}
	launch, ok := cmd().(messages.LaunchAgent)
	if !ok {
		t.Fatalf("expected messages.LaunchAgent, got %T", cmd())
	}
	if launch.Task != "first line" {
		t.Fatalf("Task = %q, want first line only", launch.Task)
	}
	if strings.ContainsAny(launch.Task, "\r\n") {
		t.Fatalf("Task carries raw newline bytes: %q", launch.Task)
	}
}

// TestLaunchTaskDialog_CancelReopensPicker pins the cancel contract: Esc
// drops the staged task handoff and returns to the assistant picker rather
// than losing the pick entirely.
func TestLaunchTaskDialog_CancelReopensPicker(t *testing.T) {
	h := newDialogHarness(t)
	h.app.activeWorkspace = harnessWorkspace()
	h.app.handleShowSelectAssistantDialog()
	pickWithCtrlT(t, h, h.app.assistantNames()[0])
	h.app.handleShowLaunchTaskDialog()

	handled, cmd := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    h.app.dialogOpenSeq,
		result: common.DialogResult{ID: DialogLaunchTask, Confirmed: false},
	})
	if !handled || cmd == nil {
		t.Fatal("expected task cancel to produce the picker-reopen cmd")
	}
	if _, ok := cmd().(messages.ShowSelectAssistantDialog); !ok {
		t.Fatalf("cancel produced %T, want ShowSelectAssistantDialog", cmd())
	}
	if h.app.pendingLaunchTask.assistant != "" {
		t.Fatal("canceled task dialog left the launch handoff armed")
	}
}

// TestLaunchTaskDialog_StaleResultClearsPending covers the displacement
// path: a task-dialog result arriving after a newer dialog opened must drop
// the handoff, not fire a stale launch.
func TestLaunchTaskDialog_StaleResultClearsPending(t *testing.T) {
	h := newDialogHarness(t)
	h.app.activeWorkspace = harnessWorkspace()
	h.app.pendingLaunchTask = pendingLaunchTaskState{
		assistant: h.app.assistantNames()[0],
		workspace: h.app.activeWorkspace,
	}
	h.app.dialogOpenSeq = 7

	handled, cmd := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    3, // older than the live dialog's seq
		result: common.DialogResult{ID: DialogLaunchTask, Confirmed: true, Value: "task"},
	})
	if !handled || cmd != nil {
		t.Fatal("expected stale task result to be dropped")
	}
	if h.app.pendingLaunchTask.assistant != "" {
		t.Fatal("stale task result left the launch handoff armed")
	}
}
