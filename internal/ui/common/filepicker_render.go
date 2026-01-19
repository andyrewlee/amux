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
		BorderForeground(ColorPrimary).
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
		Foreground(ColorPrimary).
		MarginBottom(1)
	appendLines(titleStyle.Render(fp.title))
	appendBlank(2)

	// Current path
	pathStyle := lipgloss.NewStyle().
		Foreground(ColorSecondary)
	appendLines(pathStyle.Render(fp.currentPath))
	appendBlank(2)

	// Input
	appendLines(fp.input.View())
	appendBlank(2)

	// Entries
	totalRows := fp.displayCount()
	end := min(fp.scrollOffset+fp.maxVisible, totalRows)
	for i := fp.scrollOffset; i < end; i++ {
		cursor := "  "
		if i == fp.cursor {
			cursor = "> "
		}

		lineIndex := len(lines)
		if i == 0 {
			label := "Use this directory"
			style := lipgloss.NewStyle().Foreground(ColorForeground)
			if i == fp.cursor {
				style = style.Background(ColorSelection).Bold(true)
			}
			line := cursor + style.Render(label)
			fp.addRowHit(i, lineIndex, line)
			lines = append(lines, line)
			continue
		}

		idx := fp.filteredIdx[i-1]
		entry := fp.entries[idx]

		name := entry.Name()
		style := lipgloss.NewStyle().Foreground(ColorForeground)
		if entry.IsDir() {
			name += "/"
			style = lipgloss.NewStyle().Foreground(ColorSecondary).Bold(i == fp.cursor)
		}
		if i == fp.cursor {
			style = style.Background(ColorSelection)
		}

		line := cursor + style.Render(name)
		fp.addRowHit(i, lineIndex, line)
		lines = append(lines, line)
	}

	if len(fp.filteredIdx) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(ColorMuted).Render("No matches"))
	} else if totalRows > fp.maxVisible {
		indicator := lipgloss.NewStyle().Foreground(ColorMuted).Render(
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

	return lines
}

func (fp *FilePicker) renderButtonsLine(baseLine int) string {
	buttonStyle := lipgloss.NewStyle().
		Foreground(ColorForeground).
		Background(ColorSelection).
		Padding(0, 1)

	buttons := []struct {
		id    string
		label string
	}{
		{id: "open", label: buttonStyle.Render("Open")},
		{id: "open-typed", label: buttonStyle.Render("Open typed")},
		{id: "autocomplete", label: buttonStyle.Render("Autocomplete")},
		{id: "up", label: buttonStyle.Render("Up")},
		{id: "hidden", label: buttonStyle.Render(fp.hiddenLabel())},
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
	width := min(lipgloss.Width(line), filePickerContentWidth)
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

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)
	pathStyle := lipgloss.NewStyle().
		Foreground(ColorSecondary)

	prefix := titleStyle.Render(fp.title) + "\n\n" + pathStyle.Render(fp.currentPath) + "\n\n"
	c.Y += lipgloss.Height(prefix)

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
		fp.helpItem("enter", "open"),
		fp.helpItem("esc", "cancel"),
		fp.helpItem("↑", "up"),
		fp.helpItem("↓", "down"),
		fp.helpItem("ctrl+n/p", "move"),
		fp.helpItem("tab", "autocomplete"),
		fp.helpItem("/", "open typed"),
		fp.helpItem("backspace", "parent"),
		fp.helpItem("ctrl+h", "hidden"),
	}
	return WrapHelpItems(items, width)
}

func (fp *FilePicker) hiddenLabel() string {
	if fp.showHidden {
		return "Hide hidden"
	}
	return "Show hidden"
}
