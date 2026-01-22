package compositor

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestCanvasRenderDoubleBuffer(t *testing.T) {
	canvas := NewCanvas(4, 1)
	style := vterm.Style{Fg: HexColor("#ffffff")}
	canvas.Fill(style)
	canvas.DrawText(0, 0, "abcd", style)

	first := canvas.Render()
	firstPlain := ansi.Strip(first)

	canvas.DrawText(0, 0, "wxyz", style)
	_ = canvas.Render()

	if strings.Contains(ansi.Strip(first), "wxyz") {
		t.Fatalf("expected prior render output to remain stable across next render")
	}
	if !strings.Contains(firstPlain, "abcd") {
		t.Fatalf("expected first render to contain original text, got %q", firstPlain)
	}
}

func TestCanvasRenderSuppressesUnderlineOnBlankCells(t *testing.T) {
	canvas := NewCanvas(3, 1)
	style := vterm.Style{Underline: true}
	for x := 0; x < 3; x++ {
		canvas.SetCell(x, 0, vterm.Cell{Rune: ' ', Width: 1, Style: style})
	}

	out := canvas.Render()
	if containsSGRParam(out, 4) {
		t.Fatalf("expected no underline SGR for blank cells, got %q", out)
	}
}

func TestRenderTerminalWithCanvasClampsOffscreenSelection(t *testing.T) {
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
	RenderTerminalWithCanvas(canvas, term, width, height, false, vterm.Color{Type: vterm.ColorDefault}, vterm.Color{Type: vterm.ColorDefault})

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if !canvas.Cells[y][x].Style.Reverse {
				t.Fatalf("expected selection to clamp off-screen endpoints; missing reverse at x=%d y=%d", x, y)
			}
		}
	}
}

func TestRenderTerminalWithCanvasClampsStartAboveViewport(t *testing.T) {
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
	RenderTerminalWithCanvas(canvas, term, width, height, false, vterm.Color{Type: vterm.ColorDefault}, vterm.Color{Type: vterm.ColorDefault})

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

func TestRenderTerminalWithCanvasClampsEndBelowViewport(t *testing.T) {
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
	RenderTerminalWithCanvas(canvas, term, width, height, false, vterm.Color{Type: vterm.ColorDefault}, vterm.Color{Type: vterm.ColorDefault})

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

func TestRenderTerminalWithCanvasReverseSelectionAnchor(t *testing.T) {
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
	RenderTerminalWithCanvas(canvas, term, width, height, false, vterm.Color{Type: vterm.ColorDefault}, vterm.Color{Type: vterm.ColorDefault})

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

func containsSGRParam(s string, target int) bool {
	targetStr := strconv.Itoa(target)
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b || i+1 >= len(s) || s[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(s) && s[j] != 'm' {
			j++
		}
		if j >= len(s) {
			break
		}
		params := strings.Split(s[i+2:j], ";")
		for _, param := range params {
			if param == targetStr {
				return true
			}
		}
		i = j
	}
	return false
}
