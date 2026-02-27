package common

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// RenderHelpItem renders a single help item for inline help bars.
func RenderHelpItem(styles Styles, key, desc string) string {
	return styles.HelpKey.Render(key) + styles.HelpDesc.Render(":"+desc)
}

// WrapHelpItems wraps pre-rendered help items into multiple lines constrained by width.
func WrapHelpItems(items []string, width int) []string {
	if len(items) == 0 {
		return []string{""}
	}
	if width <= 0 {
		return []string{strings.Join(items, "  ")}
	}

	var lines []string
	current := ""
	currentWidth := 0
	sep := "  "
	sepWidth := lipgloss.Width(sep)

	for _, item := range items {
		itemWidth := lipgloss.Width(item)
		if current == "" {
			current = item
			currentWidth = itemWidth
			continue
		}
		if currentWidth+sepWidth+itemWidth <= width {
			current += sep + item
			currentWidth += sepWidth + itemWidth
			continue
		}
		lines = append(lines, current)
		current = item
		currentWidth = itemWidth
	}

	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		lines = append(lines, "")
	}
	return lines
}
