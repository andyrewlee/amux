package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// overlayGeometry snapshots the overlay dimensions measured during the last
// composeOverlays pass so cursor placement and mouse hit-tests reuse them
// instead of re-rendering views the compose pass already produced. The
// width/height fields pin the terminal size at compose time — a resize (or a
// first frame not yet composed) invalidates the snapshot and consumers fall
// back to measuring live views.
type overlayGeometry struct {
	composed         bool
	width, height    int
	dialogW, dialogH int
	pickerW, pickerH int
	toastW, toastH   int
	paletteH         int
}

// fresh reports whether the snapshot reflects the current terminal size.
func (g overlayGeometry) fresh(a *App) bool {
	return g.composed && g.width == a.width && g.height == a.height
}

// composeOverlays adds overlay layers (dialogs, toasts, help, etc.) to the canvas.
func (a *App) composeOverlays(canvas *lipgloss.Canvas) {
	prefixOverlayHeight := 0
	a.overlayGeom = overlayGeometry{composed: true, width: a.width, height: a.height}

	// Dialog overlay
	if a.dialog != nil && a.dialog.Visible() {
		dialogView := a.dialog.View()
		dialogWidth, dialogHeight := viewDimensions(dialogView)
		a.overlayGeom.dialogW, a.overlayGeom.dialogH = dialogWidth, dialogHeight
		x, y := a.centeredPosition(dialogWidth, dialogHeight)
		if dialogDrawable := a.renderCache.overlayDialog.get(dialogView, x, y); dialogDrawable != nil {
			canvas.Compose(dialogDrawable)
		}
	}

	// File picker overlay
	if a.filePicker != nil && a.filePicker.Visible() {
		pickerView := a.filePicker.View()
		pickerWidth, pickerHeight := viewDimensions(pickerView)
		a.overlayGeom.pickerW, a.overlayGeom.pickerH = pickerWidth, pickerHeight
		x, y := a.centeredPosition(pickerWidth, pickerHeight)
		if pickerDrawable := a.renderCache.overlayFilePicker.get(pickerView, x, y); pickerDrawable != nil {
			canvas.Compose(pickerDrawable)
		}
	}

	// Settings dialog overlay
	if a.overlays.settings != nil && a.overlays.settings.Visible() {
		settingsView := a.overlays.settings.View()
		settingsWidth, settingsHeight := viewDimensions(settingsView)
		x, y := a.centeredPosition(settingsWidth, settingsHeight)
		if settingsDrawable := a.renderCache.overlaySettings.get(settingsView, x, y); settingsDrawable != nil {
			canvas.Compose(settingsDrawable)
		}
	}

	// Workspace env dialog overlay
	if a.overlays.env != nil && a.overlays.env.Visible() {
		envView := a.overlays.env.View()
		envWidth, envHeight := viewDimensions(envView)
		x, y := a.centeredPosition(envWidth, envHeight)
		if envDrawable := a.renderCache.overlayEnv.get(envView, x, y); envDrawable != nil {
			canvas.Compose(envDrawable)
		}
	}
	if a.overlays.projectEnv != nil && a.overlays.projectEnv.Visible() {
		envView := a.overlays.projectEnv.View()
		envWidth, envHeight := viewDimensions(envView)
		x, y := a.centeredPosition(envWidth, envHeight)
		if envDrawable := a.renderCache.overlayProjectEnv.get(envView, x, y); envDrawable != nil {
			canvas.Compose(envDrawable)
		}
	}

	// Workspace scripts dialog overlay
	if a.overlays.scripts != nil && a.overlays.scripts.Visible() {
		scriptsView := a.overlays.scripts.View()
		scriptsWidth, scriptsHeight := viewDimensions(scriptsView)
		x, y := a.centeredPosition(scriptsWidth, scriptsHeight)
		if scriptsDrawable := a.renderCache.overlayScripts.get(scriptsView, x, y); scriptsDrawable != nil {
			canvas.Compose(scriptsDrawable)
		}
	}

	// Run-script output viewer overlay
	if a.overlays.runOutput != nil && a.overlays.runOutput.Visible() {
		outView := a.overlays.runOutput.View()
		outWidth, outHeight := viewDimensions(outView)
		x, y := a.centeredPosition(outWidth, outHeight)
		if outDrawable := a.renderCache.overlayRunOutput.get(outView, x, y); outDrawable != nil {
			canvas.Compose(outDrawable)
		}
	}

	// Prefix command palette
	if a.prefixActive {
		palette := a.renderPrefixPalette()
		_, paletteHeight := viewDimensions(palette)
		prefixOverlayHeight = paletteHeight
		a.overlayGeom.paletteH = paletteHeight
		x := 0
		y := a.height - paletteHeight
		if y < 0 {
			y = 0
		}
		if prefixDrawable := a.renderCache.overlayPalette.get(palette, x, y); prefixDrawable != nil {
			canvas.Compose(prefixDrawable)
		}
	}

	// Toast notification
	if a.toast != nil && a.toast.Visible() {
		toastView := a.toast.View()
		if toastView != "" {
			toastWidth, toastHeight := viewDimensions(toastView)
			a.overlayGeom.toastW, a.overlayGeom.toastH = toastWidth, toastHeight
			x := (a.width - toastWidth) / 2
			y := a.height - 2 - prefixOverlayHeight
			if x < 0 {
				x = 0
			}
			if y < 0 {
				y = 0
			}
			if toastDrawable := a.renderCache.overlayToast.get(toastView, x, y); toastDrawable != nil {
				canvas.Compose(toastDrawable)
			}
		}
	}

	// Error overlay
	if a.err != nil {
		errView := a.renderErrorOverlay()
		errWidth, errHeight := viewDimensions(errView)
		x, y := a.centeredPosition(errWidth, errHeight)
		if errDrawable := a.renderCache.overlayErr.get(errView, x, y); errDrawable != nil {
			canvas.Compose(errDrawable)
		}
	}
}

// renderErrorOverlay returns the error overlay content.
func (a *App) renderErrorOverlay() string {
	if a.err == nil {
		return ""
	}
	errStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#f7768e")).
		Padding(1, 2).
		Width(56)
	// err.Error() can embed repo command strings or script stderr — flatten
	// newlines and strip terminal control bytes before rendering.
	errText := common.SanitizeDisplayText(strings.ReplaceAll(a.err.Error(), "\n", " "), 2048)
	return errStyle.Render("Error: " + errText + "\n\nPress any key to dismiss.")
}

func (a *App) finalizeView(view tea.View) tea.View {
	if a.pendingInputLatency {
		perf.Record("input_latency", time.Since(a.lastInputAt))
		a.pendingInputLatency = false
	}
	return view
}

func clampPane(view string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxWidth(width).
		MaxHeight(height).
		Render(view)
}

func clampLines(content string, width, maxLines int) string {
	if content == "" || width <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > width {
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	return strings.Join(lines, "\n")
}

func viewDimensions(view string) (width, height int) {
	lines := strings.Split(view, "\n")
	height = len(lines)
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			width = w
		}
	}
	return width, height
}

func (a *App) centeredPosition(width, height int) (x, y int) {
	x = (a.width - width) / 2
	y = (a.height - height) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

func (a *App) adjustSidebarMouseXY(x, y int) (int, int) {
	if a.layout == nil {
		return x, y
	}
	// Calculate sidebar X position
	sidebarX := a.layout.LeftGutter() + a.layout.DashboardWidth()
	if a.layout.ShowCenter() {
		sidebarX += a.layout.GapX() + a.layout.CenterWidth()
	}
	if a.layout.ShowSidebar() {
		sidebarX += a.layout.GapX()
	}
	// Sidebar content starts 2 columns in (border + padding)
	adjustedX := x - sidebarX - 2
	// Sidebar content starts one row below the top border.
	adjustedY := y - a.layout.TopGutter() - 1
	return adjustedX, adjustedY
}

func (a *App) overlayCursor() *tea.Cursor {
	if a.dialog != nil && a.dialog.Visible() {
		if c := a.dialog.Cursor(); c != nil {
			dialogWidth, dialogHeight := a.overlayGeom.dialogW, a.overlayGeom.dialogH
			if !a.overlayGeom.fresh(a) || dialogWidth == 0 {
				// No compose yet, a resize, or the dialog opened after the
				// last compose — measure live instead.
				dialogWidth, dialogHeight = viewDimensions(a.dialog.View())
			}
			x, y := a.centeredPosition(dialogWidth, dialogHeight)
			cursor := *c
			cursor.X += x
			cursor.Y += y
			return &cursor
		}
		return nil
	}

	if a.filePicker != nil && a.filePicker.Visible() {
		if c := a.filePicker.Cursor(); c != nil {
			pickerWidth, pickerHeight := a.overlayGeom.pickerW, a.overlayGeom.pickerH
			if !a.overlayGeom.fresh(a) || pickerWidth == 0 {
				pickerWidth, pickerHeight = viewDimensions(a.filePicker.View())
			}
			x, y := a.centeredPosition(pickerWidth, pickerHeight)
			cursor := *c
			cursor.X += x
			cursor.Y += y
			return &cursor
		}
	}

	return nil
}

func (a *App) overlayVisible() bool {
	return (a.dialog != nil && a.dialog.Visible()) ||
		(a.filePicker != nil && a.filePicker.Visible()) ||
		(a.overlays.settings != nil && a.overlays.settings.Visible()) ||
		(a.overlays.env != nil && a.overlays.env.Visible()) ||
		(a.overlays.projectEnv != nil && a.overlays.projectEnv.Visible()) ||
		(a.overlays.scripts != nil && a.overlays.scripts.Visible()) ||
		(a.overlays.runOutput != nil && a.overlays.runOutput.Visible()) ||
		a.prefixActive ||
		a.err != nil
}

// prefixPaletteHeight returns the palette's height in rows, reusing the last
// compose's measurement when fresh and rendering only when nothing composed
// yet (or a resize intervened).
func (a *App) prefixPaletteHeight() int {
	if a.overlayGeom.fresh(a) && a.overlayGeom.paletteH > 0 {
		return a.overlayGeom.paletteH
	}
	_, h := viewDimensions(a.renderPrefixPalette())
	return h
}

func (a *App) toastCoversPoint(x, y int) bool {
	if a == nil || a.toast == nil || !a.toast.Visible() {
		return false
	}
	var toastWidth, toastHeight, prefixOverlayHeight int
	// Cached dims are usable only if the toast itself was rendered in the
	// last compose (toastW > 0) — a toast that appeared since, a resize, or
	// no compose yet falls through to live measurement.
	if a.overlayGeom.fresh(a) && a.overlayGeom.toastW > 0 {
		toastWidth, toastHeight = a.overlayGeom.toastW, a.overlayGeom.toastH
		prefixOverlayHeight = a.overlayGeom.paletteH
	} else {
		toastView := a.toast.View()
		if toastView == "" {
			return false
		}
		if a.prefixActive {
			prefixOverlayHeight = a.prefixPaletteHeight()
		}
		toastWidth, toastHeight = viewDimensions(toastView)
	}
	toastX := (a.width - toastWidth) / 2
	toastY := a.height - 2 - prefixOverlayHeight
	if toastX < 0 {
		toastX = 0
	}
	if toastY < 0 {
		toastY = 0
	}
	return x >= toastX && x < toastX+toastWidth &&
		y >= toastY && y < toastY+toastHeight
}
