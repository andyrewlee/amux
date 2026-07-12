package common

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
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

// InputTransformFunc transforms input text before it's added to the input field
type InputTransformFunc func(string) string

// InputValidateFunc validates input and returns an error message (empty = valid)
type InputValidateFunc func(string) string

// Dialog is a modal dialog component
type Dialog struct {
	// Configuration
	id      string
	dtype   DialogType
	title   string
	message string
	// warning is optional informational text rendered below message on confirm
	// dialogs (e.g. the trust dialog's in-repo script indirection notice). Its
	// absence never implies "safe"; callers only set it to warn, never to
	// reassure.
	warning string
	options []string

	// State
	visible       bool
	input         textinput.Model
	cursor        int
	defaultCursor int
	confirmed     bool

	// Input transformation and validation
	inputTransform InputTransformFunc
	inputValidate  InputValidateFunc
	validationErr  string

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
		id:            id,
		dtype:         DialogConfirm,
		title:         title,
		message:       message,
		options:       []string{"Yes", "No"},
		cursor:        1,
		defaultCursor: 1,
	}
}

// SetDefaultOption sets the option selected whenever the dialog is shown.
func (d *Dialog) SetDefaultOption(index int) {
	if d == nil || index < 0 || index >= len(d.options) {
		return
	}
	d.defaultCursor = index
	d.cursor = index
}

// SetWarning sets optional informational warning text rendered below the
// message on a confirm dialog. Passing "" clears it. This is purely advisory
// (e.g. surfacing in-repo script indirection at trust time); an empty warning
// must never be read or presented as a safety guarantee.
func (d *Dialog) SetWarning(text string) {
	if d == nil {
		return
	}
	d.warning = text
}

// fuzzyMatch returns true if pattern fuzzy-matches target (case-insensitive)
func fuzzyMatch(pattern, target string) bool {
	if pattern == "" {
		return true
	}
	// Match by rune, not byte, so multibyte/CJK names filter correctly.
	pr := []rune(strings.ToLower(pattern))
	tr := []rune(strings.ToLower(target))
	pi := 0
	for ti := 0; ti < len(tr) && pi < len(pr); ti++ {
		if tr[ti] == pr[pi] {
			pi++
		}
	}
	return pi == len(pr)
}

// SetInputTransform sets a transform function that will be applied to input text
func (d *Dialog) SetInputTransform(fn InputTransformFunc) *Dialog {
	d.inputTransform = fn
	return d
}

// SetInputValidate sets a validation function that runs on each keystroke
func (d *Dialog) SetInputValidate(fn InputValidateFunc) *Dialog {
	d.inputValidate = fn
	return d
}

// transformInputMsg applies the input transform to key press and paste messages
func (d *Dialog) transformInputMsg(msg tea.Msg) tea.Msg {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		if m.Text != "" {
			transformed := d.inputTransform(m.Text)
			if transformed != m.Text {
				m.Text = transformed
				return m
			}
		}
	case tea.PasteMsg:
		transformed := d.inputTransform(m.Content)
		if transformed != m.Content {
			m.Content = transformed
			return m
		}
	}
	return msg
}

// Show makes the dialog visible
func (d *Dialog) Show() {
	d.visible = true
	d.confirmed = false
	d.validationErr = ""
	d.cursor = d.defaultCursor
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
