package keymapeditor

import (
	"fmt"
	"sort"
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

type confirmAction int

const (
	confirmNone confirmAction = iota
	confirmResetAll
	confirmLeaderChange
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
	buttonConfirmYes
	buttonConfirmNo
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

	overrides      map[string][]string
	entries        []entry
	visibleEntries []int
	cursor         int
	scroll         int

	filtering   bool
	filterInput textinput.Model

	confirming     bool
	confirmMessage string
	confirmAction  confirmAction
	pendingKeys    []string

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
	editor.initInput()
	editor.initFilter()
	editor.SetKeyMap(km, cfg)
	return editor
}

func (e *Editor) initInput() {
	e.input = textinput.New()
	e.input.Prompt = ""
	e.input.Placeholder = "ctrl+space, ctrl+@"
}

func (e *Editor) initFilter() {
	e.filterInput = textinput.New()
	e.filterInput.Prompt = ""
	e.filterInput.Placeholder = " / to filter"
	e.filterInput.Blur()
}

// SetKeyMap updates the editor's keymap and overrides.
func (e *Editor) SetKeyMap(km keymap.KeyMap, cfg config.KeyMapConfig) {
	e.keymap = km
	e.overrides = cloneBindings(cfg.Bindings)
	e.entries = buildEntries()
	e.updateFilter()
	e.ensureCursorValid()
}

// Show displays the editor.
func (e *Editor) Show(km keymap.KeyMap, cfg config.KeyMapConfig) {
	e.visible = true
	e.SetKeyMap(km, cfg)
	e.scroll = 0
	e.cursor = 0
	e.editing = false
	e.errMsg = ""
	e.filtering = false
	e.filterInput.Blur()
	e.clearConfirm()
}

// Hide hides the editor.
func (e *Editor) Hide() {
	e.visible = false
	e.editing = false
	e.errMsg = ""
	e.clearConfirm()
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
	if e.confirming {
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("y", "Y", "enter"))):
			return e, e.applyConfirm(true)
		case key.Matches(msg, key.NewBinding(key.WithKeys("n", "N", "esc"))):
			return e, e.applyConfirm(false)
		}
		return e, nil
	}

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

	if e.filtering {
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))),
			key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			e.filtering = false
			e.filterInput.Blur()
			return e, nil
		}

		prev := e.filterInput.Value()
		var cmd tea.Cmd
		e.filterInput, cmd = e.filterInput.Update(msg)
		if e.filterInput.Value() != prev {
			e.updateFilter()
			e.ensureCursorValid()
		}
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
	case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
		e.filtering = true
		e.filterInput.Focus()
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
	filterLineY := listStart - 1

	if msg.Button == tea.MouseButtonLeft {
		if e.confirming {
			for _, btn := range e.buttons {
				if contentY == btn.y && contentX >= btn.x0 && contentX < btn.x1 {
					switch btn.id {
					case buttonConfirmYes:
						return e, e.applyConfirm(true)
					case buttonConfirmNo:
						return e, e.applyConfirm(false)
					}
				}
			}
			return e, nil
		}
		if contentY == filterLineY {
			e.filtering = true
			e.filterInput.Focus()
			return e, nil
		}
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
	if index < 0 || index >= len(e.visibleEntries) {
		return e, nil
	}
	entryIndex := e.visibleEntries[index]
	if e.entries[entryIndex].kind != entryAction {
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
	entry, ok := e.currentEntry()
	if !ok || entry.kind != entryAction {
		return
	}
	current := bindingHint(keymap.BindingForAction(e.keymap, entry.action))
	e.editing = true
	e.errMsg = ""
	e.input.SetValue(current)
	e.input.CursorEnd()
	e.input.Focus()
}

func (e *Editor) applyInput() tea.Cmd {
	entry, ok := e.currentEntry()
	if !ok {
		return nil
	}
	keys := parseKeys(e.input.Value())
	if len(keys) == 0 {
		e.errMsg = "Enter at least one key (use commas to separate)"
		return nil
	}

	if entry.action == keymap.ActionLeader {
		currentKeys := keymap.BindingForAction(e.keymap, entry.action).Keys()
		if !equalKeySet(currentKeys, keys) {
			e.requestLeaderChange(keys)
			return nil
		}
	}

	actionKey := string(entry.action)
	e.overrides[actionKey] = keys
	e.rebuildKeymap()
	e.editing = false
	e.errMsg = ""
	return e.emitUpdate()
}

func (e *Editor) resetCurrent() tea.Cmd {
	entry, ok := e.currentEntry()
	if !ok || entry.kind != entryAction {
		return nil
	}
	actionKey := string(entry.action)
	delete(e.overrides, actionKey)
	e.rebuildKeymap()
	return e.emitUpdate()
}

func (e *Editor) resetAll() tea.Cmd {
	e.requestResetAll()
	return nil
}

func (e *Editor) requestResetAll() {
	e.confirming = true
	e.confirmAction = confirmResetAll
	e.confirmMessage = "Reset all keybindings to defaults?"
	e.pendingKeys = nil
	e.editing = false
	e.filtering = false
	e.filterInput.Blur()
}

func (e *Editor) requestLeaderChange(keys []string) {
	e.confirming = true
	e.confirmAction = confirmLeaderChange
	e.confirmMessage = fmt.Sprintf("Change leader to %s?", strings.Join(keys, "/"))
	e.pendingKeys = keys
	e.editing = false
	e.filtering = false
	e.filterInput.Blur()
}

func (e *Editor) applyConfirm(confirmed bool) tea.Cmd {
	if !e.confirming {
		return nil
	}
	if !confirmed {
		e.clearConfirm()
		return nil
	}
	switch e.confirmAction {
	case confirmResetAll:
		e.overrides = map[string][]string{}
		e.rebuildKeymap()
		e.clearConfirm()
		return e.emitUpdate()
	case confirmLeaderChange:
		if len(e.pendingKeys) > 0 {
			e.overrides[string(keymap.ActionLeader)] = e.pendingKeys
			e.rebuildKeymap()
		}
		e.clearConfirm()
		return e.emitUpdate()
	default:
		e.clearConfirm()
		return nil
	}
}

func (e *Editor) clearConfirm() {
	e.confirming = false
	e.confirmAction = confirmNone
	e.confirmMessage = ""
	e.pendingKeys = nil
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
	if len(e.visibleEntries) == 0 {
		return
	}
	next := e.cursor + delta
	for next >= 0 && next < len(e.visibleEntries) {
		if e.entries[e.visibleEntries[next]].kind == entryAction {
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

	filterLabel := lipgloss.NewStyle().
		Foreground(common.ColorMuted).
		Render("Filter: ")
	filterWidth := contentWidth - lipgloss.Width(filterLabel)
	if filterWidth < 6 {
		filterWidth = 6
	}
	e.filterInput.Width = filterWidth
	filterLine := filterLabel + e.filterInput.View()

	lines := []string{
		title,
		subtitle,
		ansi.Truncate(tip, contentWidth, ""),
		ansi.Truncate(filterLine, contentWidth, ""),
	}

	if len(e.visibleEntries) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(common.ColorMuted).Render("No matches"))
		for i := 1; i < listHeight; i++ {
			lines = append(lines, "")
		}
	} else {
		for i := 0; i < listHeight; i++ {
			idx := e.scroll + i
			if idx >= len(e.visibleEntries) {
				lines = append(lines, "")
				continue
			}
			entryIndex := e.visibleEntries[idx]
			lines = append(lines, e.renderEntry(entryIndex, contentWidth, idx == e.cursor))
		}
	}

	warnings := e.warningLines(4)
	if len(warnings) > 0 {
		for _, warning := range warnings {
			lines = append(lines, ansi.Truncate(warning, contentWidth, ""))
		}
	}

	if e.confirming {
		confirmLine := lipgloss.NewStyle().
			Foreground(common.ColorWarning).
			Render(e.confirmMessage + " (y/n)")
		lines = append(lines, ansi.Truncate(confirmLine, contentWidth, ""))
	}

	if e.editing {
		entry, ok := e.currentEntry()
		if ok {
			label := fmt.Sprintf("Keys for %s:", entry.label)
			lines = append(lines, "")
			lines = append(lines, ansi.Truncate(label, contentWidth, ""))
			e.input.Width = contentWidth
			lines = append(lines, ansi.Truncate(e.input.View(), contentWidth, ""))
			if e.errMsg != "" {
				errLine := lipgloss.NewStyle().Foreground(common.ColorError).Render(e.errMsg)
				lines = append(lines, ansi.Truncate(errLine, contentWidth, ""))
			}
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

func (e *Editor) renderEntry(index int, contentWidth int, selected bool) string {
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
		if selected {
			return e.styles.SelectedRow.Render(line)
		}
		return line
	default:
		return ""
	}
}

func (e *Editor) renderFooter(contentWidth int, footerY int) string {
	e.buttons = e.buttons[:0]
	var labels []struct {
		id    buttonID
		label string
	}
	if e.confirming {
		labels = []struct {
			id    buttonID
			label string
		}{
			{buttonConfirmYes, "[ Yes ]"},
			{buttonConfirmNo, "[ No ]"},
		}
	} else {
		labels = []struct {
			id    buttonID
			label string
		}{
			{buttonDone, "[ Done ]"},
			{buttonReset, "[ Reset ]"},
			{buttonResetAll, "[ Reset All ]"},
		}
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

	headerLines := 4
	inputLines := 0
	if e.editing {
		inputLines = 3
		if e.errMsg != "" {
			inputLines++
		}
	} else if e.errMsg != "" {
		inputLines = 2
	}
	warningLines := e.warningCount(4)
	confirmLines := 0
	if e.confirming {
		confirmLines = 1
	}
	footerLines := 2

	listHeight = boxHeight - headerLines - inputLines - warningLines - confirmLines - footerLines
	if listHeight < 3 {
		listHeight = 3
	}

	listStart = headerLines
	footerY = listStart + listHeight + inputLines + warningLines + confirmLines + 1
	return boxWidth, boxHeight, listHeight, listStart, footerY
}

func (e *Editor) hasOverride(action keymap.Action) bool {
	_, ok := e.overrides[string(action)]
	return ok
}

func (e *Editor) updateFilter() {
	query := strings.ToLower(strings.TrimSpace(e.filterInput.Value()))
	e.visibleEntries = e.visibleEntries[:0]
	if query == "" {
		for i := range e.entries {
			e.visibleEntries = append(e.visibleEntries, i)
		}
		e.scroll = 0
		return
	}

	for i, entry := range e.entries {
		if entry.kind != entryAction {
			continue
		}
		if strings.Contains(strings.ToLower(entry.label), query) ||
			strings.Contains(strings.ToLower(string(entry.action)), query) {
			e.visibleEntries = append(e.visibleEntries, i)
		}
	}
	e.scroll = 0
}

func (e *Editor) ensureCursorValid() {
	if len(e.visibleEntries) == 0 {
		e.cursor = 0
		e.scroll = 0
		return
	}
	if e.cursor >= len(e.visibleEntries) {
		e.cursor = len(e.visibleEntries) - 1
	}
	if e.cursor < 0 {
		e.cursor = 0
	}
	if e.entries[e.visibleEntries[e.cursor]].kind == entryAction {
		e.ensureVisible()
		return
	}
	for i, idx := range e.visibleEntries {
		if e.entries[idx].kind == entryAction {
			e.cursor = i
			e.ensureVisible()
			return
		}
	}
	e.cursor = 0
	e.scroll = 0
}

func (e *Editor) currentEntry() (entry, bool) {
	if len(e.visibleEntries) == 0 || e.cursor < 0 || e.cursor >= len(e.visibleEntries) {
		return entry{}, false
	}
	return e.entries[e.visibleEntries[e.cursor]], true
}

func (e *Editor) warningCount(limit int) int {
	lines := e.warningLines(limit)
	if len(lines) == 0 {
		return 0
	}
	return len(lines)
}

func (e *Editor) warningLines(limit int) []string {
	warnings := e.validationWarnings()
	if len(warnings) == 0 {
		return nil
	}
	if limit > 0 && len(warnings) > limit {
		remaining := len(warnings) - limit
		warnings = warnings[:limit]
		warnings = append(warnings, fmt.Sprintf("…and %d more", remaining))
	}
	out := make([]string, 0, len(warnings)+1)
	out = append(out, lipgloss.NewStyle().Foreground(common.ColorWarning).Render("Warnings:"))
	out = append(out, warnings...)
	return out
}

func (e *Editor) validationWarnings() []string {
	infos := keymap.ActionInfos()
	keyToActions := make(map[string]map[string][]string)

	for _, info := range infos {
		binding := keymap.BindingForAction(e.keymap, info.Action)
		for _, keyName := range binding.Keys() {
			if keyName == "" {
				continue
			}
			normalized := strings.ToLower(keyName)
			groupMap := keyToActions[normalized]
			if groupMap == nil {
				groupMap = make(map[string][]string)
				keyToActions[normalized] = groupMap
			}
			groupMap[info.Group] = append(groupMap[info.Group], info.Desc)
		}
	}

	var warnings []string

	leaderBinding := keymap.BindingForAction(e.keymap, keymap.ActionLeader)
	for _, keyName := range leaderBinding.Keys() {
		if keyName == "" {
			continue
		}
		if isRiskyLeaderKey(keyName) {
			warnings = append(warnings, fmt.Sprintf("Leader '%s' may conflict with terminal input", keyName))
		}
	}

	var dupEntries []string
	for keyName, groups := range keyToActions {
		for group, actions := range groups {
			if len(actions) > 1 {
				entry := keyName + "|" + group + "|" + strings.Join(uniqueStrings(actions), ", ")
				dupEntries = append(dupEntries, entry)
			}
		}
	}
	sort.Strings(dupEntries)
	for _, entry := range dupEntries {
		parts := strings.SplitN(entry, "|", 3)
		if len(parts) == 3 {
			warnings = append(warnings, fmt.Sprintf("Duplicate '%s' in %s: %s", parts[0], parts[1], parts[2]))
		}
	}

	var reservedKeys []string
	for keyName, groups := range keyToActions {
		var actions []string
		for _, groupActions := range groups {
			actions = append(actions, groupActions...)
		}
		if isTerminalReservedKey(keyName) {
			reservedKeys = append(reservedKeys, keyName+"|"+strings.Join(uniqueStrings(actions), ", "))
		}
	}
	sort.Strings(reservedKeys)
	for _, item := range reservedKeys {
		parts := strings.SplitN(item, "|", 2)
		if len(parts) == 2 {
			warnings = append(warnings, fmt.Sprintf("Terminal key '%s' bound to %s", parts[0], parts[1]))
		}
	}

	return warnings
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

func equalKeySet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	normalize := func(values []string) []string {
		out := make([]string, len(values))
		for i, value := range values {
			out[i] = strings.ToLower(strings.TrimSpace(value))
		}
		sort.Strings(out)
		return out
	}
	aa := normalize(a)
	bb := normalize(b)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
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

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	var out []string
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func isTerminalReservedKey(keyName string) bool {
	keyName = strings.ToLower(strings.TrimSpace(keyName))
	switch keyName {
	case "ctrl+c",
		"ctrl+d",
		"ctrl+z",
		"ctrl+\\",
		"ctrl+s",
		"ctrl+q",
		"ctrl+r",
		"ctrl+w",
		"ctrl+u",
		"ctrl+a",
		"ctrl+e",
		"ctrl+k",
		"ctrl+l":
		return true
	default:
		return false
	}
}

func isRiskyLeaderKey(keyName string) bool {
	keyName = strings.ToLower(strings.TrimSpace(keyName))
	if keyName == "" {
		return false
	}
	if isTerminalReservedKey(keyName) {
		return true
	}
	if strings.HasPrefix(keyName, "ctrl+") || strings.HasPrefix(keyName, "alt+") || strings.HasPrefix(keyName, "shift+") {
		return false
	}
	if strings.HasPrefix(keyName, "meta+") || strings.HasPrefix(keyName, "cmd+") {
		return false
	}
	if len(keyName) == 1 {
		return true
	}
	return keyName == "space"
}
