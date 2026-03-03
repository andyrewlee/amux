package app

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

func TestFrameLineCacheRendererStableOutput(t *testing.T) {
	var r FrameLineCacheRenderer
	canvas := lipgloss.NewCanvas(3, 2)
	setCanvasContent(canvas, 0, 0, "ABC")
	setCanvasContent(canvas, 0, 1, "DEF")

	first := r.Render(canvas)
	second := r.Render(canvas)

	if first != second {
		t.Fatalf("expected stable output for unchanged frame")
	}
}

func TestFrameLineCacheRendererReusesUnchangedLineCache(t *testing.T) {
	var r FrameLineCacheRenderer
	canvas := lipgloss.NewCanvas(3, 2)
	setCanvasContent(canvas, 0, 0, "AAA")
	setCanvasContent(canvas, 0, 1, "BBB")
	_ = r.Render(canvas)

	// If unchanged rows are not re-rendered, this sentinel should survive.
	r.lines[0] = "SENTINEL"

	setCanvasContent(canvas, 0, 1, "CCC")
	out := r.Render(canvas)

	if !strings.Contains(out, "SENTINEL") {
		t.Fatalf("expected unchanged first row to reuse cached ANSI line")
	}
	if !strings.Contains(out, "CCC") {
		t.Fatalf("expected updated second row content in output")
	}
}

func TestFrameLineCacheRendererResize(t *testing.T) {
	var r FrameLineCacheRenderer
	canvas := lipgloss.NewCanvas(2, 1)
	setCanvasContent(canvas, 0, 0, "AB")
	_ = r.Render(canvas)

	canvas.Resize(3, 1)
	setCanvasContent(canvas, 0, 0, "XYZ")
	out := r.Render(canvas)
	if !strings.Contains(out, "XYZ") {
		t.Fatalf("expected resized content in output")
	}
}

func setCanvasContent(canvas *lipgloss.Canvas, x, y int, text string) {
	if canvas == nil {
		return
	}
	for _, ch := range text {
		cell := uv.Cell{Content: string(ch), Width: 1}
		canvas.SetCell(x, y, &cell)
		x++
	}
}
