package common

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// View renders the picker
func (fp *FilePicker) View() string {
	if !fp.visible {
		return ""
	}

	lines := fp.renderLines()
	content := strings.Join(lines, "\n")
	return fp.dialogStyle().Render(content)
}

const filePickerContentWidth = 55

func (fp *FilePicker) dialogStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary()).
		Padding(1, 2).
		Width(filePickerContentWidth)
}

func (fp *FilePicker) dialogFrame() (frameX, frameY, offsetX, offsetY int) {
	frameX, frameY = fp.dialogStyle().GetFrameSize()
	offsetX = frameX / 2
	offsetY = frameY / 2
	return frameX, frameY, offsetX, offsetY
}

func (fp *FilePicker) dialogBounds(contentHeight int) (x, y, w, h int) {
	frameX, frameY, _, _ := fp.dialogFrame()
	w = filePickerContentWidth + frameX
	h = contentHeight + frameY
	x = (fp.width - w) / 2
	y = (fp.height - h) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y, w, h
}

func (fp *FilePicker) renderLines() []string {
	fp.rowHits = fp.rowHits[:0]
	fp.buttonHits = fp.buttonHits[:0]

	// Content width (dialog width minus horizontal padding)
	contentWidth := filePickerContentWidth - 4 // Padding(1, 2) = 2 chars each side

	lines := []string{}
	appendLines := func(s string) {
		if s == "" {
			return
		}
		lines = append(lines, strings.Split(s, "\n")...)
	}
	appendBlank := func(count int) {
		for i := 0; i < count; i++ {
			lines = append(lines, "")
		}
	}

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary()).
		MarginBottom(1)
	appendLines(titleStyle.Render(fp.title))
	appendBlank(2)

	// Current path - truncate if too long
	pathStyle := lipgloss.NewStyle().
		Foreground(ColorSecondary())
	displayPath := truncateToWidth(fp.currentPath, contentWidth)
	appendLines(pathStyle.Render(displayPath))
	appendBlank(2)

	// Input
	appendLines(fp.input.View())
	appendBlank(2)

	// Entries - truncate names to prevent wrapping
	totalRows := fp.displayCount()
	end := min(fp.scrollOffset+fp.maxVisible, totalRows)
	cursorWidth := 2 // "> " or "  "
	maxNameWidth := contentWidth - cursorWidth

	for i := fp.scrollOffset; i < end; i++ {
		cursor := "  "
		if i == fp.cursor {
			cursor = "> "
		}

		lineIndex := len(lines)
		idx := fp.filteredIdx[i]
		entry := fp.entries[idx]

		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		// Truncate name to fit on one line
		name = truncateToWidth(name, maxNameWidth)

		style := lipgloss.NewStyle().Foreground(ColorForeground())
		if entry.IsDir() {
			style = lipgloss.NewStyle().Foreground(ColorSecondary()).Bold(i == fp.cursor)
		}
		if i == fp.cursor {
			style = style.Background(ColorSelection())
		}

		line := cursor + style.Render(name)
		fp.addRowHit(i, lineIndex, line)
		lines = append(lines, line)
	}

	if len(fp.filteredIdx) == 0 {
		message := "No matches"
		if fp.directoriesOnly {
			message = "No subdirectories"
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(ColorMuted()).Render(message))
	} else if totalRows > fp.maxVisible {
		indicator := lipgloss.NewStyle().Foreground(ColorMuted()).Render(
			fmt.Sprintf("  (%d-%d of %d)", fp.scrollOffset+1, end, totalRows),
		)
		lines = append(lines, indicator)
	}

	// Action buttons
	appendBlank(1)
	lines = append(lines, fp.renderButtonsLine(len(lines)))

	if fp.showKeymapHints {
		appendBlank(1)
		helpWidth := 51
		helpLines := fp.helpLines(helpWidth)
		lines = append(lines, helpLines...)
	}

	fp.lastContentHeight = len(lines)
	return lines
}

// truncateToWidth truncates a string to fit within the given width,
// adding "..." suffix if truncated. Uses lipgloss.Width for accurate
// measurement that accounts for ANSI escape sequences.
func truncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 3 {
		return s
	}
	w := lipgloss.Width(s)
	if w <= maxWidth {
		return s
	}
	// Need to truncate - find the right cut point
	// Account for "..." suffix (3 chars)
	targetWidth := maxWidth - 3
	runes := []rune(s)
	for i := len(runes); i > 0; i-- {
		candidate := string(runes[:i])
		if lipgloss.Width(candidate) <= targetWidth {
			return candidate + "..."
		}
	}
	return "..."
}

func (fp *FilePicker) renderButtonsLine(baseLine int) string {
	buttonStyle := lipgloss.NewStyle().
		Foreground(ColorForeground()).
		Background(ColorSelection()).
		Padding(0, 1)

	buttons := []struct {
		id    string
		label string
	}{
		{id: "open", label: buttonStyle.Render(fp.primaryActionLabel())},
		{id: "cancel", label: buttonStyle.Render("Cancel")},
	}

	var parts []string
	x := 0
	for i, btn := range buttons {
		width := min(lipgloss.Width(btn.label), filePickerContentWidth-x)
		fp.addButtonHit(btn.id, baseLine, x, width)
		parts = append(parts, btn.label)
		x += width
		if i < len(buttons)-1 {
			x += 2
		}
	}

	return strings.Join(parts, "  ")
}

func (fp *FilePicker) addRowHit(index, lineIndex int, line string) {
	width := filePickerContentWidth
	if width <= 0 {
		return
	}
	fp.rowHits = append(fp.rowHits, filePickerRowHit{
		index: index,
		region: HitRegion{
			X:      0,
			Y:      lineIndex,
			Width:  width,
			Height: 1,
		},
	})
}

func (fp *FilePicker) addButtonHit(id string, lineIndex, x, width int) {
	if width <= 0 {
		return
	}
	fp.buttonHits = append(fp.buttonHits, HitRegion{
		ID:     id,
		X:      x,
		Y:      lineIndex,
		Width:  width,
		Height: 1,
	})
}

// Cursor returns the cursor position relative to the file picker view.
func (fp *FilePicker) Cursor() *tea.Cursor {
	if !fp.visible || fp.input.VirtualCursor() || !fp.input.Focused() {
		return nil
	}

	c := fp.input.Cursor()
	if c == nil {
		return nil
	}

	c.Y += fp.inputOffset()

	// Account for border + padding (Border=1, Padding=(1,2)).
	c.X += 3
	c.Y += 2

	return c
}

func (fp *FilePicker) helpItem(key, desc string) string {
	return RenderHelpItem(fp.styles, key, desc)
}

func (fp *FilePicker) helpLines(width int) []string {
	items := []string{
		fp.helpItem("enter", "open/select"),
		fp.helpItem("esc", "cancel"),
		fp.helpItem("↑/↓", "move"),
		fp.helpItem("tab", "enter folder"),
		fp.helpItem("backspace", "parent"),
		fp.helpItem("ctrl+h", "hidden"),
	}
	return WrapHelpItems(items, width)
}

func (fp *FilePicker) primaryActionLabel() string {
	if fp.primaryAction != "" {
		return fp.primaryAction
	}
	return "Open"
}

func (fp *FilePicker) inputOffset() int {
	contentWidth := filePickerContentWidth - 4 // Padding(1, 2) = 2 chars each side

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary()).
		MarginBottom(1)
	pathStyle := lipgloss.NewStyle().
		Foreground(ColorSecondary())

	// Use truncated path to match renderLines() behavior
	displayPath := truncateToWidth(fp.currentPath, contentWidth)

	offset := lipgloss.Height(titleStyle.Render(fp.title))
	offset += 2 // blank lines after title
	offset += lipgloss.Height(pathStyle.Render(displayPath))
	offset += 2 // blank lines after path
	return offset
}
