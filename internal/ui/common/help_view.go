package common

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// View renders the help overlay
func (h *HelpOverlay) View() string {
	if !h.visible {
		return ""
	}

	// Calculate box width (wider layout: 70 chars, responsive)
	boxWidth := 70
	if h.width > 0 && boxWidth > h.width-4 {
		boxWidth = h.width - 4
	}
	// Minimum width of 40, but never wider than terminal
	if boxWidth < 40 && h.width > 0 {
		boxWidth = min(40, h.width-2)
	}
	if boxWidth < 20 {
		boxWidth = 20 // absolute minimum for readability
	}

	// Build content lines with height constraint
	var lines []string

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary()).
		Render("Help")
	lines = append(lines, title)
	lines = append(lines, "")

	// Calculate visible range for scrolling
	maxVisible := h.maxVisibleSections()
	startIdx := h.scrollOffset
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + maxVisible
	if endIdx > len(h.sections) {
		endIdx = len(h.sections)
	}

	// Clamp scroll offset
	if h.scrollOffset > len(h.sections)-maxVisible {
		h.scrollOffset = max(0, len(h.sections)-maxVisible)
	}

	// Scroll indicator at top if not at beginning
	if startIdx > 0 {
		scrollUp := lipgloss.NewStyle().
			Foreground(ColorMuted()).
			Render("  ↑ more above")
		lines = append(lines, scrollUp)
	}

	// Sections
	for i := startIdx; i < endIdx; i++ {
		section := h.sections[i]
		isSelected := i == h.selectedSection
		isMatch := h.isSearchMatch(i)

		// Section header with selection indicator
		var prefix string
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorMuted())
		if isSelected {
			prefix = Icons.Cursor + " "
			headerStyle = headerStyle.Foreground(ColorPrimary())
		} else {
			prefix = "  "
		}
		if isMatch {
			headerStyle = headerStyle.Background(lipgloss.Color("#3d4f5f"))
		}

		lines = append(lines, prefix+headerStyle.Render(section.Title))

		// Bindings
		keyWidth := 18 // wider for longer key combos
		for _, binding := range section.Bindings {
			keyStyle := lipgloss.NewStyle().
				Foreground(ColorPrimary()).
				Width(keyWidth)
			descStyle := lipgloss.NewStyle().
				Foreground(ColorForeground())

			lines = append(lines, "  "+keyStyle.Render(binding.Key)+descStyle.Render(binding.Desc))
		}
		lines = append(lines, "") // blank line between sections
	}

	// Scroll indicator at bottom if not at end
	if endIdx < len(h.sections) {
		scrollDown := lipgloss.NewStyle().
			Foreground(ColorMuted()).
			Render("  ↓ more below")
		lines = append(lines, scrollDown)
	}

	// Divider
	divider := lipgloss.NewStyle().
		Foreground(ColorBorder()).
		Render(strings.Repeat("─", boxWidth-6))
	lines = append(lines, divider)

	// Footer with navigation hints and doc link
	var footerLine string
	if h.searchMode {
		// Search input mode
		searchPrompt := lipgloss.NewStyle().
			Foreground(ColorPrimary()).
			Render("/")
		searchText := lipgloss.NewStyle().
			Foreground(ColorForeground()).
			Render(h.searchQuery + "█")
		footerLine = searchPrompt + searchText
	} else {
		// Navigation hints on left, clickable doc link on right
		navHints := lipgloss.NewStyle().
			Foreground(ColorMuted()).
			Render("j/k:section  /:search  Esc:close")

		// Create styled doc link
		docText := "Documentation"
		docLink := lipgloss.NewStyle().
			Foreground(ColorPrimary()).
			Underline(true).
			Render(docText)

		// Calculate spacing to right-align doc link
		navWidth := lipgloss.Width(navHints)
		docWidth := len(docText)     // visible width only
		contentWidth := boxWidth - 6 // account for padding
		spacing := contentWidth - navWidth - docWidth
		if spacing < 2 {
			spacing = 2
		}

		// Track doc link position for click detection
		h.docLinkX = navWidth + spacing
		h.docLinkWidth = docWidth

		footerLine = navHints + strings.Repeat(" ", spacing) + docLink
	}
	lines = append(lines, footerLine)

	// Join all lines
	content := strings.Join(lines, "\n")

	// Create the overlay box (no lipgloss.Place - centering done by app_view.go)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderFocused()).
		Padding(1, 2).
		Width(boxWidth).
		Render(content)

	// Cache dimensions for hit testing
	h.dialogWidth = lipgloss.Width(box)
	h.dialogHeight = lipgloss.Height(box)

	return box
}

// ContainsClick returns true if the click coordinates are inside the dialog.
// The x, y coordinates should be absolute screen coordinates.
func (h *HelpOverlay) ContainsClick(x, y int) bool {
	if !h.visible || h.dialogWidth == 0 || h.dialogHeight == 0 {
		return false
	}

	// Calculate dialog position (centered, clamped to screen bounds)
	dialogX := (h.width - h.dialogWidth) / 2
	dialogY := (h.height - h.dialogHeight) / 2
	if dialogX < 0 {
		dialogX = 0
	}
	if dialogY < 0 {
		dialogY = 0
	}

	return x >= dialogX && x < dialogX+h.dialogWidth &&
		y >= dialogY && y < dialogY+h.dialogHeight
}

// isDocLinkClick checks if a click is on the documentation link.
func (h *HelpOverlay) isDocLinkClick(x, y int) bool {
	// Don't check for doc link clicks in search mode (link isn't visible)
	if !h.visible || h.searchMode || h.dialogWidth == 0 || h.dialogHeight == 0 {
		return false
	}

	// Calculate dialog position (centered, clamped to screen bounds)
	dialogX := (h.width - h.dialogWidth) / 2
	dialogY := (h.height - h.dialogHeight) / 2
	if dialogX < 0 {
		dialogX = 0
	}
	if dialogY < 0 {
		dialogY = 0
	}

	// Content area starts after border (1) and padding (1)
	contentStartX := dialogX + 1 + 2 // border + left padding
	contentStartY := dialogY + 1 + 1 // border + top padding

	// Footer is the last line of rendered content
	// Use dialogHeight minus frame (border=2 + padding=2) to get actual content height
	contentHeight := h.dialogHeight - 4 // 1 top border + 1 top padding + 1 bottom padding + 1 bottom border
	if contentHeight < 1 {
		return false
	}
	footerY := contentStartY + contentHeight - 1

	// Check if click is on the footer line and within doc link X range
	localX := x - contentStartX
	if y == footerY && localX >= h.docLinkX && localX < h.docLinkX+h.docLinkWidth {
		return true
	}

	return false
}

// isSearchMatch returns true if the section index is in search matches
func (h *HelpOverlay) isSearchMatch(idx int) bool {
	for _, m := range h.searchMatches {
		if m == idx {
			return true
		}
	}
	return false
}

// RenderHelpItem renders a single help item for inline help bars
func RenderHelpItem(styles Styles, key, desc string) string {
	return styles.HelpKey.Render(key) + styles.HelpDesc.Render(":"+desc)
}

// RenderHelpBarItems renders multiple help items for an inline help bar
func RenderHelpBarItems(styles Styles, items []HelpBinding) string {
	var parts []string
	for _, item := range items {
		parts = append(parts, RenderHelpItem(styles, item.Key, item.Desc))
	}
	return strings.Join(parts, "  ")
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
