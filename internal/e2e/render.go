package e2e

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/vterm"
)

const (
	syncBegin = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"
)

// RenderViewToBuffer renders a tea.View into a UV buffer for inspection.
func RenderViewToBuffer(view tea.View, width, height int) *uv.Buffer {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	content := view.Content
	if content != "" {
		content = strings.ReplaceAll(content, syncBegin, "")
		content = strings.ReplaceAll(content, syncEnd, "")
		content = normalizeSnapshotContent(content)
	}
	buf := uv.NewBuffer(width, height)
	styled := uv.NewStyledString(content)
	screen := bufferScreen{Buffer: buf}
	styled.Draw(screen, uv.Rect(0, 0, width, height))
	return screen.Buffer
}

// BufferToASCII converts a UV buffer to ASCII text. Trailing spaces are trimmed.
func BufferToASCII(buf *uv.Buffer) string {
	if buf == nil {
		return ""
	}
	lines := make([]string, 0, len(buf.Lines))
	for _, line := range buf.Lines {
		var b strings.Builder
		for _, cell := range line {
			if cell.Width == 0 {
				continue
			}
			content := cell.Content
			if content == "" {
				content = " "
			}
			if !isASCII(content) {
				b.WriteByte('?')
				continue
			}
			b.WriteString(content)
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return strings.Join(lines, "\n")
}

// CellsToASCII converts a vterm screen buffer to ASCII text.
func CellsToASCII(screen [][]vterm.Cell) string {
	if len(screen) == 0 {
		return ""
	}
	lines := make([]string, 0, len(screen))
	for _, row := range screen {
		var b strings.Builder
		for _, cell := range row {
			if cell.Width == 0 {
				continue
			}
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			if r > 0x7f {
				b.WriteByte('?')
				continue
			}
			b.WriteRune(r)
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return strings.Join(lines, "\n")
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7f {
			return false
		}
	}
	return true
}

func normalizeSnapshotContent(s string) string {
	if s == "" {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r > 0x7f {
			return '?'
		}
		return r
	}, s)
}

type bufferScreen struct {
	*uv.Buffer
}

func (b bufferScreen) WidthMethod() uv.WidthMethod {
	return ansi.GraphemeWidth
}
