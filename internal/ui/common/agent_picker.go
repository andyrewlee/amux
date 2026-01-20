package common

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// AgentOption represents an agent option
type AgentOption struct {
	ID   string
	Name string
}

// DefaultAgentOptions returns the default agent options
func DefaultAgentOptions() []AgentOption {
	return []AgentOption{
		{ID: "claude", Name: "claude"},
		{ID: "codex", Name: "codex"},
		{ID: "gemini", Name: "gemini"},
		{ID: "amp", Name: "amp"},
		{ID: "opencode", Name: "opencode"},
		{ID: "droid", Name: "droid"},
		{ID: "cursor", Name: "cursor"},
	}
}

// NewAgentPicker creates a new agent selection dialog with fuzzy filtering
func NewAgentPicker() *Dialog {
	options := DefaultAgentOptions()
	optionNames := make([]string, len(options))
	allIndices := make([]int, len(options))
	for i, opt := range options {
		optionNames[i] = opt.ID
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

	agentOptions := DefaultAgentOptions()
	for cursorIdx, originalIdx := range d.filteredIndices {
		opt := agentOptions[originalIdx]
		cursor := Icons.CursorEmpty + " "
		if cursorIdx == d.cursor {
			cursor = Icons.Cursor + " "
		}

		var colorStyle lipgloss.Style
		switch opt.ID {
		case "claude":
			colorStyle = lipgloss.NewStyle().Foreground(ColorClaude)
		case "codex":
			colorStyle = lipgloss.NewStyle().Foreground(ColorCodex)
		case "gemini":
			colorStyle = lipgloss.NewStyle().Foreground(ColorGemini)
		case "amp":
			colorStyle = lipgloss.NewStyle().Foreground(ColorAmp)
		case "opencode":
			colorStyle = lipgloss.NewStyle().Foreground(ColorOpencode)
		case "droid":
			colorStyle = lipgloss.NewStyle().Foreground(ColorDroid)
		case "cursor":
			colorStyle = lipgloss.NewStyle().Foreground(ColorCursor)
		default:
			colorStyle = lipgloss.NewStyle().Foreground(ColorForeground)
		}

		indicator := colorStyle.Render(Icons.Running)
		nameStyle := lipgloss.NewStyle().Foreground(ColorForeground)
		if cursorIdx == d.cursor {
			nameStyle = nameStyle.Bold(true)
		}
		name := nameStyle.Render("[" + opt.Name + "]")
		line := cursor + indicator + " " + name

		// Use full dialog content width for easier clicking
		width := d.dialogContentWidth()
		d.addOptionHit(cursorIdx, originalIdx, lineIndex, 0, width)
		lines = append(lines, line)
		lineIndex++
	}
	return lines
}
