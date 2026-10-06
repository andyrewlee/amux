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
	// Value2 carries the second input's text on two-field input dialogs
	// (empty for every other dialog kind — the field is opt-in via
	// SetSecondInput).
	Value2 string
	Index  int
	// RequestTask is emitted by the agent picker when the user chooses the
	// highlighted assistant via ctrl+t instead of enter: the pick still
	// carries Value/Index, and Confirmed stays true, but the result asks the
	// consumer to collect an optional first task before launching. Only the
	// picker sets it; every other dialog leaves it false.
	RequestTask bool
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
	input2        textinput.Model // opt-in second field; see SetSecondInput
	secondInput   bool            // input2 is active (set only by SetSecondInput)
	focused2      bool            // which input owns keystrokes on two-field dialogs
	inputLabel    string
	input2Label   string
	cursor        int
	defaultCursor int
	confirmed     bool

	// Input transformation and validation
	inputTransform InputTransformFunc
	inputValidate  InputValidateFunc
	input2Validate InputValidateFunc
	validationErr  string
	validationErr2 string

	// Fuzzy filter state
	filterEnabled   bool
	filterInput     textinput.Model
	filteredIndices []int // indices into options

	// taskEntry gates the agent picker's ctrl+t → first-task handoff: true
	// only when the consumer (the launch path) can honor a collected task.
	// Off in contexts where the pick feeds a flow with no task carrier —
	// the create-workspace handoff ends at CreateWorkspace, which has no
	// Task field — so the key stays inert there.
	taskEntry bool

	// sessionRowLive flags each option row's liveness for the run-session
	// picker's vertical renderer; nil for every other select dialog.
	sessionRowLive []bool

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

// NewSelectDialog creates a single-choice list dialog with caller-owned
// options — the generic counterpart to NewRunSessionPicker for choices that
// are not tmux sessions (e.g. a post-action follow-up). The result carries
// the chosen row's Index; esc cancels.
func NewSelectDialog(id, title, message string, options []string) *Dialog {
	return &Dialog{
		id:      id,
		dtype:   DialogSelect,
		title:   title,
		message: message,
		options: options,
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

// SetSecondInput gives an input dialog a second text field. Keystrokes route
// to the focused field; tab/shift-tab (and up/down) switch focus, enter
// confirms from either field after re-validating both — an optional field
// stays one Enter keystroke for users who skip it. The field-2 validator is
// optional; pass nil to skip live validation.
func (d *Dialog) SetSecondInput(label, placeholder string, validate InputValidateFunc) *Dialog {
	if d == nil || d.dtype != DialogInput {
		return d
	}
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = d.input.CharLimit
	// Same width contract as SetSize — negative values pass through.
	ti.SetWidth(min(40, d.width-10))
	ti.SetVirtualCursor(false)
	d.input2 = ti
	d.secondInput = true
	d.input2Label = label
	d.input2Validate = validate
	return d
}

// hasSecondInput reports whether the dialog is a two-field input form.
func (d *Dialog) hasSecondInput() bool {
	return d.dtype == DialogInput && d.secondInput
}

// SetTaskEntry arms the picker's ctrl+t key: with it enabled, ctrl+t emits
// the selection as a DialogResult with RequestTask set instead of launching
// immediately. Only meaningful on the agent picker; other dialogs ignore it.
func (d *Dialog) SetTaskEntry(enabled bool) *Dialog {
	if d == nil {
		return d
	}
	d.taskEntry = enabled
	return d
}

// selectedOption resolves the highlighted option on a select dialog, mapping
// the cursor through the fuzzy filter when one is active. ok is false when
// nothing valid is highlighted (e.g. a filter with no matches).
func (d *Dialog) selectedOption() (idx int, value string, ok bool) {
	if d.filterEnabled {
		if len(d.filteredIndices) == 0 {
			return 0, "", false
		}
		idx = d.filteredIndices[d.cursor]
		return idx, d.options[idx], true
	}
	if d.cursor < len(d.options) {
		return d.cursor, d.options[d.cursor], true
	}
	return 0, "", false
}

// SetInputLabel sets a small caption rendered above the first input — used so
// far only by two-field forms, where each field needs a visible name once
// its placeholder is filled.
func (d *Dialog) SetInputLabel(label string) *Dialog {
	if d == nil {
		return d
	}
	d.inputLabel = label
	return d
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

// SetInputValue prefills the input field's current value so a dialog opened for
// editing (e.g. rename) renders the existing value ready to edit. It affects
// input dialogs only. Call it after Show(), which resets the input to empty.
func (d *Dialog) SetInputValue(s string) {
	if d == nil || d.dtype != DialogInput {
		return
	}
	d.input.SetValue(s)
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
	d.validationErr2 = ""
	d.focused2 = false
	d.cursor = d.defaultCursor
	if d.dtype == DialogInput {
		d.input.SetValue("")
		d.input.Focus()
		if d.secondInput {
			d.input2.SetValue("")
			d.input2.Blur()
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

// SetSize sets the dialog size
func (d *Dialog) SetSize(width, height int) {
	d.width = width
	d.height = height
	if d.dtype == DialogInput {
		// Negative widths are passed through unchanged: textinput treats a
		// non-positive SetWidth as "keep default" — clamping to 1 would
		// shrink the field to a one-character window on degenerate sizes.
		w := min(40, width-10)
		d.input.SetWidth(w)
		if d.secondInput {
			d.input2.SetWidth(w)
		}
	}
	if d.dtype == DialogSelect && d.filterEnabled {
		d.filterInput.SetWidth(min(30, width-10))
	}
}
