package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/compositor"
)

// composeOverlays adds overlay layers (dialogs, toasts, help, etc.) to the canvas.
func (a *App) composeOverlays(canvas *lipgloss.Canvas) {
	// Dialog overlay
	if a.dialog != nil && a.dialog.Visible() {
		dialogView := a.dialog.View()
		dialogWidth, dialogHeight := viewDimensions(dialogView)
		x, y := a.centeredPosition(dialogWidth, dialogHeight)
		dialogDrawable := compositor.NewStringDrawable(dialogView, x, y)
		canvas.Compose(dialogDrawable)
	}

	// File picker overlay
	if a.filePicker != nil && a.filePicker.Visible() {
		pickerView := a.filePicker.View()
		pickerWidth, pickerHeight := viewDimensions(pickerView)
		x, y := a.centeredPosition(pickerWidth, pickerHeight)
		pickerDrawable := compositor.NewStringDrawable(pickerView, x, y)
		canvas.Compose(pickerDrawable)
	}

	// Settings dialog overlay
	if a.settingsDialog != nil && a.settingsDialog.Visible() {
		settingsView := a.settingsDialog.View()
		settingsWidth, settingsHeight := viewDimensions(settingsView)
		x, y := a.centeredPosition(settingsWidth, settingsHeight)
		settingsDrawable := compositor.NewStringDrawable(settingsView, x, y)
		canvas.Compose(settingsDrawable)
	}

	// Help overlay (centered like settings dialog)
	if a.helpOverlay.Visible() {
		helpView := a.helpOverlay.View()
		helpWidth, helpHeight := viewDimensions(helpView)
		x, y := a.centeredPosition(helpWidth, helpHeight)
		helpDrawable := compositor.NewStringDrawable(helpView, x, y)
		canvas.Compose(helpDrawable)
	}

	// Prefix mode indicator
	if a.prefixActive {
		indicator := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1a1b26")).
			Background(lipgloss.Color("#7aa2f7")).
			Padding(0, 1).
			Render("PREFIX")
		indicatorWidth := lipgloss.Width(indicator)
		x := a.width - indicatorWidth - 1
		y := a.height - 3
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		prefixDrawable := compositor.NewStringDrawable(indicator, x, y)
		canvas.Compose(prefixDrawable)
	}

	// Toast notification
	if a.toast.Visible() {
		toastView := a.toast.View()
		if toastView != "" {
			toastWidth := lipgloss.Width(toastView)
			x := (a.width - toastWidth) / 2
			y := a.height - 2
			if x < 0 {
				x = 0
			}
			if y < 0 {
				y = 0
			}
			toastDrawable := compositor.NewStringDrawable(toastView, x, y)
			canvas.Compose(toastDrawable)
		}
	}

	// Error overlay
	if a.err != nil {
		errView := a.renderErrorOverlay()
		errWidth, errHeight := viewDimensions(errView)
		x, y := a.centeredPosition(errWidth, errHeight)
		errDrawable := compositor.NewStringDrawable(errView, x, y)
		canvas.Compose(errDrawable)
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
	return errStyle.Render("Error: " + a.err.Error() + "\n\nPress any key to dismiss.")
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
			dialogView := a.dialog.View()
			dialogWidth, dialogHeight := viewDimensions(dialogView)
			x, y := a.centeredPosition(dialogWidth, dialogHeight)
			c.X += x
			c.Y += y
			return c
		}
		return nil
	}

	if a.filePicker != nil && a.filePicker.Visible() {
		if c := a.filePicker.Cursor(); c != nil {
			pickerView := a.filePicker.View()
			pickerWidth, pickerHeight := viewDimensions(pickerView)
			x, y := a.centeredPosition(pickerWidth, pickerHeight)
			c.X += x
			c.Y += y
			return c
		}
	}

	return nil
}
