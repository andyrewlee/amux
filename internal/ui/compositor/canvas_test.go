package compositor

import (
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
