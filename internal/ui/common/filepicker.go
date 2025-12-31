package common

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	ti.Width = 45

	fp := &FilePicker{
		id:              id,
		title:           "Select Directory",
		currentPath:     startPath,
		input:           ti,
		showHidden:      false,
		directoriesOnly: directoriesOnly,
		maxVisible:      8,
	}

	fp.loadDirectory()
	return fp
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
	query = strings.ToLower(query)
	for i, e := range fp.entries {
		if fuzzyMatch(query, e.Name()) {
			fp.filteredIdx = append(fp.filteredIdx, i)
		}
	}

	if fp.cursor >= len(fp.filteredIdx) {
		fp.cursor = max(0, len(fp.filteredIdx)-1)
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
	case tea.KeyMsg:
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
			if len(fp.filteredIdx) > 0 {
				entry := fp.entries[fp.filteredIdx[fp.cursor]]
				fp.input.SetValue(entry.Name())
				fp.applyFilter()
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "ctrl+n"))):
			if len(fp.filteredIdx) > 0 {
				fp.cursor = (fp.cursor + 1) % len(fp.filteredIdx)
				fp.ensureVisible()
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "ctrl+p"))):
			if len(fp.filteredIdx) > 0 {
				fp.cursor--
				if fp.cursor < 0 {
					fp.cursor = len(fp.filteredIdx) - 1
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
	// If input looks like a path, try to use it directly
	input := fp.input.Value()
	if strings.HasPrefix(input, "~") || strings.HasPrefix(input, "/") {
		// Expand and validate
		path := input
		if strings.HasPrefix(path, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				path = filepath.Join(home, path[1:])
			}
		}
		if info, err := os.Stat(path); err == nil {
			if info.IsDir() {
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

	// If we have a selected entry
	if len(fp.filteredIdx) > 0 && fp.cursor < len(fp.filteredIdx) {
		entry := fp.entries[fp.filteredIdx[fp.cursor]]
		newPath := filepath.Join(fp.currentPath, entry.Name())

		if entry.IsDir() {
			// Navigate into directory
			fp.currentPath = newPath
			fp.input.SetValue("")
			fp.loadDirectory()
			return fp, nil
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

// ensureVisible scrolls to keep cursor visible
func (fp *FilePicker) ensureVisible() {
	if fp.cursor < fp.scrollOffset {
		fp.scrollOffset = fp.cursor
	} else if fp.cursor >= fp.scrollOffset+fp.maxVisible {
		fp.scrollOffset = fp.cursor - fp.maxVisible + 1
	}
}

// View renders the picker
func (fp *FilePicker) View() string {
	if !fp.visible {
		return ""
	}

	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)
	content.WriteString(titleStyle.Render(fp.title))
	content.WriteString("\n\n")

	// Current path
	pathStyle := lipgloss.NewStyle().
		Foreground(ColorSecondary)
	content.WriteString(pathStyle.Render(fp.currentPath))
	content.WriteString("\n\n")

	// Input
	content.WriteString(fp.input.View())
	content.WriteString("\n\n")

	// Entries
	if len(fp.filteredIdx) == 0 {
		content.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("No matches"))
	} else {
		end := min(fp.scrollOffset+fp.maxVisible, len(fp.filteredIdx))
		for i := fp.scrollOffset; i < end; i++ {
			idx := fp.filteredIdx[i]
			entry := fp.entries[idx]

			cursor := "  "
			if i == fp.cursor {
				cursor = "> "
			}

			name := entry.Name()
			style := lipgloss.NewStyle().Foreground(ColorForeground)
			if entry.IsDir() {
				name += "/"
				style = lipgloss.NewStyle().Foreground(ColorSecondary).Bold(i == fp.cursor)
			}
			if i == fp.cursor {
				style = style.Background(ColorSelection)
			}

			content.WriteString(cursor + style.Render(name) + "\n")
		}

		// Scroll indicator
		if len(fp.filteredIdx) > fp.maxVisible {
			indicator := lipgloss.NewStyle().Foreground(ColorMuted).Render(
				fmt.Sprintf("  (%d-%d of %d)", fp.scrollOffset+1, end, len(fp.filteredIdx)),
			)
			content.WriteString(indicator)
		}
	}

	// Help
	helpStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		MarginTop(1)
	content.WriteString("\n")
	content.WriteString(helpStyle.Render("enter: select • esc: cancel • tab: complete • ctrl+h: toggle hidden"))

	// Dialog box
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(55)

	return dialogStyle.Render(content.String())
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
