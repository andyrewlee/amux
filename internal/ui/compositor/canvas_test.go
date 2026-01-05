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
