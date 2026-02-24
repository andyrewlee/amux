package common

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func viewDimensions(view string) (width, height int) {
	lines := strings.Split(view, "\n")
	height = len(lines)
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			width = w
		}
	}
	return width, height
}

// View renders the dialog
func (d *Dialog) View() string {
	if !d.visible {
		return ""
	}

	lines := d.renderLines()
	content := strings.Join(lines, "\n")
	return d.dialogStyle().Render(content)
}

// Cursor returns the cursor position relative to the dialog view.
func (d *Dialog) Cursor() *tea.Cursor {
	if !d.visible {
		return nil
	}

	var input *textinput.Model
	var prefix strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)
	prefix.WriteString(titleStyle.Render(d.title))
	prefix.WriteString("\n")

	switch d.dtype {
	case DialogInput:
		if d.message != "" {
			msgStyle := lipgloss.NewStyle().Foreground(ColorMuted).Width(d.dialogContentWidth())
			prefix.WriteString(msgStyle.Render(d.message))
			prefix.WriteString("\n\n")
		}
		input = &d.input
	case DialogSelect:
		if d.filterEnabled {
			if d.message != "" {
				wrapped := lipgloss.NewStyle().Width(d.dialogContentWidth()).Render(d.message)
				prefix.WriteString(wrapped)
				prefix.WriteString("\n\n")
			}
			input = &d.filterInput
		}
	default:
		return nil
	}

	if input == nil || input.VirtualCursor() || !input.Focused() {
		return nil
	}

	c := input.Cursor()
	if c == nil {
		return nil
	}

	c.Y += lipgloss.Height(prefix.String()) - 1

	// Account for border + padding (Border=1, Padding=(1,2)).
	c.X += 3
	c.Y += 2

	return c
}

func (d *Dialog) dialogContentWidth() int {
	width := 50
	if d.width > 0 {
		width = min(80, max(50, d.width-10))
	}
	return width
}

func (d *Dialog) dialogStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(d.dialogContentWidth())
}

func (d *Dialog) dialogFrame() (frameX, frameY, offsetX, offsetY int) {
	frameX, frameY = d.dialogStyle().GetFrameSize()
	offsetX = frameX / 2
	offsetY = frameY / 2
	return frameX, frameY, offsetX, offsetY
}

func (d *Dialog) renderLines() []string {
	d.optionHits = d.optionHits[:0]
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

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)
	appendLines(titleStyle.Render(d.title))

	switch d.dtype {
	case DialogInput:
		if d.message != "" {
			msgStyle := lipgloss.NewStyle().Foreground(ColorMuted)
			appendLines(msgStyle.Render(d.message))
			appendBlank(1)
		}
		appendLines(d.input.View())
		// Show validation error if present
		if d.validationErr != "" {
			errStyle := lipgloss.NewStyle().Foreground(ColorError)
			appendLines(errStyle.Render(d.validationErr))
		}
		// Render checkbox if configured
		if d.checkboxLabel != "" {
			appendBlank(1)
			checkbox := "[ ]"
			if d.checkboxValue {
				checkbox = "[" + Icons.Clean + "]"
			}
			checkboxStyle := lipgloss.NewStyle().Foreground(ColorForeground)
			if d.checkboxFocused {
				checkboxStyle = checkboxStyle.Foreground(ColorPrimary)
			}
			checkboxLine := len(lines)
			checkboxText := checkbox + " " + d.checkboxLabel
			appendLines(checkboxStyle.Render(checkboxText))
			// Set hit region for checkbox click handling
			d.checkboxHit = HitRegion{
				X:      0,
				Y:      checkboxLine,
				Width:  d.dialogContentWidth(),
				Height: 1,
			}
		}
		// Render second checkbox if configured
		if d.checkbox2Label != "" {
			if d.checkboxLabel == "" {
				appendBlank(1)
			}
			checkbox2 := "[ ]"
			if d.checkbox2Value {
				checkbox2 = "[" + Icons.Clean + "]"
			}
			checkbox2Style := lipgloss.NewStyle().Foreground(ColorForeground)
			if d.checkbox2Focused {
				checkbox2Style = checkbox2Style.Foreground(ColorPrimary)
			}
			checkbox2Line := len(lines)
			checkbox2Text := checkbox2 + " " + d.checkbox2Label
			appendLines(checkbox2Style.Render(checkbox2Text))
			d.checkbox2Hit = HitRegion{
				X:      0,
				Y:      checkbox2Line,
				Width:  d.dialogContentWidth(),
				Height: 1,
			}
		}
		// Render third checkbox if configured
		if d.checkbox3Label != "" {
			if d.checkboxLabel == "" && d.checkbox2Label == "" {
				appendBlank(1)
			}
			checkbox3 := "[ ]"
			if d.checkbox3Value {
				checkbox3 = "[" + Icons.Clean + "]"
			}
			checkbox3Style := lipgloss.NewStyle().Foreground(ColorForeground)
			if d.checkbox3Focused {
				checkbox3Style = checkbox3Style.Foreground(ColorPrimary)
			}
			checkbox3Line := len(lines)
			checkbox3Text := checkbox3 + " " + d.checkbox3Label
			appendLines(checkbox3Style.Render(checkbox3Text))
			d.checkbox3Hit = HitRegion{
				X:      0,
				Y:      checkbox3Line,
				Width:  d.dialogContentWidth(),
				Height: 1,
			}
		}
		appendBlank(1)
		line := d.renderInputButtonsLine(len(lines))
		lines = append(lines, line)
	case DialogConfirm:
		appendLines(d.message)
		appendBlank(1)
		lines = append(lines, d.renderOptionsLines(len(lines))...)
	case DialogSelect:
		if d.message != "" {
			appendLines(d.message)
			appendBlank(1)
		}
		lines = append(lines, d.renderOptionsLines(len(lines))...)
	}

	if d.showKeymapHints {
		helpStyle := lipgloss.NewStyle().
			Foreground(ColorMuted).
			MarginTop(1)
		appendBlank(1)
		appendLines(helpStyle.Render(d.helpText()))
	}

	return lines
}

func (d *Dialog) renderOptionsLines(baseLine int) []string {
	if d.id == "agent-picker" {
		return d.renderAgentPickerOptions(baseLine)
	}
	if d.verticalLayout {
		return d.renderVerticalOptionsLines(baseLine)
	}
	return []string{d.renderHorizontalOptionsLine(baseLine)}
}

func (d *Dialog) renderVerticalOptionsLines(baseLine int) []string {
	var lines []string
	lineIndex := baseLine

	indices := make([]int, len(d.options))
	for i := range d.options {
		indices[i] = i
	}

	for cursorIdx, originalIdx := range indices {
		opt := d.options[originalIdx]
		cursor := Icons.CursorEmpty + " "
		if cursorIdx == d.cursor {
			cursor = Icons.Cursor + " "
		}

		nameStyle := lipgloss.NewStyle().Foreground(ColorForeground)
		if cursorIdx == d.cursor {
			nameStyle = nameStyle.Bold(true)
		}
		line := cursor + nameStyle.Render(opt)

		width := d.dialogContentWidth()
		d.addOptionHit(cursorIdx, originalIdx, lineIndex, 0, width)
		lines = append(lines, line)
		lineIndex++
	}
	return lines
}

func (d *Dialog) renderHorizontalOptionsLine(baseLine int) string {
	bracketStyle := lipgloss.NewStyle().Foreground(ColorPrimary)
	selectedTextStyle := lipgloss.NewStyle().Foreground(ColorForeground)
	normalStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	const gap = 2 // gap between buttons
	var b strings.Builder
	x := 0
	for i, opt := range d.options {
		var rendered string
		if i == d.cursor {
			rendered = bracketStyle.Render("[") + selectedTextStyle.Render(" "+opt+" ") + bracketStyle.Render("]")
		} else {
			rendered = normalStyle.Render("[ " + opt + " ]")
		}
		width := min(lipgloss.Width(rendered), d.dialogContentWidth()-x)
		// Extend hit region to include gap (for easier clicking)
		hitWidth := width
		if i < len(d.options)-1 {
			hitWidth += gap // extend to cover the gap after this button
		}
		d.addOptionHit(i, i, baseLine, x, hitWidth)
		b.WriteString(rendered)
		if i < len(d.options)-1 {
			b.WriteString("  ")
			x += width + gap
		} else {
			x += width
		}
	}

	return b.String()
}

func (d *Dialog) renderInputButtonsLine(baseLine int) string {
	bracketStyle := lipgloss.NewStyle().Foreground(ColorPrimary)
	textStyle := lipgloss.NewStyle().Foreground(ColorForeground)
	normalStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	ok := bracketStyle.Render("[") + textStyle.Render(" OK ") + bracketStyle.Render("]")
	cancel := normalStyle.Render("[ Cancel ]")

	const gap = 2
	okWidth := lipgloss.Width(ok)
	// Extend OK hit region to include gap
	d.addOptionHit(0, 0, baseLine, 0, okWidth+gap)

	cancelX := okWidth + gap
	cancelWidth := min(lipgloss.Width(cancel), max(0, d.dialogContentWidth()-cancelX))
	d.addOptionHit(1, 1, baseLine, cancelX, cancelWidth)

	return ok + "  " + cancel
}

func (d *Dialog) addOptionHit(cursorIdx, optionIdx, line, x, width int) {
	if width <= 0 {
		return
	}
	d.optionHits = append(d.optionHits, dialogOptionHit{
		cursorIndex: cursorIdx,
		optionIndex: optionIdx,
		region: HitRegion{
			X:      x,
			Y:      line,
			Width:  width,
			Height: 1,
		},
	})
}

func (d *Dialog) helpText() string {
	switch d.dtype {
	case DialogInput:
		if d.checkboxLabel != "" || d.checkbox2Label != "" || d.checkbox3Label != "" {
			return "↑/↓: navigate • space: toggle • enter: confirm • esc: cancel"
		}
		return "enter: confirm • esc: cancel • click OK/Cancel"
	case DialogConfirm:
		return "←/→ or tab: choose • enter: confirm • esc: cancel"
	case DialogSelect:
		if d.filterEnabled {
			return "type to filter • ↑/↓ or tab: move • enter: select • esc: cancel"
		}
		if d.verticalLayout {
			return "↑/↓ or tab: move • enter: select • esc: cancel"
		}
		return "←/→ or tab: move • enter: select • esc: cancel"
	default:
		return "enter: confirm • esc: cancel"
	}
}
