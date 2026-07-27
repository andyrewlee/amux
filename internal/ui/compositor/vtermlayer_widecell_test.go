package compositor

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/vterm"
)

// wideGlyphRow builds a single-row snapshot of the form "<wide>ab" padded to
// width, i.e. a wide glyph followed by narrow cells.
func wideGlyphRow(width int) *VTermSnapshot {
	row := vterm.MakeBlankLine(width)
	row[0] = vterm.Cell{Rune: '中', Width: 2}
	row[1] = vterm.Cell{Width: 0}
	row[2] = vterm.Cell{Rune: 'a', Width: 1}
	row[3] = vterm.Cell{Rune: 'b', Width: 1}
	return &VTermSnapshot{Screen: [][]vterm.Cell{row}, Width: width, Height: 1}
}

// TestVTermLayerKeepsWideGlyphAndColumnAlignment is the regression test for the
// "text drifts one column left" rendering bug: writing an explicit zero-width
// continuation cell made ultraviolet's Line.Set blank the wide base cell it
// belonged to, leaving an orphan placeholder that emits nothing, so every cell
// after a wide glyph rendered one column too far left.
func TestVTermLayerKeepsWideGlyphAndColumnAlignment(t *testing.T) {
	layer := NewVTermLayer(wideGlyphRow(4))

	canvas := lipgloss.NewCanvas(4, 1)
	canvas.Compose(&PositionedVTermLayer{VTermLayer: layer, PosX: 0, PosY: 0, Width: 4, Height: 1})

	if got, want := canvas.Render(), "中ab"; got != want {
		t.Fatalf("rendered %q, want %q (line shifted left / wide glyph lost)", got, want)
	}

	base := canvas.CellAt(0, 0)
	if base == nil || base.Content != "中" || base.Width != 2 {
		t.Fatalf("expected wide glyph at column 0, got %+v", base)
	}
	for x, want := range map[int]string{2: "a", 3: "b"} {
		if cell := canvas.CellAt(x, 0); cell == nil || cell.Content != want {
			t.Fatalf("expected %q at column %d, got %+v", want, x, cell)
		}
	}
}

// TestVTermLayerBlanksOrphanContinuationCell covers a zero-width cell whose
// wide base is missing (a half-erased glyph): it must occupy its column as a
// blank rather than collapse the rest of the line leftward.
func TestVTermLayerBlanksOrphanContinuationCell(t *testing.T) {
	row := vterm.MakeBlankLine(3)
	row[0] = vterm.Cell{Width: 0} // orphan at the start of the line
	row[1] = vterm.Cell{Rune: 'a', Width: 1}
	row[2] = vterm.Cell{Rune: 'b', Width: 1}
	snap := &VTermSnapshot{Screen: [][]vterm.Cell{row}, Width: 3, Height: 1}

	canvas := lipgloss.NewCanvas(3, 1)
	canvas.Compose(&PositionedVTermLayer{VTermLayer: NewVTermLayer(snap), PosX: 0, PosY: 0, Width: 3, Height: 1})

	if got, want := canvas.Render(), " ab"; got != want {
		t.Fatalf("rendered %q, want %q", got, want)
	}
}

// TestCanvasDrawScreenBlanksOrphanContinuationCell is the string-render
// counterpart: Canvas.Render skips zero-width cells, so an orphan there would
// shorten the line by a column.
func TestCanvasDrawScreenBlanksOrphanContinuationCell(t *testing.T) {
	row := vterm.MakeBlankLine(3)
	row[0] = vterm.Cell{Width: 0}
	row[1] = vterm.Cell{Rune: 'a', Width: 1}
	row[2] = vterm.Cell{Rune: 'b', Width: 1}

	canvas := NewCanvas(3, 1)
	canvas.DrawScreen(0, 0, 3, 1, [][]vterm.Cell{row}, CursorState{}, 0, SelectionRegion{})

	if got, want := canvas.Cells[0][0].Width, 1; got != want {
		t.Fatalf("orphan continuation width = %d, want %d", got, want)
	}
	if got := canvas.Cells[0][1]; got.Rune != 'a' || got.Width != 1 {
		t.Fatalf("cell after the orphan was disturbed: %+v", got)
	}
}

// TestCanvasBlanksWidowedWideBase covers the mirror defect: a wide base whose
// continuation was overwritten still draws two columns, so the rendered line
// comes out one column too wide and everything after it shifts right.
func TestCanvasBlanksWidowedWideBase(t *testing.T) {
	row := vterm.MakeBlankLine(4)
	row[0] = vterm.Cell{Rune: '中', Width: 2}
	row[1] = vterm.Cell{Rune: 'a', Width: 1} // continuation overwritten
	row[2] = vterm.Cell{Rune: 'b', Width: 1}

	canvas := NewCanvas(4, 1)
	canvas.DrawScreen(0, 0, 4, 1, [][]vterm.Cell{row}, CursorState{}, 0, SelectionRegion{})

	if got, want := ansi.StringWidth(ansi.Strip(canvas.Render())), 4; got != want {
		t.Fatalf("rendered width = %d, want %d (%q)", got, want, ansi.Strip(canvas.Render()))
	}
}

// TestCanvasRenderKeepsColumnCount asserts on the rendered string rather than
// on individual cells, so the orphan guard is pinned end-to-end through
// Canvas.Render (the center pane's string fallback) and not just in the buffer.
func TestCanvasRenderKeepsColumnCount(t *testing.T) {
	row := vterm.MakeBlankLine(3)
	row[0] = vterm.Cell{Width: 0} // orphan
	row[1] = vterm.Cell{Rune: 'a', Width: 1}
	row[2] = vterm.Cell{Rune: 'b', Width: 1}

	canvas := NewCanvas(3, 1)
	canvas.DrawScreen(0, 0, 3, 1, [][]vterm.Cell{row}, CursorState{}, 0, SelectionRegion{})

	out := ansi.Strip(canvas.Render())
	if got, want := ansi.StringWidth(out), 3; got != want {
		t.Fatalf("rendered width = %d, want %d (%q)", got, want, out)
	}
	if want := " ab"; out != want {
		t.Fatalf("rendered %q, want %q", out, want)
	}
}

// TestCanvasDrawScreenKeepsWideGlyph pins that the orphan guard does not touch
// a legitimate wide/continuation pair.
func TestCanvasDrawScreenKeepsWideGlyph(t *testing.T) {
	row := vterm.MakeBlankLine(4)
	row[0] = vterm.Cell{Rune: '中', Width: 2}
	row[1] = vterm.Cell{Width: 0}
	row[2] = vterm.Cell{Rune: 'a', Width: 1}

	canvas := NewCanvas(4, 1)
	canvas.DrawScreen(0, 0, 4, 1, [][]vterm.Cell{row}, CursorState{}, 0, SelectionRegion{})

	if canvas.Cells[0][0].Rune != '中' || canvas.Cells[0][0].Width != 2 {
		t.Fatalf("wide base was altered: %+v", canvas.Cells[0][0])
	}
	if canvas.Cells[0][1].Width != 0 {
		t.Fatalf("continuation cell was altered: %+v", canvas.Cells[0][1])
	}
}
