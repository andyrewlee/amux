package app

import (
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// FrameLineCacheRenderer renders a lipgloss canvas into a full-screen ANSI
// string while caching per-row render output. Unchanged rows reuse cached ANSI
// lines, avoiding full-frame cell-to-ANSI conversion on every frame.
type FrameLineCacheRenderer struct {
	width  int
	height int
	valid  bool

	prev  [][]uv.Cell
	curr  [][]uv.Cell
	lines []string

	builders    [2]strings.Builder
	builderNext int
}

func (r *FrameLineCacheRenderer) Invalidate() {
	r.valid = false
}

func (r *FrameLineCacheRenderer) Render(canvas *lipgloss.Canvas) string {
	if canvas == nil {
		r.Invalidate()
		return ""
	}
	width := canvas.Width()
	height := canvas.Height()
	if width <= 0 || height <= 0 {
		r.Invalidate()
		return ""
	}
	if r.ensureSize(width, height) {
		r.valid = false
	}

	for y := 0; y < height; y++ {
		rowChanged := !r.valid
		prevRow := r.prev[y]
		currRow := r.curr[y]

		for x := 0; x < width; x++ {
			cell := uv.EmptyCell
			if got := canvas.CellAt(x, y); got != nil {
				cell = *got
			}
			currRow[x] = cell
			if !rowChanged && !prevRow[x].Equal(&cell) {
				rowChanged = true
			}
		}

		if rowChanged {
			r.lines[y] = uv.Line(currRow).Render()
		}
	}

	r.prev, r.curr = r.curr, r.prev
	r.valid = true

	b := &r.builders[r.builderNext]
	r.builderNext = (r.builderNext + 1) % len(r.builders)
	b.Reset()

	for y := 0; y < height; y++ {
		if y > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(r.lines[y])
	}
	return b.String()
}

func (r *FrameLineCacheRenderer) ensureSize(width, height int) bool {
	if r.width == width && r.height == height && len(r.prev) == height && len(r.curr) == height && len(r.lines) == height {
		return false
	}
	r.width = width
	r.height = height
	r.prev = makeUVCellGrid(width, height)
	r.curr = makeUVCellGrid(width, height)
	r.lines = make([]string, height)
	for y := range r.lines {
		r.lines[y] = uv.Line(r.prev[y]).Render()
	}
	return true
}

func makeUVCellGrid(width, height int) [][]uv.Cell {
	grid := make([][]uv.Cell, height)
	for y := range grid {
		row := make([]uv.Cell, width)
		for x := range row {
			row[x] = uv.EmptyCell
		}
		grid[y] = row
	}
	return grid
}
