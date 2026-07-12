package compositor

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestCanvasRenderEmitsGraphemeCluster(t *testing.T) {
	canvas := NewCanvas(2, 1)
	// "é" as base rune 'e' plus combining acute accent U+0301, stored the way
	// vterm stores clusters: base rune + full cluster string.
	cluster := "e\u0301"
	canvas.SetCell(0, 0, vterm.Cell{Rune: 'e', Width: 1, GraphemeCluster: cluster})

	out := canvas.Render()
	if !strings.Contains(out, cluster) {
		t.Fatalf("expected render output to contain full grapheme cluster %q (U+0301 bytes 0xCC 0x81), got %q", cluster, out)
	}
}

func TestCanvasRenderEmptyClusterFallsBackToRenderableRune(t *testing.T) {
	canvas := NewCanvas(2, 1)
	canvas.SetCell(0, 0, vterm.Cell{Rune: 'x', Width: 1})
	canvas.SetCell(1, 0, vterm.Cell{Rune: 0, Width: 1})

	out := canvas.Render()
	if !strings.Contains(out, "x") {
		t.Fatalf("expected fallback to emit rune 'x', got %q", out)
	}
	// A zero rune with no cluster must render as RenderableRune(0) == ' ',
	// never as a NUL byte.
	if strings.ContainsRune(out, 0) {
		t.Fatalf("expected NUL rune to render as space via RenderableRune, got %q", out)
	}
	if !strings.Contains(out, "x ") {
		t.Fatalf("expected zero-rune cell to fall back to a space after 'x', got %q", out)
	}
}

func TestCanvasRenderSuppressesUnderlineOnBlankCells(t *testing.T) {
	canvas := NewCanvas(3, 1)
	style := vterm.Style{Underline: true}
	for x := 0; x < 3; x++ {
		canvas.SetCell(x, 0, vterm.Cell{Rune: ' ', Width: 1, Style: style})
	}

	out := canvas.Render()
	if vterm.ContainsSGRParam(out, 4) {
		t.Fatalf("expected no underline SGR for blank cells, got %q", out)
	}
}

func TestRenderSnapshotWithCanvasClampsOffscreenSelection(t *testing.T) {
	width, height := 5, 3
	term := vterm.New(width, height)
	term.Scrollback = [][]vterm.Cell{
		makeLine("aaaaa", width),
		makeLine("bbbbb", width),
		makeLine("ccccc", width),
		makeLine("ddddd", width),
	}
	term.Screen = [][]vterm.Cell{
		makeLine("eeeee", width),
		makeLine("fffff", width),
		makeLine("ggggg", width),
	}
	term.ViewOffset = 1
	term.SetSelection(2, 1, 3, 6, true, false)

	canvas := NewCanvas(width, height)
	RenderSnapshotWithCanvas(canvas, NewVTermSnapshot(term, false), width, height, vterm.Color{Type: vterm.ColorDefault}, vterm.Color{Type: vterm.ColorDefault})

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if !canvas.Cells[y][x].Style.Reverse {
				t.Fatalf("expected selection to clamp off-screen endpoints; missing reverse at x=%d y=%d", x, y)
			}
		}
	}
}

func TestRenderSnapshotWithCanvasClampsStartAboveViewport(t *testing.T) {
	width, height := 5, 3
	term := vterm.New(width, height)
	term.Scrollback = [][]vterm.Cell{
		makeLine("aaaaa", width),
		makeLine("bbbbb", width),
		makeLine("ccccc", width),
		makeLine("ddddd", width),
	}
	term.Screen = [][]vterm.Cell{
		makeLine("eeeee", width),
		makeLine("fffff", width),
		makeLine("ggggg", width),
	}
	term.ViewOffset = 1
	term.SetSelection(3, 1, 1, 4, true, false)

	canvas := NewCanvas(width, height)
	RenderSnapshotWithCanvas(canvas, NewVTermSnapshot(term, false), width, height, vterm.Color{Type: vterm.ColorDefault}, vterm.Color{Type: vterm.ColorDefault})

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			reversed := canvas.Cells[y][x].Style.Reverse
			switch y {
			case 0:
				if !reversed {
					t.Fatalf("expected full highlight on first visible row, missing reverse at x=%d y=%d", x, y)
				}
			case 1:
				if x <= 1 && !reversed {
					t.Fatalf("expected end line to highlight through x=1, missing reverse at x=%d y=%d", x, y)
				}
				if x > 1 && reversed {
					t.Fatalf("expected end line to stop at x=1, unexpected reverse at x=%d y=%d", x, y)
				}
			case 2:
				if reversed {
					t.Fatalf("expected no highlight after end line, unexpected reverse at x=%d y=%d", x, y)
				}
			}
		}
	}
}

func TestRenderSnapshotWithCanvasClampsEndBelowViewport(t *testing.T) {
	width, height := 5, 3
	term := vterm.New(width, height)
	term.Scrollback = [][]vterm.Cell{
		makeLine("aaaaa", width),
		makeLine("bbbbb", width),
		makeLine("ccccc", width),
		makeLine("ddddd", width),
	}
	term.Screen = [][]vterm.Cell{
		makeLine("eeeee", width),
		makeLine("fffff", width),
		makeLine("ggggg", width),
	}
	term.ViewOffset = 1
	term.SetSelection(2, 3, 1, 6, true, false)

	canvas := NewCanvas(width, height)
	RenderSnapshotWithCanvas(canvas, NewVTermSnapshot(term, false), width, height, vterm.Color{Type: vterm.ColorDefault}, vterm.Color{Type: vterm.ColorDefault})

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			reversed := canvas.Cells[y][x].Style.Reverse
			switch y {
			case 0:
				if x < 2 && reversed {
					t.Fatalf("expected start line to begin at x=2, unexpected reverse at x=%d y=%d", x, y)
				}
				if x >= 2 && !reversed {
					t.Fatalf("expected start line highlight from x=2, missing reverse at x=%d y=%d", x, y)
				}
			case 1, 2:
				if !reversed {
					t.Fatalf("expected full highlight on rows after start line, missing reverse at x=%d y=%d", x, y)
				}
			}
		}
	}
}

func TestRenderSnapshotWithCanvasReverseSelectionAnchor(t *testing.T) {
	width, height := 5, 3
	term := vterm.New(width, height)
	term.Scrollback = [][]vterm.Cell{
		makeLine("aaaaa", width),
		makeLine("bbbbb", width),
		makeLine("ccccc", width),
		makeLine("ddddd", width),
	}
	term.Screen = [][]vterm.Cell{
		makeLine("eeeee", width),
		makeLine("fffff", width),
		makeLine("ggggg", width),
	}
	term.ViewOffset = 1
	term.SetSelection(4, 5, 1, 3, true, false)

	canvas := NewCanvas(width, height)
	RenderSnapshotWithCanvas(canvas, NewVTermSnapshot(term, false), width, height, vterm.Color{Type: vterm.ColorDefault}, vterm.Color{Type: vterm.ColorDefault})

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			reversed := canvas.Cells[y][x].Style.Reverse
			switch y {
			case 0:
				if x < 1 && reversed {
					t.Fatalf("expected start line to begin at x=1, unexpected reverse at x=%d y=%d", x, y)
				}
				if x >= 1 && !reversed {
					t.Fatalf("expected start line highlight from x=1, missing reverse at x=%d y=%d", x, y)
				}
			case 1:
				if !reversed {
					t.Fatalf("expected middle line to be fully highlighted, missing reverse at x=%d y=%d", x, y)
				}
			case 2:
				if x <= 4 && !reversed {
					t.Fatalf("expected end line to highlight through x=4, missing reverse at x=%d y=%d", x, y)
				}
				if x > 4 && reversed {
					t.Fatalf("expected end line to stop at x=4, unexpected reverse at x=%d y=%d", x, y)
				}
			}
		}
	}
}

func makeLine(text string, width int) []vterm.Cell {
	line := vterm.MakeBlankLine(width)
	for i, r := range text {
		if i >= width {
			break
		}
		line[i] = vterm.Cell{Rune: r, Width: 1}
	}
	return line
}
