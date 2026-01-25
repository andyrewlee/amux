package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/vterm"
)

// viewMonitorMode renders monitor mode using layer-based compositing.
func (a *App) viewMonitorMode() tea.View {
	view := tea.View{
		AltScreen:            true,
		MouseMode:            tea.MouseModeCellMotion,
		BackgroundColor:      common.ColorBackground,
		ForegroundColor:      common.ColorForeground,
		KeyboardEnhancements: tea.KeyboardEnhancements{ReportEventTypes: true},
	}

	// Create canvas at screen dimensions
	canvas := a.canvasFor(a.width, a.height)

	// Render monitor grid content
	gridContent := a.renderMonitorGrid()
	gridDrawable := compositor.NewStringDrawable(gridContent, 0, 0)
	canvas.Compose(gridDrawable)

	// Render styled toolbar at top (composed separately to support styled buttons)
	toolbarContent := a.monitorHeaderText()
	toolbarDrawable := compositor.NewStringDrawable(toolbarContent, 0, 0)
	canvas.Compose(toolbarDrawable)

	// Compose overlays using the same layer-based approach as normal mode
	a.composeOverlays(canvas)

	view.SetContent(syncBegin + canvas.Render() + syncEnd)
	view.Cursor = a.overlayCursor()
	return view
}

func (a *App) renderMonitorGrid() string {
	if a.width <= 0 || a.height <= 0 {
		return ""
	}

	allTabs := a.center.MonitorTabs()
	tabs := a.filterMonitorTabs(allTabs)
	if len(tabs) == 0 {
		canvas := a.monitorCanvasFor(a.width, a.height)
		canvas.Fill(vterm.Style{Fg: compositor.HexColor(common.HexColor(common.ColorForeground)), Bg: compositor.HexColor(common.HexColor(common.ColorBackground))})
		empty := "No agents running"
		if a.monitorFilter != "" && len(allTabs) > 0 {
			empty = "No agents for " + a.monitorFilterLabel(a.monitorFilter)
		}
		x := (a.width - ansi.StringWidth(empty)) / 2
		y := a.height / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		canvas.DrawText(x, y, empty, vterm.Style{Fg: compositor.HexColor(common.HexColor(common.ColorMuted))})
		return canvas.Render()
	}

	gridX, gridY, gridW, gridH := a.monitorGridArea()
	grid := monitorGridLayout(len(tabs), gridW, gridH)
	if grid.cols == 0 || grid.rows == 0 {
		return ""
	}

	tabSizes := make([]center.TabSize, 0, len(tabs))
	for i, tab := range tabs {
		rect := monitorTileRect(grid, i, gridX, gridY)
		contentW := rect.W - 2
		contentH := rect.H - 3 // border + header line
		if contentW < 1 {
			contentW = 1
		}
		if contentH < 1 {
			contentH = 1
		}
		tabSizes = append(tabSizes, center.TabSize{
			ID:     tab.ID,
			Width:  contentW,
			Height: contentH,
		})
	}

	layoutKey := a.monitorLayoutKeyFor(tabs, gridW, gridH, tabSizes)
	if layoutKey != a.monitorLayoutKey {
		a.center.ResizeTabs(tabSizes)
		a.monitorLayoutKey = layoutKey
	}

	selectedIndex := a.center.MonitorSelectedIndex(len(tabs))
	activeID := center.TabID("")
	if selectedIndex >= 0 && selectedIndex < len(tabs) {
		activeID = tabs[selectedIndex].ID
	}
	snapshots := a.center.MonitorTabSnapshotsWithActive(activeID)
	snapByID := make(map[center.TabID]center.MonitorTabSnapshot, len(snapshots))
	for _, snap := range snapshots {
		snapByID[snap.ID] = snap
	}

	canvas := a.monitorCanvasFor(a.width, a.height)
	canvas.Fill(vterm.Style{
		Fg: compositor.HexColor(common.HexColor(common.ColorForeground)),
		Bg: compositor.HexColor(common.HexColor(common.ColorBackground)),
	})

	// Header/toolbar is rendered separately in viewMonitorMode to support styled buttons

	projectNames := make(map[string]string, len(a.projects))
	for _, project := range a.projects {
		projectNames[project.Path] = project.Name
	}

	for idx, tab := range tabs {
		rect := monitorTileRect(grid, idx, gridX, gridY)
		focused := idx == selectedIndex
		if rect.W < 4 || rect.H < 4 {
			continue
		}

		borderStyle := vterm.Style{Fg: compositor.HexColor(common.HexColor(common.ColorBorder))}
		if focused {
			borderStyle.Fg = compositor.HexColor(common.HexColor(common.ColorBorderFocused))
		}
		canvas.DrawBorder(rect.X, rect.Y, rect.W, rect.H, borderStyle, focused)

		innerX := rect.X + 1
		innerY := rect.Y + 1
		innerW := rect.W - 2
		innerH := rect.H - 2
		if innerW < 1 || innerH < 1 {
			continue
		}

		worktreeName := "unknown"
		if tab.Worktree != nil && tab.Worktree.Name != "" {
			worktreeName = tab.Worktree.Name
		}
		projectName := ""
		if tab.Worktree != nil {
			projectName = projectNames[tab.Worktree.Repo]
		}
		if projectName == "" {
			projectName = monitorProjectName(tab.Worktree)
		}

		statusIcon := common.Icons.Idle
		if tab.Running {
			statusIcon = common.Icons.Running
		}

		assistant := tab.Name
		if assistant == "" {
			assistant = tab.Assistant
		}

		cursor := common.Icons.CursorEmpty
		if focused {
			cursor = common.Icons.Cursor
		}
		header := fmt.Sprintf("%s %s %s/%s", cursor, statusIcon, projectName, worktreeName)
		if assistant != "" {
			header += " [" + assistant + "]"
		}

		hStyle := vterm.Style{Fg: compositor.HexColor(common.HexColor(common.ColorForeground)), Bold: true}
		if focused {
			hStyle.Bg = compositor.HexColor(common.HexColor(common.ColorSelection))
		}
		canvas.DrawText(innerX, innerY, header, hStyle)

		contentY := innerY + 1
		contentH := innerH - 1
		if contentH <= 0 {
			continue
		}

		snap, ok := snapByID[tab.ID]
		if !ok || len(snap.Screen) == 0 {
			canvas.DrawText(innerX, contentY, "No active agent", vterm.Style{Fg: compositor.HexColor(common.HexColor(common.ColorMuted))})
			continue
		}

		canvas.DrawScreen(
			innerX,
			contentY,
			innerW,
			contentH,
			snap.Screen,
			snap.CursorX,
			snap.CursorY,
			focused,
			snap.ViewOffset,
			snap.SelActive,
			snap.SelStartX,
			snap.SelStartY,
			snap.SelEndX,
			snap.SelEndY,
		)
	}

	return canvas.Render()
}

func (a *App) handleMonitorInput(msg tea.KeyPressMsg) tea.Cmd {
	// Monitor mode - type to interact:
	// All keys -> Forward to selected tile's PTY

	// Forward all other keys to selected tile's PTY
	tabs := a.filterMonitorTabs(a.center.MonitorTabs())
	if len(tabs) == 0 {
		return nil
	}
	idx := a.center.MonitorSelectedIndex(len(tabs))
	return a.center.HandleMonitorInput(tabs[idx].ID, msg)
}

func (a *App) projectForWorktree(wt *data.Worktree) *data.Project {
	if wt == nil {
		return nil
	}
	for i := range a.projects {
		project := &a.projects[i]
		if project.Path == wt.Repo {
			return project
		}
		for j := range project.Worktrees {
			if project.Worktrees[j].Root == wt.Root {
				return project
			}
		}
	}
	return nil
}
