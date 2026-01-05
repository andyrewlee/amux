package keymapeditor

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/keymap"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

const (
	editorMaxWidth      = 80
	editorMaxHeight     = 24
	editorBorderWidth   = 1
	editorPaddingX      = 2
	editorPaddingY      = 1
	doubleClickDuration = 400 * time.Millisecond
)

type entryKind int

const (
	entryHeader entryKind = iota
	entryAction
)

type entry struct {
	kind   entryKind
	label  string
	action keymap.Action
	group  string
}

type buttonID int

const (
	buttonDone buttonID = iota
	buttonReset
	buttonResetAll
)

type button struct {
	id buttonID
	x0 int
	x1 int
	y  int
}

// Editor renders the keymap editor overlay.
type Editor struct {
	visible bool

	width   int
	height  int
	offsetX int
	offsetY int

	styles common.Styles
	keymap keymap.KeyMap

	overrides map[string][]string
	entries   []entry
	cursor    int
	scroll    int

	editing bool
	input   textinput.Model
	errMsg  string

	lastClickIndex int
	lastClickAt    time.Time
	buttons        []button
}

// New creates a new keymap editor.
func New(km keymap.KeyMap, cfg config.KeyMapConfig) *Editor {
	editor := &Editor{
		styles:    common.DefaultStyles(),
		overrides: cloneBindings(cfg.Bindings),
	}
	editor.SetKeyMap(km, cfg)
	editor.initInput()
	return editor
}

func (e *Editor) initInput() {
	e.input = textinput.New()
	e.input.Prompt = ""
	e.input.Placeholder = "ctrl+space, ctrl+@"
}

// SetKeyMap updates the editor's keymap and overrides.
func (e *Editor) SetKeyMap(km keymap.KeyMap, cfg config.KeyMapConfig) {
	e.keymap = km
	e.overrides = cloneBindings(cfg.Bindings)
	e.entries = buildEntries()
	if !isSelectableEntry(e.cursor, e.entries) {
		e.cursor = firstSelectableEntry(e.entries)
	}
}

// Show displays the editor.
func (e *Editor) Show(km keymap.KeyMap, cfg config.KeyMapConfig) {
	e.visible = true
	e.SetKeyMap(km, cfg)
	e.scroll = 0
	e.cursor = firstSelectableEntry(e.entries)
	e.editing = false
	e.errMsg = ""
}

// Hide hides the editor.
func (e *Editor) Hide() {
	e.visible = false
	e.editing = false
	e.errMsg = ""
}

// Visible returns whether the editor is visible.
func (e *Editor) Visible() bool {
	return e.visible
}

// SetSize sets the available screen size.
func (e *Editor) SetSize(width, height int) {
	e.width = width
	e.height = height
}

// SetOffset sets the overlay origin for mouse hit testing.
func (e *Editor) SetOffset(x, y int) {
	e.offsetX = x
	e.offsetY = y
}

// Update handles messages while the editor is visible.
func (e *Editor) Update(msg tea.Msg) (*Editor, tea.Cmd) {
	if !e.visible {
		return e, nil
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		return e.handleMouse(msg)
	case tea.KeyMsg:
		return e.handleKey(msg)
	}

	return e, nil
}

func (e *Editor) handleKey(msg tea.KeyMsg) (*Editor, tea.Cmd) {
	if e.editing {
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			e.editing = false
			e.errMsg = ""
			return e, nil
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			return e, e.applyInput()
		}

		var cmd tea.Cmd
		e.input, cmd = e.input.Update(msg)
		return e, cmd
	}

	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		e.Hide()
		return e, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
		e.moveCursor(1)
	case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
		e.moveCursor(-1)
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		e.startEditing()
	case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
		return e, e.resetCurrent()
	case key.Matches(msg, key.NewBinding(key.WithKeys("R"))):
		return e, e.resetAll()
	}

	return e, nil
}

func (e *Editor) handleMouse(msg tea.MouseMsg) (*Editor, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return e, nil
	}

	localX := msg.X - e.offsetX
	localY := msg.Y - e.offsetY

	if localX < 0 || localY < 0 {
		return e, nil
	}

	contentX := localX - editorBorderWidth - editorPaddingX
	contentY := localY - editorBorderWidth - editorPaddingY
	if contentX < 0 || contentY < 0 {
		return e, nil
	}

	_, _, listHeight, listStart, _ := e.layout()

	if msg.Button == tea.MouseButtonLeft {
		for _, btn := range e.buttons {
			if contentY == btn.y && contentX >= btn.x0 && contentX < btn.x1 {
				switch btn.id {
				case buttonDone:
					e.Hide()
					return e, nil
				case buttonReset:
					return e, e.resetCurrent()
				case buttonResetAll:
					return e, e.resetAll()
				}
			}
		}
	}

	if msg.Button != tea.MouseButtonLeft || contentY < listStart || contentY >= listStart+listHeight {
		return e, nil
	}

	index := e.scroll + (contentY - listStart)
	if index < 0 || index >= len(e.entries) {
		return e, nil
	}
	if e.entries[index].kind != entryAction {
		return e, nil
	}

	e.cursor = index
	if e.lastClickIndex == index && time.Since(e.lastClickAt) <= doubleClickDuration {
		e.lastClickAt = time.Time{}
		e.startEditing()
		return e, nil
	}
	e.lastClickIndex = index
	e.lastClickAt = time.Now()
	return e, nil
}

func (e *Editor) startEditing() {
	if !isSelectableEntry(e.cursor, e.entries) {
		return
	}
	entry := e.entries[e.cursor]
	current := bindingHint(keymap.BindingForAction(e.keymap, entry.action))
	e.editing = true
	e.errMsg = ""
	e.input.SetValue(current)
	e.input.CursorEnd()
	e.input.Focus()
}

func (e *Editor) applyInput() tea.Cmd {
	entry := e.entries[e.cursor]
	keys := parseKeys(e.input.Value())
	if len(keys) == 0 {
		e.errMsg = "Enter at least one key (use commas to separate)"
		return nil
	}

	actionKey := string(entry.action)
	e.overrides[actionKey] = keys
	e.rebuildKeymap()
	e.editing = false
	e.errMsg = ""
	return e.emitUpdate()
}

func (e *Editor) resetCurrent() tea.Cmd {
	if !isSelectableEntry(e.cursor, e.entries) {
		return nil
	}
	actionKey := string(e.entries[e.cursor].action)
	delete(e.overrides, actionKey)
	e.rebuildKeymap()
	return e.emitUpdate()
}

func (e *Editor) resetAll() tea.Cmd {
	e.overrides = map[string][]string{}
	e.rebuildKeymap()
	e.editing = false
	e.errMsg = ""
	return e.emitUpdate()
}

func (e *Editor) rebuildKeymap() {
	e.keymap = keymap.New(config.KeyMapConfig{Bindings: cloneBindings(e.overrides)})
}

func (e *Editor) emitUpdate() tea.Cmd {
	update := cloneBindings(e.overrides)
	return func() tea.Msg {
		return messages.KeymapUpdated{Bindings: update}
	}
}

func (e *Editor) moveCursor(delta int) {
	if len(e.entries) == 0 {
		return
	}
	next := e.cursor + delta
	for next >= 0 && next < len(e.entries) {
		if e.entries[next].kind == entryAction {
			e.cursor = next
			e.ensureVisible()
			return
		}
		next += delta
	}
}

func (e *Editor) ensureVisible() {
	_, _, listHeight, listStart, _ := e.layout()
	_ = listStart
	if e.cursor < e.scroll {
		e.scroll = e.cursor
	}
	if e.cursor >= e.scroll+listHeight {
		e.scroll = e.cursor - listHeight + 1
	}
	if e.scroll < 0 {
		e.scroll = 0
	}
}

// View renders the editor box (not placed).
func (e *Editor) View() string {
	if !e.visible {
		return ""
	}

	boxWidth, boxHeight, listHeight, _, footerY := e.layout()
	contentWidth := boxWidth - (editorBorderWidth*2 + editorPaddingX*2)

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(common.ColorPrimary).
		Render("Keymap Editor")

	subtitle := lipgloss.NewStyle().
		Foreground(common.ColorMuted).
		Render("Enter to edit • r reset • R reset all • Esc to close")

	tip := lipgloss.NewStyle().
		Foreground(common.ColorMuted).
		Render("Tip: change Leader if Ctrl+Space doesn't work")

	lines := []string{title, subtitle, ansi.Truncate(tip, contentWidth, "")}

	for i := 0; i < listHeight; i++ {
		idx := e.scroll + i
		if idx >= len(e.entries) {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, e.renderEntry(idx, contentWidth))
	}

	if e.editing {
		entry := e.entries[e.cursor]
		label := fmt.Sprintf("Keys for %s:", entry.label)
		lines = append(lines, "")
		lines = append(lines, ansi.Truncate(label, contentWidth, ""))
		e.input.Width = contentWidth
		lines = append(lines, ansi.Truncate(e.input.View(), contentWidth, ""))
		if e.errMsg != "" {
			errLine := lipgloss.NewStyle().Foreground(common.ColorError).Render(e.errMsg)
			lines = append(lines, ansi.Truncate(errLine, contentWidth, ""))
		}
	} else if e.errMsg != "" {
		lines = append(lines, "")
		errLine := lipgloss.NewStyle().Foreground(common.ColorError).Render(e.errMsg)
		lines = append(lines, ansi.Truncate(errLine, contentWidth, ""))
	}

	lines = append(lines, "")
	lines = append(lines, e.renderFooter(contentWidth, footerY))

	content := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(common.ColorBorderFocused).
		Padding(editorPaddingY, editorPaddingX).
		Width(boxWidth).
		Height(boxHeight).
		Render(strings.Join(lines, "\n"))

	return content
}

func (e *Editor) renderEntry(index int, contentWidth int) string {
	entry := e.entries[index]
	switch entry.kind {
	case entryHeader:
		header := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorMuted).
			Render(entry.label)
		return ansi.Truncate(header, contentWidth, "")
	case entryAction:
		keyWidth := 22
		if contentWidth < keyWidth+10 {
			keyWidth = contentWidth / 2
		}
		binding := keymap.BindingForAction(e.keymap, entry.action)
		keys := bindingHint(binding)
		if keys == "" {
			keys = "-"
		}
		keyStyle := lipgloss.NewStyle().Foreground(common.ColorPrimary)
		if !e.hasOverride(entry.action) {
			keyStyle = lipgloss.NewStyle().Foreground(common.ColorMuted)
		}
		keyCell := keyStyle.Render(padRight(keys, keyWidth))
		line := keyCell + " " + entry.label
		line = ansi.Truncate(line, contentWidth, "")
		if index == e.cursor {
			return e.styles.SelectedRow.Render(line)
		}
		return line
	default:
		return ""
	}
}

func (e *Editor) renderFooter(contentWidth int, footerY int) string {
	e.buttons = e.buttons[:0]
	labels := []struct {
		id    buttonID
		label string
	}{
		{buttonDone, "[ Done ]"},
		{buttonReset, "[ Reset ]"},
		{buttonResetAll, "[ Reset All ]"},
	}

	x := 0
	var parts []string
	for i, btn := range labels {
		if i > 0 {
			parts = append(parts, "  ")
			x += 2
		}
		width := lipgloss.Width(btn.label)
		e.buttons = append(e.buttons, button{id: btn.id, x0: x, x1: x + width, y: footerY})
		x += width
		parts = append(parts, e.styles.HelpKey.Render(btn.label))
	}

	line := strings.Join(parts, "")
	return ansi.Truncate(line, contentWidth, "")
}

func (e *Editor) layout() (boxWidth int, boxHeight int, listHeight int, listStart int, footerY int) {
	maxWidth := e.width - 8
	if maxWidth < 40 {
		maxWidth = e.width - 4
	}
	if maxWidth < 30 {
		maxWidth = 30
	}
	boxWidth = editorMaxWidth
	if maxWidth < boxWidth {
		boxWidth = maxWidth
	}

	maxHeight := e.height - 6
	if maxHeight < 12 {
		maxHeight = 12
	}
	boxHeight = editorMaxHeight
	if maxHeight < boxHeight {
		boxHeight = maxHeight
	}

	headerLines := 3
	inputLines := 0
	if e.editing {
		inputLines = 3
		if e.errMsg != "" {
			inputLines++
		}
	} else if e.errMsg != "" {
		inputLines = 2
	}
	footerLines := 2

	listHeight = boxHeight - headerLines - inputLines - footerLines
	if listHeight < 3 {
		listHeight = 3
	}

	listStart = headerLines
	footerY = listStart + listHeight + inputLines + 1
	return boxWidth, boxHeight, listHeight, listStart, footerY
}

func (e *Editor) hasOverride(action keymap.Action) bool {
	_, ok := e.overrides[string(action)]
	return ok
}

func buildEntries() []entry {
	var entries []entry
	var currentGroup string
	for _, info := range keymap.ActionInfos() {
		if info.Group != currentGroup {
			currentGroup = info.Group
			entries = append(entries, entry{kind: entryHeader, label: currentGroup})
		}
		entries = append(entries, entry{
			kind:   entryAction,
			label:  info.Desc,
			action: info.Action,
			group:  info.Group,
		})
	}
	return entries
}

func isSelectableEntry(index int, entries []entry) bool {
	if index < 0 || index >= len(entries) {
		return false
	}
	return entries[index].kind == entryAction
}

func firstSelectableEntry(entries []entry) int {
	for i, entry := range entries {
		if entry.kind == entryAction {
			return i
		}
	}
	return 0
}

func bindingHint(binding key.Binding) string {
	if binding.Help().Key != "" {
		return binding.Help().Key
	}
	return strings.Join(binding.Keys(), "/")
}

func padRight(value string, width int) string {
	if width <= 0 {
		return value
	}
	gap := width - lipgloss.Width(value)
	if gap <= 0 {
		return value
	}
	return value + strings.Repeat(" ", gap)
}

func parseKeys(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ',', '/', ';', '\n', '\t':
			return true
		default:
			return false
		}
	})
	keys := make([]string, 0, len(parts))
	for _, part := range parts {
		key := strings.TrimSpace(part)
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func cloneBindings(input map[string][]string) map[string][]string {
	if len(input) == 0 {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(input))
	for key, values := range input {
		clone := make([]string, len(values))
		copy(clone, values)
		out[key] = clone
	}
	return out
}
