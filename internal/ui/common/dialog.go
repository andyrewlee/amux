package common

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
)

// DialogType identifies the type of dialog
type DialogType int

const (
	DialogNone DialogType = iota
	DialogInput
	DialogConfirm
	DialogSelect
	DialogMultiSelect
)

// DialogResult is sent when a dialog is completed
type DialogResult struct {
	ID        string
	Confirmed bool
	Value     string
	Index     int
	Values    []string
	Indices   []int
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
	selected  map[int]bool

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

	// Input validation
	inputValidate func(string) string
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

// NewMultiSelectDialog creates a new multi-selection dialog.
func NewMultiSelectDialog(id, title, message string, options []string) *Dialog {
	return &Dialog{
		id:       id,
		dtype:    DialogMultiSelect,
		title:    title,
		message:  message,
		options:  options,
		cursor:   0,
		selected: make(map[int]bool),
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

// SetInputValidate sets a validation function for input dialogs.
// The function receives the current input value and returns an error message
// (empty string means valid).
func (d *Dialog) SetInputValidate(validate func(string) string) {
	d.inputValidate = validate
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
	if d.dtype == DialogMultiSelect {
		if d.selected == nil {
			d.selected = make(map[int]bool)
		}
		for k := range d.selected {
			delete(d.selected, k)
		}
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
			case DialogMultiSelect:
				indices, values := d.selectedValues()
				if len(indices) == 0 {
					if idx, ok := d.currentIndex(); ok {
						indices = []int{idx}
						values = []string{d.options[idx]}
					}
				}
				return d, func() tea.Msg {
					return DialogResult{
						ID:        d.id,
						Confirmed: true,
						Indices:   indices,
						Values:    values,
					}
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys(" "))) && d.dtype == DialogMultiSelect:
			if idx, ok := d.currentIndex(); ok {
				if d.selected == nil {
					d.selected = make(map[int]bool)
				}
				if d.selected[idx] {
					delete(d.selected, idx)
				} else {
					d.selected[idx] = true
				}
			}
			return d, nil

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
	if (d.dtype == DialogSelect || d.dtype == DialogMultiSelect) && d.filterEnabled {
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

func (d *Dialog) currentIndex() (int, bool) {
	if d.filterEnabled && len(d.filteredIndices) > 0 {
		if d.cursor >= 0 && d.cursor < len(d.filteredIndices) {
			return d.filteredIndices[d.cursor], true
		}
		return -1, false
	}
	if d.cursor >= 0 && d.cursor < len(d.options) {
		return d.cursor, true
	}
	return -1, false
}

func (d *Dialog) selectedValues() ([]int, []string) {
	if len(d.selected) == 0 {
		return nil, nil
	}
	indices := make([]int, 0, len(d.selected))
	for idx := range d.selected {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	values := make([]string, 0, len(indices))
	for _, idx := range indices {
		if idx >= 0 && idx < len(d.options) {
			values = append(values, d.options[idx])
		}
	}
	return indices, values
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
