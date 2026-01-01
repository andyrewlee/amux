package sidebar

import (
	"strings"

	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/charmbracelet/lipgloss"
)

const focusGutterWidth = 1

func paneContentWidth(totalWidth int) int {
	width := totalWidth - 2 - focusGutterWidth
	if width < 1 {
		return 1
	}
	return width
}

func wrapPane(content string, width int, focused bool) string {
	contentWidth := paneContentWidth(width)
	content = lipgloss.NewStyle().Width(contentWidth).Render(content)
	content = addFocusGutter(content, focused)
	return lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Render(content)
}

func addFocusGutter(content string, focused bool) string {
	lines := strings.Split(content, "\n")
	gutter := " "
	gutterStyle := lipgloss.NewStyle()
	if focused {
		gutter = "│"
		gutterStyle = gutterStyle.Foreground(common.ColorBorderFocused)
	}
	for i := range lines {
		if focused {
			lines[i] = gutterStyle.Render(gutter) + lines[i]
		} else {
			lines[i] = gutter + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
