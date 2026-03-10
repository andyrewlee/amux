package app

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

func TestViewHidesTerminalCursorWhenSettingsOverlayIsVisible(t *testing.T) {
	h, err := NewHarness(HarnessOptions{
		Mode:   HarnessCenter,
		Tabs:   1,
		Width:  160,
		Height: 48,
	})
	if err != nil {
		t.Fatalf("expected harness creation to succeed: %v", err)
	}
	if len(h.tabs) != 1 || h.tabs[0] == nil || h.tabs[0].Terminal == nil {
		t.Fatal("expected center harness terminal")
	}
	h.tabs[0].Terminal.CursorX = 1
	h.tabs[0].Terminal.CursorY = h.tabs[0].Terminal.Height - 1

	base := h.Render()
	if base.Cursor == nil {
		t.Fatal("expected visible terminal cursor before overlay")
	}

	h.app.settingsDialog = common.NewSettingsDialog(common.ThemeTokyoNight)
	h.app.settingsDialog.Show()
	h.app.settingsDialog.SetSize(h.app.width, h.app.height)

	overlay := h.Render()
	if overlay.Cursor != nil {
		t.Fatal("expected terminal cursor to be hidden while settings overlay is visible")
	}
}

func TestViewKeepsTerminalCursorWhenOnlyToastIsVisible(t *testing.T) {
	h, err := NewHarness(HarnessOptions{
		Mode:   HarnessCenter,
		Tabs:   1,
		Width:  160,
		Height: 48,
	})
	if err != nil {
		t.Fatalf("expected harness creation to succeed: %v", err)
	}
	if len(h.tabs) != 1 || h.tabs[0] == nil || h.tabs[0].Terminal == nil {
		t.Fatal("expected center harness terminal")
	}
	h.tabs[0].Terminal.CursorX = 1
	h.tabs[0].Terminal.CursorY = h.tabs[0].Terminal.Height - 1

	base := h.Render()
	if base.Cursor == nil {
		t.Fatal("expected visible terminal cursor before toast")
	}

	_ = h.app.toast.ShowInfo("copy complete")

	toastView := h.Render()
	if toastView.Cursor == nil {
		t.Fatal("expected terminal cursor to remain visible while toast is shown")
	}
}

func TestViewHidesTerminalCursorWhenToastCoversIt(t *testing.T) {
	h, err := NewHarness(HarnessOptions{
		Mode:   HarnessCenter,
		Tabs:   1,
		Width:  60,
		Height: 12,
	})
	if err != nil {
		t.Fatalf("expected harness creation to succeed: %v", err)
	}
	if len(h.tabs) != 1 || h.tabs[0] == nil || h.tabs[0].Terminal == nil {
		t.Fatal("expected center harness terminal")
	}

	_ = h.app.toast.ShowInfo("copy complete")
	toastView := h.app.toast.View()
	if toastView == "" {
		t.Fatal("expected visible toast")
	}
	toastWidth := lipgloss.Width(toastView)
	toastX := (h.app.width - toastWidth) / 2
	toastY := h.app.height - 2
	h.tabs[0].Terminal.CursorX = toastX
	h.tabs[0].Terminal.CursorY = toastY

	view := h.Render()
	if view.Cursor != nil {
		t.Fatal("expected hardware cursor to stay hidden when a toast covers the cursor cell")
	}
}

func TestViewHardwareCursorDelegationDoesNotMutateCachedSnapshot(t *testing.T) {
	h, err := NewHarness(HarnessOptions{
		Mode:   HarnessCenter,
		Tabs:   1,
		Width:  160,
		Height: 48,
	})
	if err != nil {
		t.Fatalf("expected harness creation to succeed: %v", err)
	}
	if len(h.tabs) != 1 || h.tabs[0] == nil || h.tabs[0].Terminal == nil {
		t.Fatal("expected center harness terminal")
	}
	h.tabs[0].Terminal.CursorX = 1
	h.tabs[0].Terminal.CursorY = h.tabs[0].Terminal.Height - 1

	view := h.Render()
	if view.Cursor == nil {
		t.Fatal("expected hardware cursor delegation during render")
	}

	layer := h.app.center.TerminalLayerWithCursorOwner(true)
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected cached terminal layer snapshot after render")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected cached snapshot to retain software cursor visibility after hardware delegation")
	}
}

func TestViewHidesOverlayCursorWhenToastCoversIt(t *testing.T) {
	h, err := NewHarness(HarnessOptions{
		Mode:   HarnessCenter,
		Tabs:   1,
		Width:  80,
		Height: 24,
	})
	if err != nil {
		t.Fatalf("expected harness creation to succeed: %v", err)
	}

	dialog := common.NewInputDialog("rename", "Rename", "file name")
	dialog.Show()
	h.app.dialog = dialog

	_ = h.app.toast.ShowInfo(strings.Repeat("toast ", 12))

	covered := false
	for height := 4; height <= 24; height++ {
		h.app.height = height
		h.app.layout.Resize(h.app.width, h.app.height)
		h.app.updateLayout()
		dialog.SetSize(h.app.width, h.app.height)

		cursor := h.app.overlayCursor()
		if cursor != nil && h.app.toastCoversPoint(cursor.X, cursor.Y) {
			covered = true
			break
		}
	}
	if !covered {
		t.Fatal("expected toast to cover the overlay cursor in test setup")
	}

	view := h.Render()
	if view.Cursor != nil {
		t.Fatal("expected overlay cursor to stay hidden when a toast covers the cursor cell")
	}
}

func TestViewWrapsRenderedFrameInSynchronizedOutputMarkers(t *testing.T) {
	h, err := NewHarness(HarnessOptions{
		Mode:   HarnessCenter,
		Tabs:   1,
		Width:  160,
		Height: 48,
	})
	if err != nil {
		t.Fatalf("expected harness creation to succeed: %v", err)
	}

	view := h.Render()
	if !strings.HasPrefix(view.Content, syncBegin) {
		t.Fatal("expected rendered frame to start with DEC 2026 sync begin marker")
	}
	if !strings.HasSuffix(view.Content, syncEnd) {
		t.Fatal("expected rendered frame to end with DEC 2026 sync end marker")
	}
}
