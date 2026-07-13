package common

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// EnvDialogResult is sent when the workspace environment-variable dialog
// closes. Canceled is true when the user dismissed via Esc, in which case the
// caller must discard every edit (no mutation, no persist) -- the same
// cancel contract SettingsResult uses.
type EnvDialogResult struct {
	Canceled bool
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
//
// First-cut scope (plan 058's Design decision): only EDITING an existing
// pair's value and REMOVING a pair are supported. Adding a brand-new key
// needs a second input target inside a row (a name field, with its own
// validation) -- a materially different widget than every other multi-row
// editor in this package -- so "add" is deferred to a follow-up, the same
// escape hatch plan 031 used for "add a new assistant".
type EnvDialog struct {
	visible bool
	width   int

	// keys is the display order. It is built once at construction (sorted,
	// for a deterministic and testable row order) and only ever shrinks (on
	// remove); it is never re-sorted, so removing a row does not reshuffle
	// the rows around it.
	keys   []string
	values map[string]string
	cursor int
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
	return &EnvDialog{keys: keys, values: values}
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

	switch {
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("esc"))):
		d.visible = false
		return d, func() tea.Msg { return EnvDialogResult{Canceled: true} }

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("enter"))):
		d.visible = false
		return d, func() tea.Msg { return EnvDialogResult{} }

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("down"))):
		d.moveCursor(1)
		return d, nil

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("up"))):
		d.moveCursor(-1)
		return d, nil

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("ctrl+d"))):
		d.deleteFocusedPair()
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

	lines := []string{title.Render("Workspace Environment"), ""}

	if len(d.keys) == 0 {
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

	lines = append(lines, "", muted.Render("up/down move  ctrl+d remove  enter save  esc cancel"))
	return lines
}
