package common

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"github.com/andyrewlee/amux/internal/logging"
)

// DialogType identifies the type of dialog
type DialogType int

const (
	DialogNone DialogType = iota
	DialogInput
	DialogConfirm
	DialogSelect
)

// DialogResult is sent when a dialog is completed
type DialogResult struct {
	ID        string
	Confirmed bool
	Value     string
	Index     int
}

// Dialog is a modal dialog component
type Dialog struct {
	// Configuration
	id      string
	dtype   DialogType
	title   string
	message string
	options []string

	// State
	visible   bool
	input     textinput.Model
	cursor    int
	confirmed bool

	// Fuzzy filter state
	filterEnabled   bool
	filterInput     textinput.Model
	filteredIndices []int // indices into options

	// Layout
	width  int
	height int

	// Zone manager for mouse handling
	zone *zone.Manager
	// Display settings
	showKeymapHints bool
}

// NewInputDialog creates a new input dialog
func NewInputDialog(id, title, placeholder string) *Dialog {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40

	return &Dialog{
		id:    id,
		dtype: DialogInput,
		title: title,
		input: ti,
	}
}

// NewConfirmDialog creates a new confirmation dialog
func NewConfirmDialog(id, title, message string) *Dialog {
	return &Dialog{
		id:      id,
		dtype:   DialogConfirm,
		title:   title,
		message: message,
		options: []string{"Yes", "No"},
		cursor:  1, // Default to "No"
	}
}

// NewSelectDialog creates a new selection dialog
func NewSelectDialog(id, title, message string, options []string) *Dialog {
	return &Dialog{
		id:      id,
		dtype:   DialogSelect,
		title:   title,
		message: message,
		options: options,
		cursor:  0,
	}
}

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
	fi.Width = 30

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

// fuzzyMatch returns true if pattern fuzzy-matches target (case-insensitive)
func fuzzyMatch(pattern, target string) bool {
	if pattern == "" {
		return true
	}
	pattern = strings.ToLower(pattern)
	target = strings.ToLower(target)
	pi := 0
	for ti := 0; ti < len(target) && pi < len(pattern); ti++ {
		if target[ti] == pattern[pi] {
			pi++
		}
	}
	return pi == len(pattern)
}

// Show makes the dialog visible
func (d *Dialog) Show() {
	d.visible = true
	d.confirmed = false
	d.cursor = 0
	if d.dtype == DialogInput {
		d.input.SetValue("")
		d.input.Focus()
	}
	if d.filterEnabled {
		d.filterInput.SetValue("")
		d.filterInput.Focus()
		d.applyFilter()
	}
}

// applyFilter updates filteredIndices based on current filter input
func (d *Dialog) applyFilter() {
	query := d.filterInput.Value()
	d.filteredIndices = nil
	for i, opt := range d.options {
		if fuzzyMatch(query, opt) {
			d.filteredIndices = append(d.filteredIndices, i)
		}
	}
	// Clamp cursor to filtered range
	if d.cursor >= len(d.filteredIndices) {
		d.cursor = max(0, len(d.filteredIndices)-1)
	}
}

// Hide hides the dialog
func (d *Dialog) Hide() {
	d.visible = false
}

// Visible returns whether the dialog is visible
func (d *Dialog) Visible() bool {
	return d.visible
}

// SetZone sets the shared zone manager for click targets.
func (d *Dialog) SetZone(z *zone.Manager) {
	d.zone = z
}

// SetShowKeymapHints controls whether helper text is rendered.
func (d *Dialog) SetShowKeymapHints(show bool) {
	d.showKeymapHints = show
}

// Update handles messages
func (d *Dialog) Update(msg tea.Msg) (*Dialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if cmd := d.handleClick(msg); cmd != nil {
				return d, cmd
			}
		}

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			d.visible = false
			return d, func() tea.Msg {
				return DialogResult{ID: d.id, Confirmed: false}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			logging.Info("Dialog Enter pressed: id=%s value=%s", d.id, d.input.Value())
			d.visible = false
			switch d.dtype {
			case DialogInput:
				value := d.input.Value()
				id := d.id
				logging.Info("Dialog returning InputResult: id=%s value=%s", id, value)
				return d, func() tea.Msg {
					return DialogResult{
						ID:        id,
						Confirmed: true,
						Value:     value,
					}
				}
			case DialogConfirm:
				return d, func() tea.Msg {
					return DialogResult{
						ID:        d.id,
						Confirmed: d.cursor == 0,
					}
				}
			case DialogSelect:
				// For filtered dialogs, return the original index
				var originalIdx int
				var value string
				if d.filterEnabled && len(d.filteredIndices) > 0 {
					originalIdx = d.filteredIndices[d.cursor]
					value = d.options[originalIdx]
				} else if !d.filterEnabled && d.cursor < len(d.options) {
					originalIdx = d.cursor
					value = d.options[d.cursor]
				} else {
					// No valid selection
					d.visible = false
					return d, func() tea.Msg {
						return DialogResult{ID: d.id, Confirmed: false}
					}
				}
				return d, func() tea.Msg {
					return DialogResult{
						ID:        d.id,
						Confirmed: true,
						Index:     originalIdx,
						Value:     value,
					}
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("tab", "down"))):
			if d.dtype != DialogInput {
				maxLen := len(d.options)
				if d.filterEnabled {
					maxLen = len(d.filteredIndices)
				}
				if maxLen > 0 {
					d.cursor = (d.cursor + 1) % maxLen
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab", "up"))):
			if d.dtype != DialogInput {
				maxLen := len(d.options)
				if d.filterEnabled {
					maxLen = len(d.filteredIndices)
				}
				if maxLen > 0 {
					d.cursor--
					if d.cursor < 0 {
						d.cursor = maxLen - 1
					}
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("h", "left"))):
			if d.dtype == DialogConfirm {
				d.cursor = 0
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("l", "right"))):
			if d.dtype == DialogConfirm {
				d.cursor = 1
			}
		}
	}

	// Update text input if applicable
	if d.dtype == DialogInput {
		var cmd tea.Cmd
		d.input, cmd = d.input.Update(msg)
		return d, cmd
	}

	// Update filter input for filtered select dialogs
	if d.dtype == DialogSelect && d.filterEnabled {
		oldValue := d.filterInput.Value()
		var cmd tea.Cmd
		d.filterInput, cmd = d.filterInput.Update(msg)
		// Reapply filter if input changed
		if d.filterInput.Value() != oldValue {
			d.applyFilter()
		}
		return d, cmd
	}

	return d, nil
}

func (d *Dialog) handleClick(msg tea.MouseMsg) tea.Cmd {
	if d.zone == nil {
		return nil
	}

	switch d.dtype {
	case DialogInput:
		if z := d.zone.Get("dialog-" + d.id + "-opt-0"); z != nil && z.InBounds(msg) {
			value := d.input.Value()
			d.visible = false
			return func() tea.Msg {
				return DialogResult{
					ID:        d.id,
					Confirmed: true,
					Value:     value,
				}
			}
		}
		if z := d.zone.Get("dialog-" + d.id + "-opt-1"); z != nil && z.InBounds(msg) {
			d.visible = false
			return func() tea.Msg {
				return DialogResult{ID: d.id, Confirmed: false}
			}
		}

	case DialogConfirm:
		if z := d.zone.Get("dialog-" + d.id + "-opt-0"); z != nil && z.InBounds(msg) {
			d.cursor = 0
			d.visible = false
			return func() tea.Msg {
				return DialogResult{ID: d.id, Confirmed: true}
			}
		}
		if z := d.zone.Get("dialog-" + d.id + "-opt-1"); z != nil && z.InBounds(msg) {
			d.cursor = 1
			d.visible = false
			return func() tea.Msg {
				return DialogResult{ID: d.id, Confirmed: false}
			}
		}

	case DialogSelect:
		if d.filterEnabled {
			for cursorIdx, originalIdx := range d.filteredIndices {
				id := "dialog-" + d.id + "-opt-" + strconv.Itoa(originalIdx)
				if z := d.zone.Get(id); z != nil && z.InBounds(msg) {
					d.cursor = cursorIdx
					value := d.options[originalIdx]
					d.visible = false
					return func() tea.Msg {
						return DialogResult{
							ID:        d.id,
							Confirmed: true,
							Index:     originalIdx,
							Value:     value,
						}
					}
				}
			}
		} else {
			for i := range d.options {
				id := "dialog-" + d.id + "-opt-" + strconv.Itoa(i)
				if z := d.zone.Get(id); z != nil && z.InBounds(msg) {
					d.cursor = i
					d.visible = false
					value := d.options[i]
					return func() tea.Msg {
						return DialogResult{
							ID:        d.id,
							Confirmed: true,
							Index:     i,
							Value:     value,
						}
					}
				}
			}
		}
	}

	return nil
}

// View renders the dialog
func (d *Dialog) View() string {
	if !d.visible {
		return ""
	}

	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)
	content.WriteString(titleStyle.Render(d.title))
	content.WriteString("\n\n")

	// Content based on type
	switch d.dtype {
	case DialogInput:
		content.WriteString(d.input.View())
		content.WriteString("\n\n")
		content.WriteString(d.renderInputButtons())

	case DialogConfirm:
		content.WriteString(d.message)
		content.WriteString("\n\n")
		content.WriteString(d.renderOptions())

	case DialogSelect:
		if d.message != "" {
			content.WriteString(d.message)
			content.WriteString("\n\n")
		}
		content.WriteString(d.renderOptions())
	}

	if d.showKeymapHints {
		helpStyle := lipgloss.NewStyle().
			Foreground(ColorMuted).
			MarginTop(1)
		content.WriteString("\n")
		content.WriteString(helpStyle.Render(d.helpText()))
	}

	// Dialog box
	width := 50
	if d.width > 0 {
		// Use available width, but cap at reasonable max for readability if needed,
		// or just use a percentage.
		// For paths, we want more space.
		width = min(80, max(50, d.width-10))
	}

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(width)

	return dialogStyle.Render(content.String())
}

// renderOptions renders the selection options
func (d *Dialog) renderOptions() string {
	var b strings.Builder

	// For agent picker, show filter input and filtered list
	if d.id == "agent-picker" {
		// Show filter input
		if d.filterEnabled {
			b.WriteString(d.filterInput.View())
			b.WriteString("\n\n")
		}

		agentOptions := DefaultAgentOptions()

		// If no matches, show message
		if d.filterEnabled && len(d.filteredIndices) == 0 {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("No matches"))
			return b.String()
		}

		// Render only filtered options
		for cursorIdx, originalIdx := range d.filteredIndices {
			opt := agentOptions[originalIdx]
			cursor := Icons.CursorEmpty + " "
			if cursorIdx == d.cursor {
				cursor = Icons.Cursor + " "
			}

			// Get agent color for indicator only
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
			if d.zone != nil {
				line = d.zone.Mark("dialog-"+d.id+"-opt-"+strconv.Itoa(originalIdx), line)
			}
			b.WriteString(line + "\n")
		}
		return b.String()
	}

	// For confirm dialogs, show horizontal options
	selectedStyle := lipgloss.NewStyle().
		Foreground(ColorForeground).
		Background(ColorPrimary).
		Padding(0, 1)

	normalStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Padding(0, 1)

	for i, opt := range d.options {
		rendered := normalStyle.Render(opt)
		if i == d.cursor {
			rendered = selectedStyle.Render(opt)
		}
		if d.zone != nil {
			rendered = d.zone.Mark("dialog-"+d.id+"-opt-"+strconv.Itoa(i), rendered)
		}
		b.WriteString(rendered)
		if i < len(d.options)-1 {
			b.WriteString("  ")
		}
	}

	return b.String()
}

func (d *Dialog) renderInputButtons() string {
	selectedStyle := lipgloss.NewStyle().
		Foreground(ColorForeground).
		Background(ColorPrimary).
		Padding(0, 1)

	normalStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Padding(0, 1)

	// Highlight OK by default for visual affordance
	ok := selectedStyle.Render("OK")
	cancel := normalStyle.Render("Cancel")
	if d.zone != nil {
		ok = d.zone.Mark("dialog-"+d.id+"-opt-0", ok)
		cancel = d.zone.Mark("dialog-"+d.id+"-opt-1", cancel)
	}

	return ok + "  " + cancel
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

// SetSize sets the dialog size
func (d *Dialog) SetSize(width, height int) {
	d.width = width
	d.height = height
	if d.dtype == DialogInput {
		d.input.Width = min(40, width-10)
	}
}
