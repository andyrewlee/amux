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
		Foreground(ColorPrimary()).
		MarginBottom(1)
	prefix.WriteString(titleStyle.Render(d.title))
	prefix.WriteString("\n\n")

	switch d.dtype {
	case DialogInput:
		input = &d.input
	case DialogSelect:
		if d.filterEnabled {
			if d.message != "" {
				prefix.WriteString(d.message)
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
		BorderForeground(ColorPrimary()).
		Padding(1, 2).
		Width(d.dialogContentWidth())
}

func (d *Dialog) dialogFrame() (frameX, frameY, offsetX, offsetY int) {
	frameX, frameY = d.dialogStyle().GetFrameSize()
	offsetX = frameX / 2
	offsetY = frameY / 2
	return frameX, frameY, offsetX, offsetY
}

// renderedLineCount returns the number of content-area lines after the dialog
// style wraps the raw lines. This accounts for word-wrapping that the style's
// fixed width may introduce, ensuring hit regions match rendered positions.
func (d *Dialog) renderedLineCount(rawLines []string) int {
	content := strings.Join(rawLines, "\n")
	rendered := d.dialogStyle().Render(content)
	renderedH := len(strings.Split(rendered, "\n"))
	_, frameY, _, _ := d.dialogFrame()
	return renderedH - frameY
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
		Foreground(ColorPrimary()).
		MarginBottom(1)
	appendLines(titleStyle.Render(d.title))
	appendBlank(1)

	switch d.dtype {
	case DialogInput:
		appendLines(d.input.View())
		// Show validation error if present
		if d.validationErr != "" {
			errStyle := lipgloss.NewStyle().Foreground(ColorError())
			appendLines(errStyle.Render(d.validationErr))
		}
		appendBlank(1)
		baseLine := d.renderedLineCount(lines)
		line := d.renderInputButtonsLine(baseLine)
		lines = append(lines, line)
	case DialogConfirm:
		appendLines(d.message)
		appendBlank(1)
		baseLine := d.renderedLineCount(lines)
		lines = append(lines, d.renderOptionsLines(baseLine)...)
	case DialogSelect:
		if d.message != "" {
			appendLines(d.message)
			appendBlank(1)
		}
		baseLine := d.renderedLineCount(lines)
		lines = append(lines, d.renderOptionsLines(baseLine)...)
	}

	if d.showKeymapHints {
		helpStyle := lipgloss.NewStyle().
			Foreground(ColorMuted()).
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
	return []string{d.renderHorizontalOptionsLine(baseLine)}
}

func (d *Dialog) renderHorizontalOptionsLine(baseLine int) string {
	selectedStyle := lipgloss.NewStyle().
		Foreground(ColorForeground()).
		Background(ColorPrimary()).
		Padding(0, 1)

	normalStyle := lipgloss.NewStyle().
		Foreground(ColorMuted()).
		Padding(0, 1)

	const gap = 2 // gap between buttons
	var b strings.Builder
	x := 0
	for i, opt := range d.options {
		rendered := normalStyle.Render(opt)
		if i == d.cursor {
			rendered = selectedStyle.Render(opt)
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
	selectedStyle := lipgloss.NewStyle().
		Foreground(ColorForeground()).
		Background(ColorPrimary()).
		Padding(0, 1)

	normalStyle := lipgloss.NewStyle().
		Foreground(ColorMuted()).
		Padding(0, 1)

	ok := selectedStyle.Render("OK")
	cancel := normalStyle.Render("Cancel")

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
		return "enter: confirm • esc: cancel • click OK/Cancel"
	case DialogConfirm:
		return "h/l or tab: choose • enter: confirm • esc: cancel"
	case DialogSelect:
		if d.filterEnabled {
			return "type to filter • ↑/↓ or tab: move • enter: select • esc: cancel"
		}
		return "↑/↓ or tab: move • enter: select • esc: cancel"
	default:
		return "enter: confirm • esc: cancel"
	}
}
