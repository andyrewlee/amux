package common

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// EnvScope identifies which env editor emitted an EnvDialogResult — the
// workspace-scoped and project-scoped editors share this widget, and the
// scope (set at construction) is what routes the result, not which App
// pointer happens to be non-nil.
type EnvScope int

const (
	// EnvScopeWorkspace is the default: the workspace env editor.
	EnvScopeWorkspace EnvScope = iota
	// EnvScopeProject is the per-project env editor.
	EnvScopeProject
)

// EnvDialogResult is sent when an environment-variable dialog closes.
// Canceled is true when the user dismissed via Esc, in which case the
// caller must discard every edit (no mutation, no persist) -- the same
// cancel contract SettingsResult uses.
type EnvDialogResult struct {
	Canceled bool
	Scope    EnvScope
}

// EnvDialog is a modal dialog that edits a single workspace's
// environment-variable map. It mirrors SettingsDialog's Assistants section
// (handleAssistantFieldKey in settings_assistants.go): rows render as
// "key: value", Up/Down move a row cursor, printable runes edit the focused
// row's value, Backspace deletes the last rune of the focused value, and
// Ctrl+D removes the focused pair outright.
//
// This widget is domain-agnostic on purpose (internal/ui/common imports
// neither internal/data nor internal/process elsewhere): it just edits
// whatever map[string]string it is given. Excluding reserved keys
// (process.IsReservedScriptEnvKey) is the caller's job -- see
// internal/app's handleShowWorkspaceEnvDialog, which filters ws.Env before
// calling NewEnvDialog and filters again defensively before persisting.
// Callers that want domain validation surfaced inside the dialog wire
// SetKeyValidator (e.g. to reject reserved names) -- without one the dialog
// enforces only the generic name rules (non-empty, no whitespace/'=').
//
// Adding a new pair: ctrl+a opens a two-field input (name -> value, Tab
// switches fields, Enter commits, Esc cancels just the add). A duplicate
// name cancels the add and moves the cursor to the existing row instead of
// silently overwriting it.
type EnvDialog struct {
	visible bool
	width   int
	title   string

	// keys is the display order. It is built once at construction (sorted,
	// for a deterministic and testable row order); removes shrink it and adds
	// insert at the sorted position, so the order stays stable and sorted
	// without ever re-sorting.
	keys   []string
	values map[string]string
	cursor int

	// Add-mode state (see the doc comment above). notice is a transient
	// footer line shown after an add/cancel/duplicate outcome; it clears on
	// the next keypress. keyValidator is the optional domain hook from
	// SetKeyValidator ("" return = accept).
	adding       bool
	addField     int // 0 = name, 1 = value
	addName      string
	addValue     string
	addError     string
	notice       string
	keyValidator func(string) string
	scope        EnvScope
}

// NewEnvDialog seeds the dialog from env, which is copied so later edits in
// the dialog cannot alias the caller's map (mirroring SetAssistants).
func NewEnvDialog(env map[string]string) *EnvDialog {
	keys := make([]string, 0, len(env))
	values := make(map[string]string, len(env))
	for k, v := range env {
		keys = append(keys, k)
		values[k] = v
	}
	sort.Strings(keys)
	return &EnvDialog{keys: keys, values: values, title: "Workspace Environment"}
}

// SetTitle overrides the rendered heading — the project-level editor reuses
// this dialog but is not workspace-scoped, so it must not claim to be.
func (d *EnvDialog) SetTitle(title string) {
	if title != "" {
		d.title = title
	}
}

// SetScope marks which editor instance this dialog is; emitted
// EnvDialogResults carry it so dispatch routes on the result itself.
func (d *EnvDialog) SetScope(scope EnvScope) {
	d.scope = scope
}

// SetKeyValidator installs the optional domain check run on a new key name
// when the user commits an add ("" return = accept; anything else renders as
// the in-dialog error). The workspace editor wires this to
// process.IsReservedScriptEnvKey so a reserved name is rejected visibly, not
// just dropped at persist time by the caller-side filter.
func (d *EnvDialog) SetKeyValidator(fn func(string) string) {
	d.keyValidator = fn
}

func (d *EnvDialog) Show()            { d.visible = true }
func (d *EnvDialog) Hide()            { d.visible = false }
func (d *EnvDialog) Visible() bool    { return d.visible }
func (d *EnvDialog) SetSize(w, _ int) { d.width = w }
func (d *EnvDialog) Cursor() *tea.Cursor {
	return nil
}

// Env returns the (possibly edited) map for read-back on close: a copy so the
// caller cannot mutate the dialog's internal state through the returned map.
// Removed pairs are simply absent (deleteFocusedPair drops them from both
// keys and values), so there is no separate "removed" set to reconcile.
func (d *EnvDialog) Env() map[string]string {
	out := make(map[string]string, len(d.values))
	for k, v := range d.values {
		out[k] = v
	}
	return out
}

// Update handles input. Like SettingsDialog, Esc always cancels. While a row
// is focused, only Up/Down/Ctrl+D/Backspace are structural; every other
// printable rune (including j/k and space) is typed into the focused row's
// value -- there is no "leave the field" key distinct from Enter here, since
// (unlike Settings) this dialog has only one section to route around.
func (d *EnvDialog) Update(msg tea.Msg) (*EnvDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	d.notice = ""

	if d.adding {
		return d.updateAddMode(keyMsg)
	}

	switch {
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("esc"))):
		d.visible = false
		return d, func() tea.Msg { return EnvDialogResult{Canceled: true, Scope: d.scope} }

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("enter"))):
		d.visible = false
		return d, func() tea.Msg { return EnvDialogResult{Scope: d.scope} }

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("down"))):
		d.moveCursor(1)
		return d, nil

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("up"))):
		d.moveCursor(-1)
		return d, nil

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("ctrl+d"))):
		d.deleteFocusedPair()
		return d, nil

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("ctrl+a"))):
		d.startAdd()
		return d, nil

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("backspace"))):
		d.deleteFocusedRune()
		return d, nil
	}

	if keyMsg.Text != "" {
		d.appendFocusedText(keyMsg.Text)
	}
	return d, nil
}

// updateAddMode handles input while the two-field add input is open: Tab
// toggles between name and value, Enter advances name->value (when the name
// is valid) and commits from the value field, Esc cancels just the add
// (list edits are untouched). Structural list keys are inert here.
func (d *EnvDialog) updateAddMode(keyMsg tea.KeyPressMsg) (*EnvDialog, tea.Cmd) {
	switch {
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("esc"))):
		d.cancelAdd("add canceled")
		return d, nil

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("tab", "shift+tab"))):
		d.addField = 1 - d.addField
		return d, nil

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("enter"))):
		if d.addField == 0 {
			if err := d.validateAddName(); err != "" {
				d.addError = err
				return d, nil
			}
			d.addError = ""
			d.addField = 1
			return d, nil
		}
		d.commitAdd()
		return d, nil

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("backspace"))):
		if d.addField == 0 {
			d.addName = trimLastRune(d.addName)
		} else {
			d.addValue = trimLastRune(d.addValue)
		}
		return d, nil
	}

	if keyMsg.Text != "" {
		txt := keepRunes(keyMsg.Text, isPrintableFieldRune)
		if d.addField == 0 {
			d.addName += txt
		} else {
			d.addValue += txt
		}
	}
	return d, nil
}

// startAdd opens the two-field add input.
func (d *EnvDialog) startAdd() {
	d.adding = true
	d.addField = 0
	d.addName = ""
	d.addValue = ""
	d.addError = ""
}

// cancelAdd closes the add input without committing, leaving a footer note.
func (d *EnvDialog) cancelAdd(note string) {
	d.adding = false
	d.addError = ""
	if note != "" {
		d.notice = note
	}
}

// validateAddName enforces the generic name rules plus the optional domain
// validator; it returns "" for a usable name or the error text to display.
func (d *EnvDialog) validateAddName() string {
	name := strings.TrimSpace(d.addName)
	if name == "" {
		return "name required"
	}
	if strings.ContainsAny(name, " \t\n\r=") {
		return "name must not contain whitespace or '='"
	}
	for _, r := range name {
		if !isPrintableFieldRune(r) {
			return "name must be printable"
		}
	}
	if d.keyValidator != nil {
		if err := d.keyValidator(name); err != "" {
			return err
		}
	}
	return ""
}

// commitAdd applies the add: a duplicate name cancels the add and focuses
// the existing row (never silently overwrite); a new name is inserted at
// its sorted position so the display order stays sorted.
func (d *EnvDialog) commitAdd() {
	name := strings.TrimSpace(d.addName)
	if err := d.validateAddName(); err != "" {
		d.addError = err
		return
	}
	if _, exists := d.values[name]; exists {
		for i, k := range d.keys {
			if k == name {
				d.cursor = i
				break
			}
		}
		d.cancelAdd(name + " already exists")
		return
	}
	idx := sort.SearchStrings(d.keys, name)
	d.keys = append(d.keys, "")
	copy(d.keys[idx+1:], d.keys[idx:])
	d.keys[idx] = name
	d.values[name] = d.addValue
	d.cursor = idx
	d.cancelAdd("added " + name)
}

// moveCursor moves the row cursor by delta, wrapping within the row list
// (mirroring moveAssistantCursor's theme-cursor-style wraparound).
func (d *EnvDialog) moveCursor(delta int) {
	n := len(d.keys)
	if n == 0 {
		return
	}
	d.cursor = ((d.cursor+delta)%n + n) % n
}

// focusedKey returns the key at cursor, or "" if the row list is empty or the
// cursor is out of range.
func (d *EnvDialog) focusedKey() (string, bool) {
	if d.cursor < 0 || d.cursor >= len(d.keys) {
		return "", false
	}
	return d.keys[d.cursor], true
}

// appendFocusedText appends filtered text to the focused row's value, reusing
// the same printable-rune filter as the tmux fields and assistant commands.
func (d *EnvDialog) appendFocusedText(txt string) {
	k, ok := d.focusedKey()
	if !ok {
		return
	}
	d.values[k] += keepRunes(txt, isPrintableFieldRune)
}

// deleteFocusedRune removes the last rune from the focused row's value.
func (d *EnvDialog) deleteFocusedRune() {
	k, ok := d.focusedKey()
	if !ok {
		return
	}
	d.values[k] = trimLastRune(d.values[k])
}

// deleteFocusedPair removes the focused key/value pair outright (not just its
// value) and clamps the cursor to stay within the now-shorter row list.
func (d *EnvDialog) deleteFocusedPair() {
	k, ok := d.focusedKey()
	if !ok {
		return
	}
	delete(d.values, k)
	d.keys = append(d.keys[:d.cursor], d.keys[d.cursor+1:]...)
	if d.cursor >= len(d.keys) {
		d.cursor = len(d.keys) - 1
	}
	if d.cursor < 0 {
		d.cursor = 0
	}
}

func (d *EnvDialog) View() string {
	if !d.visible {
		return ""
	}
	return d.dialogStyle().Render(strings.Join(d.renderLines(), "\n"))
}

func (d *EnvDialog) dialogContentWidth() int {
	if d.width > 0 {
		return min(50, max(35, d.width-20))
	}
	return 40
}

func (d *EnvDialog) dialogStyle() lipgloss.Style {
	return dialogBorderStyle(d.dialogContentWidth())
}

func (d *EnvDialog) renderLines() []string {
	title := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary())
	muted := lipgloss.NewStyle().Foreground(ColorMuted())

	lines := []string{title.Render(d.title), ""}

	if len(d.keys) == 0 && !d.adding {
		lines = append(lines, muted.Render("No editable environment variables."))
	}
	for i, k := range d.keys {
		style, prefix := muted, "  "
		if i == d.cursor {
			style = lipgloss.NewStyle().Foreground(ColorPrimary()).Bold(true)
			prefix = Icons.Cursor + " "
		}
		lines = append(lines, prefix+style.Render(k+": "+d.values[k]))
	}

	if d.adding {
		lines = append(lines, "", muted.Render("New entry"))
		nameStyle, valueStyle := muted, muted
		namePrefix, valuePrefix := "  ", "  "
		if d.addField == 0 {
			nameStyle = lipgloss.NewStyle().Foreground(ColorPrimary()).Bold(true)
			namePrefix = Icons.Cursor + " "
		} else {
			valueStyle = lipgloss.NewStyle().Foreground(ColorPrimary()).Bold(true)
			valuePrefix = Icons.Cursor + " "
		}
		lines = append(lines,
			namePrefix+nameStyle.Render("name:  "+d.addName),
			valuePrefix+valueStyle.Render("value: "+d.addValue))
		if d.addError != "" {
			lines = append(lines, lipgloss.NewStyle().Foreground(ColorError()).Render("  "+d.addError))
		}
		lines = append(lines, "", muted.Render("tab switch field  enter next/commit  esc cancel"))
		return lines
	}

	if d.notice != "" {
		lines = append(lines, "", muted.Render(d.notice))
	}
	lines = append(lines, "", muted.Render("up/down move  ctrl+a add  ctrl+d remove  enter save  esc cancel"))
	return lines
}
