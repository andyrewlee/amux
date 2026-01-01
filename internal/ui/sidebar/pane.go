package sidebar

import (
	"strings"

	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/charmbracelet/lipgloss"
)

const panePaddingX = 1

func paneContentWidth(totalWidth int) int {
	innerWidth := totalWidth - 2
	if innerWidth < 1 {
		return 1
	}
	padding := panePadding(innerWidth)
	contentWidth := innerWidth - (padding * 2)
	if contentWidth < 1 {
		return 1
	}
	return contentWidth
}

func panePadding(innerWidth int) int {
	if innerWidth < (panePaddingX*2 + 1) {
		return 0
	}
	return panePaddingX
}

func wrapPane(content string, width int, focused bool) string {
	border := lipgloss.RoundedBorder()
	borderColor := common.ColorBorder
	if focused {
		borderColor = common.ColorBorderFocused
	}
	return renderBorder(content, width, border, borderColor)
}

func renderBorder(content string, width int, border lipgloss.Border, borderColor lipgloss.Color) string {
	innerWidth := width - 2
	if innerWidth < 1 {
		return content
	}
	padding := panePadding(innerWidth)
	contentWidth := innerWidth - (padding * 2)
	if contentWidth < 1 {
		contentWidth = 1
	}

	content = lipgloss.NewStyle().Width(contentWidth).Render(content)
	lines := strings.Split(content, "\n")
	pad := strings.Repeat(" ", padding)
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	var b strings.Builder
	top := border.TopLeft + strings.Repeat(border.Top, innerWidth) + border.TopRight
	bottom := border.BottomLeft + strings.Repeat(border.Bottom, innerWidth) + border.BottomRight
	b.WriteString(borderStyle.Render(top))
	b.WriteString("\n")
	left := borderStyle.Render(border.Left)
	right := borderStyle.Render(border.Right)
	for i, line := range lines {
		b.WriteString(left)
		b.WriteString(pad)
		b.WriteString(line)
		b.WriteString(pad)
		b.WriteString(right)
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(borderStyle.Render(bottom))
	return b.String()
}
