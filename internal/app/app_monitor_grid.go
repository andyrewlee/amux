package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
)

type monitorGrid struct {
	cols       int
	rows       int
	colWidths  []int
	rowHeights []int
	gapX       int
	gapY       int
}

func monitorGridLayout(count, width, height int) monitorGrid {
	grid := monitorGrid{
		gapX: 1,
		gapY: 1,
	}
	if count <= 0 || width <= 0 || height <= 0 {
		return grid
	}

	minTileWidth := 20
	minTileHeight := 6
	bestCols := 1
	bestScore := -1
	bestArea := -1

	for cols := 1; cols <= count; cols++ {
		rows := (count + cols - 1) / cols
		gridWidth := width - grid.gapX*(cols-1)
		gridHeight := height - grid.gapY*(rows-1)
		if gridWidth <= 0 || gridHeight <= 0 {
			continue
		}

		tileWidth := gridWidth / cols
		tileHeight := gridHeight / rows
		if tileWidth <= 0 || tileHeight <= 0 {
			continue
		}

		score := tileWidth
		if tileHeight < score {
			score = tileHeight
		}
		if tileWidth < minTileWidth || tileHeight < minTileHeight {
			score /= 2
		}
		area := tileWidth * tileHeight
		if score > bestScore || (score == bestScore && area > bestArea) {
			bestScore = score
			bestArea = area
			bestCols = cols
		}
	}

	rows := (count + bestCols - 1) / bestCols
	gridWidth := width - grid.gapX*(bestCols-1)
	if gridWidth < bestCols {
		gridWidth = bestCols
	}
	gridHeight := height - grid.gapY*(rows-1)
	if gridHeight < rows {
		gridHeight = rows
	}

	grid.cols = bestCols
	grid.rows = rows
	grid.colWidths = make([]int, bestCols)
	grid.rowHeights = make([]int, rows)

	baseCol := gridWidth / bestCols
	extraCol := gridWidth % bestCols
	for i := 0; i < bestCols; i++ {
		grid.colWidths[i] = baseCol
		if i < extraCol {
			grid.colWidths[i]++
		}
	}

	baseRow := gridHeight / rows
	extraRow := gridHeight % rows
	for i := 0; i < rows; i++ {
		grid.rowHeights[i] = baseRow
		if i < extraRow {
			grid.rowHeights[i]++
		}
	}

	return grid
}

type monitorRect struct {
	X int
	Y int
	W int
	H int
}

func monitorTileRect(grid monitorGrid, index int, offsetX, offsetY int) monitorRect {
	if grid.cols == 0 || grid.rows == 0 {
		return monitorRect{}
	}
	row := index / grid.cols
	col := index % grid.cols
	if row < 0 || col < 0 || row >= len(grid.rowHeights) || col >= len(grid.colWidths) {
		return monitorRect{}
	}

	x := offsetX
	for i := 0; i < col; i++ {
		x += grid.colWidths[i] + grid.gapX
	}
	y := offsetY
	for i := 0; i < row; i++ {
		y += grid.rowHeights[i] + grid.gapY
	}

	return monitorRect{
		X: x,
		Y: y,
		W: grid.colWidths[col],
		H: grid.rowHeights[row],
	}
}

func (a *App) monitorGridArea() (int, int, int, int) {
	if a.height <= 2 {
		return 0, 0, a.width, a.height
	}
	return 0, 1, a.width, a.height - 1
}

func (a *App) monitorCanvasFor(width, height int) *compositor.Canvas {
	if width <= 0 || height <= 0 {
		width = 1
		height = 1
	}
	if a.monitorCanvas == nil {
		a.monitorCanvas = compositor.NewCanvas(width, height)
	} else if a.monitorCanvas.Width != width || a.monitorCanvas.Height != height {
		a.monitorCanvas.Resize(width, height)
	}
	return a.monitorCanvas
}

func (a *App) canvasFor(width, height int) *lipgloss.Canvas {
	if width <= 0 || height <= 0 {
		width = 1
		height = 1
	}
	if a.canvas == nil {
		a.canvas = lipgloss.NewCanvas(width, height)
	} else if a.canvas.Width() != width || a.canvas.Height() != height {
		a.canvas.Resize(width, height)
	}
	a.canvas.Clear()
	return a.canvas
}

func (a *App) monitorLayoutKeyFor(tabs []center.MonitorTab, gridW, gridH int, sizes []center.TabSize) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%dx%d:%d|", gridW, gridH, len(tabs))
	for i, tab := range tabs {
		b.WriteString(string(tab.ID))
		if i < len(sizes) {
			fmt.Fprintf(&b, ":%dx%d", sizes[i].Width, sizes[i].Height)
		}
		b.WriteString("|")
	}
	return b.String()
}

type monitorProjectFilter struct {
	Key   string
	Label string
}

func (a *App) monitorProjectFilters() []monitorProjectFilter {
	tabs := a.center.MonitorTabs()
	seen := make(map[string]bool)
	filters := make([]monitorProjectFilter, 0, len(tabs))
	for _, tab := range tabs {
		if tab.Worktree == nil {
			continue
		}
		key, label := a.monitorProjectKeyLabel(tab.Worktree)
		if key == "" {
			continue
		}
		if label == "" {
			label = key
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		filters = append(filters, monitorProjectFilter{Key: key, Label: label})
	}

	if len(filters) == 0 {
		return filters
	}

	labelCounts := make(map[string]int, len(filters))
	for _, filter := range filters {
		labelCounts[filter.Label]++
	}

	if len(labelCounts) == len(filters) {
		return filters
	}

	for i, filter := range filters {
		if labelCounts[filter.Label] > 1 {
			suffix := filepath.Base(filepath.Dir(filter.Key))
			if suffix == "" || suffix == "." || suffix == string(filepath.Separator) {
				suffix = filepath.Base(filter.Key)
			}
			if suffix == "" || suffix == filter.Label {
				suffix = filter.Key
			}
			filters[i].Label = fmt.Sprintf("%s (%s)", filter.Label, suffix)
		}
	}

	return filters
}

func (a *App) monitorProjectKeyLabel(wt *data.Worktree) (string, string) {
	if wt == nil {
		return "", ""
	}
	if project := a.projectForWorktree(wt); project != nil {
		key := project.Path
		if key == "" {
			key = wt.Repo
		}
		if key == "" {
			key = wt.Root
		}
		label := project.Name
		if label == "" {
			label = monitorProjectName(wt)
		}
		return key, label
	}
	key := wt.Repo
	if key == "" {
		key = wt.Root
	}
	label := monitorProjectName(wt)
	if label == "" {
		label = key
	}
	return key, label
}

func (a *App) monitorFilterLabel(key string) string {
	if key == "" {
		return "All"
	}
	for _, filter := range a.monitorProjectFilters() {
		if filter.Key == key {
			return filter.Label
		}
	}
	if base := filepath.Base(key); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return key
}

func (a *App) filterMonitorTabs(tabs []center.MonitorTab) []center.MonitorTab {
	if a.monitorFilter == "" {
		return tabs
	}
	var filtered []center.MonitorTab
	for _, tab := range tabs {
		if tab.Worktree == nil {
			continue
		}
		key, _ := a.monitorProjectKeyLabel(tab.Worktree)
		if key == a.monitorFilter {
			filtered = append(filtered, tab)
		}
	}
	return filtered
}

func (a *App) monitorExitHit(x, y int) bool {
	if y != 0 {
		return false
	}
	header := a.monitorHeaderText()
	exitText := "[Exit]"
	headerStripped := ansi.Strip(header)
	idx := strings.Index(headerStripped, exitText)
	if idx < 0 {
		return false
	}
	start := idx
	end := start + len(exitText)
	return x >= start && x < end
}

func (a *App) monitorFilterHit(x, y int) (string, bool) {
	if y != 0 {
		return "", false
	}
	header := a.monitorHeaderText()
	headerStripped := ansi.Strip(header)

	// Check "All" button
	allText := "[All]"
	allIdx := strings.Index(headerStripped, allText)
	if allIdx >= 0 && x >= allIdx && x < allIdx+len(allText) {
		return "", true
	}

	// Check project buttons
	filters := a.monitorProjectFilters()
	for _, filter := range filters {
		btnText := "[" + filter.Label + "]"
		idx := strings.Index(headerStripped, btnText)
		if idx >= 0 && x >= idx && x < idx+len(btnText) {
			return filter.Key, true
		}
	}
	return "", false
}

func (a *App) selectMonitorTile(paneX, paneY int) (int, bool) {
	tabs := a.filterMonitorTabs(a.center.MonitorTabs())
	count := len(tabs)
	if count == 0 {
		return -1, false
	}

	gridX, gridY, gridW, gridH := a.monitorGridArea()
	x := paneX - gridX
	y := paneY - gridY
	if x < 0 || y < 0 || x >= gridW || y >= gridH {
		return -1, false
	}

	grid := monitorGridLayout(count, gridW, gridH)
	if grid.cols == 0 || grid.rows == 0 {
		return -1, false
	}

	col := -1
	for c := 0; c < grid.cols; c++ {
		if x < grid.colWidths[c] {
			col = c
			break
		}
		x -= grid.colWidths[c]
		if c < grid.cols-1 {
			if x < grid.gapX {
				return -1, false
			}
			x -= grid.gapX
		}
	}

	row := -1
	for r := 0; r < grid.rows; r++ {
		if y < grid.rowHeights[r] {
			row = r
			break
		}
		y -= grid.rowHeights[r]
		if r < grid.rows-1 {
			if y < grid.gapY {
				return -1, false
			}
			y -= grid.gapY
		}
	}

	if row < 0 || col < 0 {
		return -1, false
	}

	index := row*grid.cols + col
	if index >= 0 && index < count {
		a.center.SetMonitorSelectedIndex(index, count)
		return index, true
	}
	return -1, false
}

func monitorProjectName(wt *data.Worktree) string {
	if wt == nil {
		return "unknown"
	}
	if wt.Repo != "" {
		return filepath.Base(wt.Repo)
	}
	if wt.Root != "" {
		return filepath.Base(wt.Root)
	}
	return "unknown"
}

func (a *App) monitorHeaderText() string {
	// Build filter buttons
	filters := a.monitorProjectFilters()
	activeStyle := lipgloss.NewStyle().
		Foreground(common.ColorForeground).
		Background(common.ColorBackground).
		Bold(true).
		Padding(0, 1)
	inactiveStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted).
		Background(common.ColorBackground).
		Padding(0, 1)

	var filterButtons strings.Builder
	if a.monitorFilter == "" {
		filterButtons.WriteString(activeStyle.Render("[All]"))
	} else {
		filterButtons.WriteString(inactiveStyle.Render("[All]"))
	}
	for _, filter := range filters {
		btnText := "[" + filter.Label + "]"
		if a.monitorFilter == filter.Key {
			filterButtons.WriteString(activeStyle.Render(btnText))
		} else {
			filterButtons.WriteString(inactiveStyle.Render(btnText))
		}
	}

	exitBtn := inactiveStyle.Render("[Exit]")
	filtersStr := filterButtons.String()
	filtersWidth := ansi.StringWidth(filtersStr)
	exitWidth := ansi.StringWidth(exitBtn)
	padding := a.width - filtersWidth - exitWidth
	if padding < 0 {
		padding = 0
	}
	return filtersStr + strings.Repeat(" ", padding) + exitBtn
}
