package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/ui/common"
)

type overlayStub struct {
	visible bool
	updated bool
}

func (o *overlayStub) Visible() bool { return o.visible }

func (o *overlayStub) Update(tea.Msg) (*overlayStub, tea.Cmd) {
	o.updated = true
	return o, nil
}

func TestHandleOverlayInput_NilTypedPointer(t *testing.T) {
	var overlay *overlayStub
	cmds := make([]tea.Cmd, 0, 1)

	updated, consumed := handleOverlayInput(overlay, tea.KeyPressMsg{}, &cmds, true)
	if consumed {
		t.Fatal("expected nil overlay input not to be consumed")
	}
	if updated != nil {
		t.Fatal("expected updated overlay to remain nil")
	}
	if len(cmds) != 0 {
		t.Fatal("expected no commands for nil overlay")
	}
}

func TestHandleOverlayInput_VisibleOverlayConsumesKey(t *testing.T) {
	overlay := &overlayStub{visible: true}
	cmds := make([]tea.Cmd, 0, 1)

	updated, consumed := handleOverlayInput(overlay, tea.KeyPressMsg{}, &cmds, true)
	if !consumed {
		t.Fatal("expected key input to be consumed for visible overlay")
	}
	if !updated.updated {
		t.Fatal("expected visible overlay to receive Update")
	}
}

func TestHandleOverlayInput_VisibleOverlayConsumesWheel(t *testing.T) {
	overlay := &overlayStub{visible: true}
	cmds := make([]tea.Cmd, 0, 1)

	updated, consumed := handleOverlayInput(overlay, tea.MouseWheelMsg{Button: tea.MouseWheelDown}, &cmds, true)
	if !consumed {
		t.Fatal("expected wheel input to be consumed for visible overlay")
	}
	if !updated.updated {
		t.Fatal("expected visible overlay to receive wheel Update")
	}
}

func TestHandleOverlayInput_VisibleOverlayConsumesMotion(t *testing.T) {
	overlay := &overlayStub{visible: true}
	cmds := make([]tea.Cmd, 0, 1)

	updated, consumed := handleOverlayInput(overlay, tea.MouseMotionMsg{Button: tea.MouseLeft}, &cmds, true)
	if !consumed {
		t.Fatal("expected motion input to be consumed for visible overlay")
	}
	if !updated.updated {
		t.Fatal("expected visible overlay to receive motion Update")
	}
}

func TestHandleOverlayInput_VisibleOverlayConsumesRelease(t *testing.T) {
	overlay := &overlayStub{visible: true}
	cmds := make([]tea.Cmd, 0, 1)

	updated, consumed := handleOverlayInput(overlay, tea.MouseReleaseMsg{Button: tea.MouseLeft}, &cmds, true)
	if !consumed {
		t.Fatal("expected release input to be consumed for visible overlay")
	}
	if !updated.updated {
		t.Fatal("expected visible overlay to receive release Update")
	}
}

func TestAppUpdate_FilePickerVisibleConsumesWheel(t *testing.T) {
	h, err := NewHarness(HarnessOptions{
		Mode:   HarnessCenter,
		Width:  120,
		Height: 40,
		Tabs:   1,
	})
	if err != nil {
		t.Fatalf("harness init: %v", err)
	}

	tmp := t.TempDir()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := os.Mkdir(filepath.Join(tmp, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}

	h.app.filePicker = common.NewFilePicker(DialogAddProject, tmp, true)
	h.app.filePicker.SetSize(h.app.width, h.app.height)
	h.app.filePicker.Show()

	before := ansi.Strip(h.app.filePicker.View())
	if !strings.Contains(before, "> alpha/") {
		t.Fatalf("expected initial file picker selection on alpha, got %q", before)
	}

	h.app.update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})

	after := ansi.Strip(h.app.filePicker.View())
	if !strings.Contains(after, "> beta/") {
		t.Fatalf("expected wheel input to move file picker selection to beta, got %q", after)
	}
}

func TestHandleDialogResult_AddProjectEmptyShowsWarning(t *testing.T) {
	app := &App{toast: common.NewToastModel()}

	cmd := app.handleDialogResult(common.DialogResult{
		ID:        DialogAddProject,
		Confirmed: true,
		Value:     "",
	})

	if cmd == nil {
		t.Fatal("expected warning toast command")
	}
	if view := ansi.Strip(app.toast.View()); !strings.Contains(view, "Project path is required") {
		t.Fatalf("expected project path warning toast, got %q", view)
	}
}
