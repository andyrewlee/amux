package common

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// NewAgentPicker creates a new agent selection dialog with fuzzy filtering
func NewAgentPicker(options []string) *Dialog {
	optionNames := normalizeAssistantOptions(options)
	if len(optionNames) == 0 {
		optionNames = []string{"claude"}
	}
	allIndices := make([]int, len(optionNames))
	for i := range optionNames {
		allIndices[i] = i
	}

	// Create filter input
	fi := textinput.New()
	fi.Placeholder = "Type to filter..."
	fi.Focus()
	fi.CharLimit = 20
	fi.SetWidth(30)
	fi.SetVirtualCursor(false)

	return &Dialog{
		id:              "agent-picker",
		dtype:           DialogSelect,
		title:           "New Agent",
		message:         "Select agent type:",
		options:         optionNames,
		cursor:          0,
		filterEnabled:   true,
		filterInput:     fi,
		filteredIndices: allIndices,
	}
}

func (d *Dialog) renderAgentPickerOptions(baseLine int) []string {
	lines := []string{}
	lineIndex := baseLine

	if d.filterEnabled {
		inputLines := strings.Split(d.filterInput.View(), "\n")
		lines = append(lines, inputLines...)
		lineIndex += len(inputLines)
		lines = append(lines, "", "")
		lineIndex += 2
	}

	if d.filterEnabled && len(d.filteredIndices) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(ColorMuted).Render("No matches"))
		return lines
	}

	for cursorIdx, originalIdx := range d.filteredIndices {
		opt := d.options[originalIdx]
		cursor := Icons.CursorEmpty + " "
		if cursorIdx == d.cursor {
			cursor = Icons.Cursor + " "
		}

		indicator := lipgloss.NewStyle().Foreground(AgentColor(opt)).Render(Icons.Running)
		nameStyle := lipgloss.NewStyle().Foreground(ColorForeground)
		if cursorIdx == d.cursor {
			nameStyle = nameStyle.Bold(true)
		}
		name := nameStyle.Render("[" + opt + "]")
		line := cursor + indicator + " " + name

		// Use full dialog content width for easier clicking
		width := d.dialogContentWidth()
		d.addOptionHit(cursorIdx, originalIdx, lineIndex, 0, width)
		lines = append(lines, line)
		lineIndex++
	}
	return lines
}

func normalizeAssistantOptions(options []string) []string {
	seen := make(map[string]struct{}, len(options))
	out := make([]string, 0, len(options))
	for _, option := range options {
		name := strings.TrimSpace(option)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
