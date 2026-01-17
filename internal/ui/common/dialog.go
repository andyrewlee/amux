package common

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	width      int
	height     int
	optionHits []dialogOptionHit
	// Display settings
	showKeymapHints bool
}

type dialogOptionHit struct {
	cursorIndex int
	optionIndex int
	region      HitRegion
}

// NewInputDialog creates a new input dialog
func NewInputDialog(id, title, placeholder string) *Dialog {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	ti.CharLimit = 100
	ti.SetWidth(40)
	ti.SetVirtualCursor(false)

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
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if cmd := d.handleClick(msg); cmd != nil {
				return d, cmd
			}
		}

	case tea.KeyPressMsg:
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

func (d *Dialog) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	if !d.visible {
		return nil
	}

	lines := d.renderLines()
	if len(lines) == 0 {
		return nil
	}

	content := strings.Join(lines, "\n")
	dialogView := d.dialogStyle().Render(content)
	dialogW, dialogH := viewDimensions(dialogView)
	dialogX := (d.width - dialogW) / 2
	dialogY := (d.height - dialogH) / 2
	if dialogX < 0 {
		dialogX = 0
	}
	if dialogY < 0 {
		dialogY = 0
	}
	if msg.X < dialogX || msg.X >= dialogX+dialogW || msg.Y < dialogY || msg.Y >= dialogY+dialogH {
		return nil
	}

	_, _, contentOffsetX, contentOffsetY := d.dialogFrame()
	localX := msg.X - dialogX - contentOffsetX
	localY := msg.Y - dialogY - contentOffsetY
	if localX < 0 || localY < 0 {
		return nil
	}

	for _, hit := range d.optionHits {
		if hit.region.Contains(localX, localY) {
			d.cursor = hit.cursorIndex
			d.visible = false

			switch d.dtype {
			case DialogInput:
				value := d.input.Value()
				return func() tea.Msg {
					return DialogResult{
						ID:        d.id,
						Confirmed: hit.optionIndex == 0,
						Value:     value,
					}
				}
			case DialogConfirm:
				return func() tea.Msg {
					return DialogResult{ID: d.id, Confirmed: hit.optionIndex == 0}
				}
			case DialogSelect:
				value := d.options[hit.optionIndex]
				return func() tea.Msg {
					return DialogResult{
						ID:        d.id,
						Confirmed: true,
						Index:     hit.optionIndex,
						Value:     value,
					}
				}
			}
		}
	}

	return nil
}

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
	appendBlank(1)

	switch d.dtype {
	case DialogInput:
		appendLines(d.input.View())
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

	return []string{d.renderHorizontalOptionsLine(baseLine)}
}

func (d *Dialog) renderHorizontalOptionsLine(baseLine int) string {
	selectedStyle := lipgloss.NewStyle().
		Foreground(ColorForeground).
		Background(ColorPrimary).
		Padding(0, 1)

	normalStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
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
		Foreground(ColorForeground).
		Background(ColorPrimary).
		Padding(0, 1)

	normalStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
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

// SetSize sets the dialog size
func (d *Dialog) SetSize(width, height int) {
	d.width = width
	d.height = height
	if d.dtype == DialogInput {
		d.input.SetWidth(min(40, width-10))
	}
	if d.dtype == DialogSelect && d.filterEnabled {
		d.filterInput.SetWidth(min(30, width-10))
	}
}
