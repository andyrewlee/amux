package app

import (
	"strings"
	"testing"
	"time"

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

	h.app.overlays.settings = common.NewSettingsDialog(common.ThemeTokyoNight, "", "", "")
	h.app.overlays.settings.Show()
	h.app.overlays.settings.SetSize(h.app.width, h.app.height)

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
		Width:  95,
		Height: 8,
	})
	if err != nil {
		t.Fatalf("expected harness creation to succeed: %v", err)
	}
	if len(h.tabs) != 1 || h.tabs[0] == nil || h.tabs[0].Terminal == nil {
		t.Fatal("expected center harness terminal")
	}

	_ = h.app.toast.Show("copy complete", common.ToastInfo, time.Minute)

	termOffsetX, termOffsetY, termW, termH := h.app.center.TerminalViewport()
	centerX := h.app.layout.LeftGutter() + h.app.layout.DashboardWidth() + h.app.layout.GapX()
	termX := centerX + termOffsetX
	termY := h.app.layout.TopGutter() + termOffsetY

	toastView := h.app.toast.View()
	if toastView == "" {
		t.Fatal("expected visible toast")
	}
	toastW, toastH := viewDimensions(toastView)
	toastX := (h.app.width - toastW) / 2
	toastY := h.app.height - 2

	overlapLeft := termX
	if toastX > overlapLeft {
		overlapLeft = toastX
	}
	overlapTop := termY
	if toastY > overlapTop {
		overlapTop = toastY
	}
	overlapRight := termX + termW
	if toastX+toastW < overlapRight {
		overlapRight = toastX + toastW
	}
	overlapBottom := termY + termH
	if toastY+toastH < overlapBottom {
		overlapBottom = toastY + toastH
	}
	if overlapLeft >= overlapRight || overlapTop >= overlapBottom {
		t.Fatal("expected toast and terminal viewport to overlap in test setup")
	}

	h.tabs[0].Terminal.CursorX = overlapLeft - termX
	h.tabs[0].Terminal.CursorY = overlapTop - termY

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

// TestOverlayCursor_UsesComposedGeometry pins the contract for the
// dialog/picker cursor path: a fresh compose snapshot supplies the overlay
// dims (no second View() render); a stale or empty snapshot falls back to
// measuring live.
func TestOverlayCursor_UsesComposedGeometry(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Tabs: 1, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	dialog := common.NewInputDialog("rename", "Rename", "file name")
	dialog.Show()
	dialog.SetSize(120, 40)
	h.app.dialog = dialog

	inner := dialog.Cursor()
	if inner == nil {
		t.Fatal("expected the input dialog to own a cursor")
	}

	// Sentinel dims: the cursor offset must follow them, proving the cache
	// (not a re-render) drove placement.
	h.app.overlayGeom = overlayGeometry{composed: true, width: 120, height: 40, dialogW: 20, dialogH: 10}
	c := h.app.overlayCursor()
	if c == nil {
		t.Fatal("expected an overlay cursor")
	}
	wantX, wantY := 50+inner.X, 15+inner.Y // centeredPosition(20,10) on 120x40
	if c.X != wantX || c.Y != wantY {
		t.Fatalf("overlayCursor = (%d,%d), want (%d,%d) from composed dims", c.X, c.Y, wantX, wantY)
	}

	// Stale snapshot → live measurement resumes.
	h.app.overlayGeom.width = 999
	liveW, liveH := viewDimensions(dialog.View())
	liveC := h.app.overlayCursor()
	lx, ly := h.app.centeredPosition(liveW, liveH)
	if liveC.X != lx+inner.X || liveC.Y != ly+inner.Y {
		t.Fatalf("stale cache: overlayCursor = (%d,%d), want (%d,%d)", liveC.X, liveC.Y, lx+inner.X, ly+inner.Y)
	}
}
