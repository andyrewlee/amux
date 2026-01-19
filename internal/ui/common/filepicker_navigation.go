package common

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

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
