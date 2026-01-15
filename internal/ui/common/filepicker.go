package common

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// FilePicker is a file/directory picker dialog
type FilePicker struct {
	id              string
	title           string
	currentPath     string
	entries         []os.DirEntry
	filteredIdx     []int
	cursor          int
	input           textinput.Model
	showHidden      bool
	directoriesOnly bool
	visible         bool
	width           int
	height          int
	scrollOffset    int
	maxVisible      int
	rowHits         []filePickerRowHit
	buttonHits      []HitRegion
	styles          Styles
	showKeymapHints bool
}

type filePickerRowHit struct {
	index  int
	region HitRegion
}

// NewFilePicker creates a new file picker starting at the given path
func NewFilePicker(id, startPath string, directoriesOnly bool) *FilePicker {
	// Expand ~ to home directory
	if strings.HasPrefix(startPath, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			startPath = filepath.Join(home, startPath[1:])
		}
	}

	ti := textinput.New()
	ti.Placeholder = "Type to filter or enter path..."
	ti.Focus()
	ti.CharLimit = 200
	ti.SetWidth(45)
	ti.SetVirtualCursor(false)

	fp := &FilePicker{
		id:              id,
		title:           "Select Directory",
		currentPath:     startPath,
		input:           ti,
		showHidden:      false,
		directoriesOnly: directoriesOnly,
		maxVisible:      8,
		styles:          DefaultStyles(),
		showKeymapHints: true,
	}

	fp.loadDirectory()
	return fp
}

// SetShowKeymapHints controls whether helper text is rendered.
func (fp *FilePicker) SetShowKeymapHints(show bool) {
	fp.showKeymapHints = show
}

// SetStyles updates the file picker styles (for theme changes).
func (fp *FilePicker) SetStyles(styles Styles) {
	fp.styles = styles
}

// loadDirectory loads entries from the current path
func (fp *FilePicker) loadDirectory() {
	fp.entries = nil
	fp.filteredIdx = nil
	fp.cursor = 0
	fp.scrollOffset = 0

	entries, err := os.ReadDir(fp.currentPath)
	if err != nil {
		return
	}

	// Filter and sort: directories first, then alphabetically
	var dirs, files []os.DirEntry
	for _, e := range entries {
		// Skip hidden files unless enabled
		if !fp.showHidden && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, e)
		} else if !fp.directoriesOnly {
			files = append(files, e)
		}
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name()) < strings.ToLower(dirs[j].Name())
	})
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name()) < strings.ToLower(files[j].Name())
	})

	fp.entries = append(dirs, files...)
	fp.applyFilter()
}

// applyFilter updates filteredIdx based on input
func (fp *FilePicker) applyFilter() {
	query := fp.input.Value()

	// If query looks like an absolute or relative path, try to navigate
	if strings.HasPrefix(query, "/") || strings.HasPrefix(query, "~") || strings.HasPrefix(query, ".") {
		// Don't filter, let user complete the path
		fp.filteredIdx = make([]int, len(fp.entries))
		for i := range fp.entries {
			fp.filteredIdx[i] = i
		}
		return
	}

	fp.filteredIdx = nil
	if strings.Contains(query, "/") {
		parts := strings.Split(query, "/")
		query = parts[len(parts)-1]
	}
	query = strings.ToLower(query)
	for i, e := range fp.entries {
		if fuzzyMatch(query, e.Name()) {
			fp.filteredIdx = append(fp.filteredIdx, i)
		}
	}

	if fp.cursor >= len(fp.filteredIdx) {
		fp.cursor = min(fp.cursor, len(fp.filteredIdx))
	}
	if fp.cursor < 0 {
		fp.cursor = 0
	}
}

// Show makes the picker visible
func (fp *FilePicker) Show() {
	fp.visible = true
	fp.input.SetValue("")
	fp.input.Focus()
	fp.loadDirectory()
}

// Hide hides the picker
func (fp *FilePicker) Hide() {
	fp.visible = false
}

// Visible returns whether the picker is visible
func (fp *FilePicker) Visible() bool {
	return fp.visible
}

// Update handles messages
func (fp *FilePicker) Update(msg tea.Msg) (*FilePicker, tea.Cmd) {
	if !fp.visible {
		return fp, nil
	}

	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			fp.moveCursor(-1)
			return fp, nil
		}
		if msg.Button == tea.MouseWheelDown {
			fp.moveCursor(1)
			return fp, nil
		}

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			contentHeight := len(fp.renderLines())
			if contentHeight == 0 {
				return fp, nil
			}

			dialogX, dialogY, dialogW, dialogH := fp.dialogBounds(contentHeight)
			if msg.X < dialogX || msg.X >= dialogX+dialogW || msg.Y < dialogY || msg.Y >= dialogY+dialogH {
				return fp, nil
			}

			_, _, contentOffsetX, contentOffsetY := fp.dialogFrame()
			localX := msg.X - dialogX - contentOffsetX
			localY := msg.Y - dialogY - contentOffsetY
			if localX < 0 || localY < 0 {
				return fp, nil
			}

			for _, hit := range fp.buttonHits {
				if hit.Contains(localX, localY) {
					switch hit.ID {
					case "open":
						return fp.handleEnter()
					case "open-typed":
						if fp.handleOpenFromInput() {
							return fp, nil
						}
					case "autocomplete":
						fp.handleAutocomplete()
						return fp, nil
					case "up":
						parent := filepath.Dir(fp.currentPath)
						if parent != fp.currentPath {
							fp.currentPath = parent
							fp.loadDirectory()
						}
						return fp, nil
					case "hidden":
						fp.showHidden = !fp.showHidden
						fp.loadDirectory()
						return fp, nil
					case "cancel":
						fp.visible = false
						return fp, func() tea.Msg { return DialogResult{ID: fp.id, Confirmed: false} }
					}
				}
			}

			for _, hit := range fp.rowHits {
				if hit.region.Contains(localX, localY) {
					fp.cursor = hit.index
					fp.ensureVisible()
					return fp.handleEnter()
				}
			}
		}

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			fp.visible = false
			return fp, func() tea.Msg {
				return DialogResult{ID: fp.id, Confirmed: false}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			return fp.handleEnter()

		case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
			if fp.handleOpenFromInput() {
				return fp, nil
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
			// Tab = autocomplete or select first match
			fp.handleAutocomplete()

		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "ctrl+n"))):
			if fp.displayCount() > 0 {
				fp.cursor = (fp.cursor + 1) % fp.displayCount()
				fp.ensureVisible()
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "ctrl+p"))):
			if fp.displayCount() > 0 {
				fp.cursor--
				if fp.cursor < 0 {
					fp.cursor = fp.displayCount() - 1
				}
				fp.ensureVisible()
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("backspace"))):
			// If input is empty and backspace, go up a directory
			if fp.input.Value() == "" {
				parent := filepath.Dir(fp.currentPath)
				if parent != fp.currentPath {
					fp.currentPath = parent
					fp.loadDirectory()
				}
				return fp, nil
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+h"))):
			// Toggle hidden files
			fp.showHidden = !fp.showHidden
			fp.loadDirectory()
			return fp, nil
		}
	}

	// Update text input
	oldValue := fp.input.Value()
	var cmd tea.Cmd
	fp.input, cmd = fp.input.Update(msg)

	// Check if input is a path we should navigate to
	newValue := fp.input.Value()
	if newValue != oldValue {
		fp.handlePathInput(newValue)
	}

	return fp, cmd
}

// handlePathInput checks if the input is a navigable path
func (fp *FilePicker) handlePathInput(input string) {
	// Expand ~ to home
	if strings.HasPrefix(input, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded := filepath.Join(home, input[1:])
			if info, err := os.Stat(expanded); err == nil && info.IsDir() {
				fp.currentPath = expanded
				fp.input.SetValue("")
				fp.loadDirectory()
				return
			}
		}
	}

	// Check if it's an absolute path
	if strings.HasPrefix(input, "/") {
		if info, err := os.Stat(input); err == nil && info.IsDir() {
			fp.currentPath = input
			fp.input.SetValue("")
			fp.loadDirectory()
			return
		}
	}

	// Otherwise, just filter
	fp.applyFilter()
}

// handleEnter handles the enter key
func (fp *FilePicker) handleEnter() (*FilePicker, tea.Cmd) {
	// If input looks like a path, try to open it
	input := strings.TrimSpace(fp.input.Value())
	if input != "" {
		path := input
		if strings.HasPrefix(path, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				path = filepath.Join(home, path[1:])
			}
		} else if !filepath.IsAbs(path) {
			path = filepath.Join(fp.currentPath, path)
		}
		if info, err := os.Stat(path); err == nil {
			if info.IsDir() {
				fp.currentPath = path
				fp.input.SetValue("")
				fp.loadDirectory()
				return fp, nil
			}
			if !fp.directoriesOnly {
				fp.visible = false
				return fp, func() tea.Msg {
					return DialogResult{
						ID:        fp.id,
						Confirmed: true,
						Value:     path,
					}
				}
			}
		}
	}

	// If cursor is on the "Use this directory" row, select current directory.
	if fp.cursor == 0 {
		fp.visible = false
		return fp, func() tea.Msg {
			return DialogResult{
				ID:        fp.id,
				Confirmed: true,
				Value:     fp.currentPath,
			}
		}
	}

	// If we have a selected entry, open directories
	if len(fp.filteredIdx) > 0 && fp.cursor > 0 && fp.cursor-1 < len(fp.filteredIdx) {
		entry := fp.entries[fp.filteredIdx[fp.cursor-1]]
		if entry.IsDir() {
			newPath := filepath.Join(fp.currentPath, entry.Name())
			fp.currentPath = newPath
			fp.input.SetValue("")
			fp.loadDirectory()
			return fp, nil
		}
		if !fp.directoriesOnly {
			selectedPath := filepath.Join(fp.currentPath, entry.Name())
			fp.visible = false
			return fp, func() tea.Msg {
				return DialogResult{
					ID:        fp.id,
					Confirmed: true,
					Value:     selectedPath,
				}
			}
		}
	}

	// Otherwise, select current directory
	fp.visible = false
	return fp, func() tea.Msg {
		return DialogResult{
			ID:        fp.id,
			Confirmed: true,
			Value:     fp.currentPath,
		}
	}
}

// handleOpenFromInput navigates into the path typed in the input when it is a directory.
func (fp *FilePicker) handleOpenFromInput() bool {
	input := strings.TrimSpace(fp.input.Value())
	if input == "" {
		return false
	}

	path := input
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[1:])
		}
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(fp.currentPath, path)
	}

	if info, err := os.Stat(path); err == nil && info.IsDir() {
		fp.currentPath = path
		fp.input.SetValue("")
		fp.loadDirectory()
		return true
	}

	return false
}

// ensureVisible scrolls to keep cursor visible
func (fp *FilePicker) ensureVisible() {
	total := fp.displayCount()
	if total == 0 {
		fp.scrollOffset = 0
		return
	}

	if fp.cursor < fp.scrollOffset {
		fp.scrollOffset = fp.cursor
	} else if fp.cursor >= fp.scrollOffset+fp.maxVisible {
		fp.scrollOffset = fp.cursor - fp.maxVisible + 1
	}
}

func (fp *FilePicker) moveCursor(delta int) {
	total := fp.displayCount()
	if total == 0 {
		return
	}

	if delta > 0 {
		fp.cursor = (fp.cursor + 1) % total
	} else if delta < 0 {
		fp.cursor--
		if fp.cursor < 0 {
			fp.cursor = total - 1
		}
	}
	fp.ensureVisible()
}

func (fp *FilePicker) displayCount() int {
	return len(fp.filteredIdx) + 1
}

// View renders the picker
func (fp *FilePicker) View() string {
	if !fp.visible {
		return ""
	}

	lines := fp.renderLines()
	content := strings.Join(lines, "\n")
	return fp.dialogStyle().Render(content)
}

const filePickerContentWidth = 55

func (fp *FilePicker) dialogStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(filePickerContentWidth)
}

func (fp *FilePicker) dialogFrame() (frameX, frameY, offsetX, offsetY int) {
	frameX, frameY = fp.dialogStyle().GetFrameSize()
	offsetX = frameX / 2
	offsetY = frameY / 2
	return frameX, frameY, offsetX, offsetY
}

func (fp *FilePicker) dialogBounds(contentHeight int) (x, y, w, h int) {
	frameX, frameY, _, _ := fp.dialogFrame()
	w = filePickerContentWidth + frameX
	h = contentHeight + frameY
	x = (fp.width - w) / 2
	y = (fp.height - h) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y, w, h
}

func (fp *FilePicker) renderLines() []string {
	fp.rowHits = fp.rowHits[:0]
	fp.buttonHits = fp.buttonHits[:0]

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

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)
	appendLines(titleStyle.Render(fp.title))
	appendBlank(2)

	// Current path
	pathStyle := lipgloss.NewStyle().
		Foreground(ColorSecondary)
	appendLines(pathStyle.Render(fp.currentPath))
	appendBlank(2)

	// Input
	appendLines(fp.input.View())
	appendBlank(2)

	// Entries
	totalRows := fp.displayCount()
	end := min(fp.scrollOffset+fp.maxVisible, totalRows)
	for i := fp.scrollOffset; i < end; i++ {
		cursor := "  "
		if i == fp.cursor {
			cursor = "> "
		}

		lineIndex := len(lines)
		if i == 0 {
			label := "Use this directory"
			style := lipgloss.NewStyle().Foreground(ColorForeground)
			if i == fp.cursor {
				style = style.Background(ColorSelection).Bold(true)
			}
			line := cursor + style.Render(label)
			fp.addRowHit(i, lineIndex, line)
			lines = append(lines, line)
			continue
		}

		idx := fp.filteredIdx[i-1]
		entry := fp.entries[idx]

		name := entry.Name()
		style := lipgloss.NewStyle().Foreground(ColorForeground)
		if entry.IsDir() {
			name += "/"
			style = lipgloss.NewStyle().Foreground(ColorSecondary).Bold(i == fp.cursor)
		}
		if i == fp.cursor {
			style = style.Background(ColorSelection)
		}

		line := cursor + style.Render(name)
		fp.addRowHit(i, lineIndex, line)
		lines = append(lines, line)
	}

	if len(fp.filteredIdx) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(ColorMuted).Render("No matches"))
	} else if totalRows > fp.maxVisible {
		indicator := lipgloss.NewStyle().Foreground(ColorMuted).Render(
			fmt.Sprintf("  (%d-%d of %d)", fp.scrollOffset+1, end, totalRows),
		)
		lines = append(lines, indicator)
	}

	// Action buttons
	appendBlank(1)
	lines = append(lines, fp.renderButtonsLine(len(lines)))

	if fp.showKeymapHints {
		appendBlank(1)
		helpWidth := 51
		helpLines := fp.helpLines(helpWidth)
		lines = append(lines, helpLines...)
	}

	return lines
}

func (fp *FilePicker) renderButtonsLine(baseLine int) string {
	buttonStyle := lipgloss.NewStyle().
		Foreground(ColorForeground).
		Background(ColorSelection).
		Padding(0, 1)

	buttons := []struct {
		id    string
		label string
	}{
		{id: "open", label: buttonStyle.Render("Open")},
		{id: "open-typed", label: buttonStyle.Render("Open typed")},
		{id: "autocomplete", label: buttonStyle.Render("Autocomplete")},
		{id: "up", label: buttonStyle.Render("Up")},
		{id: "hidden", label: buttonStyle.Render(fp.hiddenLabel())},
		{id: "cancel", label: buttonStyle.Render("Cancel")},
	}

	var parts []string
	x := 0
	for i, btn := range buttons {
		width := min(lipgloss.Width(btn.label), filePickerContentWidth-x)
		fp.addButtonHit(btn.id, baseLine, x, width)
		parts = append(parts, btn.label)
		x += width
		if i < len(buttons)-1 {
			x += 2
		}
	}

	return strings.Join(parts, "  ")
}

func (fp *FilePicker) addRowHit(index, lineIndex int, line string) {
	width := min(lipgloss.Width(line), filePickerContentWidth)
	if width <= 0 {
		return
	}
	fp.rowHits = append(fp.rowHits, filePickerRowHit{
		index: index,
		region: HitRegion{
			X:      0,
			Y:      lineIndex,
			Width:  width,
			Height: 1,
		},
	})
}

func (fp *FilePicker) addButtonHit(id string, lineIndex, x, width int) {
	if width <= 0 {
		return
	}
	fp.buttonHits = append(fp.buttonHits, HitRegion{
		ID:     id,
		X:      x,
		Y:      lineIndex,
		Width:  width,
		Height: 1,
	})
}

// Cursor returns the cursor position relative to the file picker view.
func (fp *FilePicker) Cursor() *tea.Cursor {
	if !fp.visible || fp.input.VirtualCursor() || !fp.input.Focused() {
		return nil
	}

	c := fp.input.Cursor()
	if c == nil {
		return nil
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)
	pathStyle := lipgloss.NewStyle().
		Foreground(ColorSecondary)

	prefix := titleStyle.Render(fp.title) + "\n\n" + pathStyle.Render(fp.currentPath) + "\n\n"
	c.Y += lipgloss.Height(prefix)

	// Account for border + padding (Border=1, Padding=(1,2)).
	c.X += 3
	c.Y += 2

	return c
}

func (fp *FilePicker) helpItem(key, desc string) string {
	return RenderHelpItem(fp.styles, key, desc)
}

func (fp *FilePicker) helpLines(width int) []string {
	items := []string{
		fp.helpItem("enter", "open"),
		fp.helpItem("esc", "cancel"),
		fp.helpItem("↑", "up"),
		fp.helpItem("↓", "down"),
		fp.helpItem("ctrl+n/p", "move"),
		fp.helpItem("tab", "autocomplete"),
		fp.helpItem("/", "open typed"),
		fp.helpItem("backspace", "parent"),
		fp.helpItem("ctrl+h", "hidden"),
	}
	return WrapHelpItems(items, width)
}

func (fp *FilePicker) handleAutocomplete() {
	if fp.cursor > 0 && len(fp.filteredIdx) > 0 {
		entry := fp.entries[fp.filteredIdx[fp.cursor-1]]
		if entry.IsDir() {
			fp.input.SetValue(entry.Name() + "/")
			if fp.handleOpenFromInput() {
				return
			}
		} else {
			fp.input.SetValue(entry.Name())
		}
		fp.applyFilter()
	}
}

func (fp *FilePicker) hiddenLabel() string {
	if fp.showHidden {
		return "Hide hidden"
	}
	return "Show hidden"
}

// SetSize sets the picker size
func (fp *FilePicker) SetSize(width, height int) {
	fp.width = width
	fp.height = height
	fp.maxVisible = min(10, (height-15)/2)
	if fp.maxVisible < 3 {
		fp.maxVisible = 3
	}
}
