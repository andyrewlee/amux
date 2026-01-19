package app

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
)

// buildBorderedPane creates a bordered pane with exact dimensions, manually drawing the border
func buildBorderedPane(content string, width, height int, focused bool) string {
	if width < 3 || height < 3 {
		return ""
	}

	borderColor := common.ColorBorder
	// Border characters - use thick borders for focused panes
	topLeft, topRight, bottomLeft, bottomRight := "╭", "╮", "╰", "╯"
	horizontal, vertical := "─", "│"
	if focused {
		borderColor = common.ColorBorderFocused
		// Use thick border characters for focused pane
		topLeft, topRight, bottomLeft, bottomRight = "┏", "┓", "┗", "┛"
		horizontal, vertical = "━", "┃"
	}
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	// Content area dimensions (inside border and padding)
	contentWidth := width - 4   // 2 for border, 2 for padding
	contentHeight := height - 2 // 2 for border (top + bottom)
	if contentWidth < 1 {
		contentWidth = 1
	}
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Truncate and pad content to exact size
	lines := strings.Split(content, "\n")
	if len(lines) > contentHeight {
		lines = lines[:contentHeight]
	}
	// Pad with empty lines if needed
	for len(lines) < contentHeight {
		lines = append(lines, "")
	}
	// Truncate each line to width and pad (ANSI-aware to preserve styled content)
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w > contentWidth {
			// Truncate using ANSI-aware function to preserve escape sequences
			lines[i] = ansi.Truncate(line, contentWidth, "")
		} else if w < contentWidth {
			// Pad with spaces
			lines[i] = line + strings.Repeat(" ", contentWidth-w)
		}
	}

	// Build the box
	var result strings.Builder
	innerWidth := width - 2 // width inside left/right borders

	// Top border
	result.WriteString(borderStyle.Render(topLeft + strings.Repeat(horizontal, innerWidth) + topRight))
	result.WriteString("\n")

	// Content lines with side borders and padding
	for _, line := range lines {
		result.WriteString(borderStyle.Render(vertical))
		result.WriteString(" ") // left padding
		result.WriteString(line)
		result.WriteString(" ") // right padding
		result.WriteString(borderStyle.Render(vertical))
		result.WriteString("\n")
	}

	// Bottom border
	result.WriteString(borderStyle.Render(bottomLeft + strings.Repeat(horizontal, innerWidth) + bottomRight))

	return result.String()
}

func borderDrawables(x, y, width, height int, focused bool) []*compositor.StringDrawable {
	if width < 3 || height < 3 {
		return nil
	}

	borderColor := common.ColorBorder
	topLeft, topRight, bottomLeft, bottomRight := "╭", "╮", "╰", "╯"
	horizontal, vertical := "─", "│"
	if focused {
		borderColor = common.ColorBorderFocused
		topLeft, topRight, bottomLeft, bottomRight = "┏", "┓", "┗", "┛"
		horizontal, vertical = "━", "┃"
	}

	style := lipgloss.NewStyle().Foreground(borderColor)
	innerWidth := width - 2

	top := style.Render(topLeft + strings.Repeat(horizontal, innerWidth) + topRight)
	bottom := style.Render(bottomLeft + strings.Repeat(horizontal, innerWidth) + bottomRight)

	vertLines := make([]string, height-2)
	for i := range vertLines {
		vertLines[i] = vertical
	}
	vertContent := style.Render(strings.Join(vertLines, "\n"))

	return []*compositor.StringDrawable{
		compositor.NewStringDrawable(top, x, y),
		compositor.NewStringDrawable(bottom, x, y+height-1),
		compositor.NewStringDrawable(vertContent, x, y+1),
		compositor.NewStringDrawable(vertContent, x+width-1, y+1),
	}
}

func centerOffset(container, content int) int {
	if container <= content {
		return 0
	}
	return (container - content) / 2
}

func findButtonRegion(lines []string, button string) (common.HitRegion, bool) {
	buttonLines := strings.Split(button, "\n")
	if len(buttonLines) == 0 {
		return common.HitRegion{}, false
	}
	strippedButtonLines := make([]string, len(buttonLines))
	for i, line := range buttonLines {
		strippedButtonLines[i] = ansi.Strip(line)
		if strippedButtonLines[i] == "" && len(buttonLines) == 1 {
			return common.HitRegion{}, false
		}
	}
	buttonWidth, buttonHeight := viewDimensions(button)

	for i := 0; i+len(buttonLines) <= len(lines); i++ {
		strippedLine := ansi.Strip(lines[i])
		idx := strings.Index(strippedLine, strippedButtonLines[0])
		if idx < 0 {
			continue
		}
		start := ansi.StringWidth(strippedLine[:idx])
		matched := true
		for j := 1; j < len(buttonLines); j++ {
			lineStripped := ansi.Strip(lines[i+j])
			if idx >= len(lineStripped) || !strings.HasPrefix(lineStripped[idx:], strippedButtonLines[j]) {
				matched = false
				break
			}
		}
		if matched {
			return common.HitRegion{
				X:      start,
				Y:      i,
				Width:  buttonWidth,
				Height: buttonHeight,
			}, true
		}
	}

	return common.HitRegion{}, false
}
