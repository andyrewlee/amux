package common

import (
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// FilePicker is a file/directory picker dialog
type FilePicker struct {
	id                string
	title             string
	currentPath       string
	entries           []os.DirEntry
	filteredIdx       []int
	cursor            int
	input             textinput.Model
	showHidden        bool
	directoriesOnly   bool
	visible           bool
	width             int
	height            int
	scrollOffset      int
	maxVisible        int
	rowHits           []filePickerRowHit
	buttonHits        []HitRegion
	styles            Styles
	showKeymapHints   bool
	primaryAction     string
	lastContentHeight int // Cached from View() for click handling
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
	ti.Placeholder = "Type to filter or paste path..."
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
		primaryAction:   "Open",
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

// SetTitle updates the dialog title.
func (fp *FilePicker) SetTitle(title string) {
	if title == "" {
		return
	}
	fp.title = title
}

// SetPrimaryActionLabel updates the primary action label.
func (fp *FilePicker) SetPrimaryActionLabel(label string) {
	if label == "" {
		return
	}
	fp.primaryAction = label
}

// Show makes the picker visible
func (fp *FilePicker) Show() {
	fp.visible = true
	fp.input.SetValue(fp.inputBasePath())
	fp.input.CursorEnd()
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

func (fp *FilePicker) inputBasePath() string {
	base := filepath.Clean(fp.currentPath)
	sep := string(os.PathSeparator)
	if base == sep {
		return base
	}
	if !strings.HasSuffix(base, sep) {
		base += sep
	}
	return base
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
			// Use cached content height from last View() render
			if fp.lastContentHeight == 0 {
				return fp, nil
			}

			dialogX, dialogY, dialogW, dialogH := fp.dialogBounds(fp.lastContentHeight)
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
						return fp.confirmCurrentDirectory()
					case "up":
						parent := filepath.Dir(fp.currentPath)
						if parent != fp.currentPath {
							fp.currentPath = parent
							fp.input.SetValue(fp.inputBasePath())
							fp.input.CursorEnd()
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
			if fp.handleBackspace() {
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
	return len(fp.filteredIdx)
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
