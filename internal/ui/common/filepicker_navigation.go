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

// applyFilter updates filteredIdx based on input.
// filteredIdx == nil means no filter is active (no suggestions shown).
// filteredIdx == []int{} (non-nil, empty) means filter is active but nothing matched.
func (fp *FilePicker) applyFilter() {
	rawQuery := strings.TrimSpace(fp.input.Value())
	query := rawQuery
	base := fp.inputBasePath()
	sep := string(os.PathSeparator)
	withinCurrent := false

	if rawQuery != "" {
		if strings.HasPrefix(rawQuery, base) {
			query = strings.TrimPrefix(rawQuery, base)
			withinCurrent = true
		} else if strings.HasPrefix(rawQuery, fp.currentPath) {
			trimmed := strings.TrimPrefix(rawQuery, fp.currentPath)
			if trimmed == "" || strings.HasPrefix(trimmed, sep) {
				trimmed = strings.TrimPrefix(trimmed, sep)
				query = trimmed
				withinCurrent = true
			}
		}
	}

	// Only show suggestions when user has typed a filter within the current directory.
	if !withinCurrent || query == "" {
		fp.filteredIdx = nil
		return
	}

	if strings.Contains(query, "/") {
		parts := strings.Split(query, "/")
		query = parts[len(parts)-1]
	}
	query = strings.ToLower(query)
	fp.filteredIdx = []int{} // non-nil empty: filter active
	for i, e := range fp.entries {
		nameLower := strings.ToLower(e.Name())
		if strings.HasPrefix(nameLower, query) {
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

// handlePathInput checks if the input has crossed a directory boundary and
// reloads entries when needed, then reapplies the filter.
func (fp *FilePicker) handlePathInput(input string) {
	trimmed := strings.TrimSpace(input)
	if trimmed != "" && filepath.IsAbs(trimmed) {
		dir := trimmed
		if !strings.HasSuffix(trimmed, string(os.PathSeparator)) {
			dir = filepath.Dir(trimmed)
		}
		dir = filepath.Clean(dir)
		if dir != filepath.Clean(fp.currentPath) {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				fp.currentPath = dir
				fp.loadDirectory() // loadDirectory calls applyFilter
				return
			}
		}
	}
	fp.applyFilter()
}

func (fp *FilePicker) confirmCurrentDirectory() (*FilePicker, tea.Cmd) {
	if fp.multiSelect {
		return fp.multiSelectAdd(fp.currentPath)
	}
	fp.visible = false
	return fp, func() tea.Msg {
		return DialogResult{
			ID:        fp.id,
			Confirmed: true,
			Value:     fp.currentPath,
		}
	}
}

// multiSelectAdd validates and adds a path in multi-select mode.
func (fp *FilePicker) multiSelectAdd(path string) (*FilePicker, tea.Cmd) {
	fp.statusMessage = ""
	if fp.validatePath != nil {
		if errMsg := fp.validatePath(path, fp.selectedPaths); errMsg != "" {
			fp.statusMessage = errMsg
			return fp, nil
		}
	}
	fp.selectedPaths = append(fp.selectedPaths, path)
	return fp, nil
}

// multiSelectDone closes the picker and returns all selected paths.
func (fp *FilePicker) multiSelectDone() (*FilePicker, tea.Cmd) {
	if len(fp.selectedPaths) < 1 {
		fp.statusMessage = "Select at least 1 repo"
		return fp, nil
	}
	fp.visible = false
	paths := make([]string, len(fp.selectedPaths))
	copy(paths, fp.selectedPaths)
	return fp, func() tea.Msg {
		return DialogResult{
			ID:        fp.id,
			Confirmed: true,
			Values:    paths,
		}
	}
}

func (fp *FilePicker) isBaseInput(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return true
	}
	base := fp.inputBasePath()
	if trimmed == base {
		return true
	}
	sep := string(os.PathSeparator)
	if strings.HasSuffix(base, sep) && strings.TrimSuffix(base, sep) == trimmed {
		return true
	}
	return filepath.Clean(trimmed) == filepath.Clean(fp.currentPath)
}

func (fp *FilePicker) handleBackspace() bool {
	return false // Let textinput handle it normally
}

func (fp *FilePicker) resolveInputPath(input string) (string, bool) {
	path := strings.TrimSpace(input)
	if path == "" {
		return "", false
	}

	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		} else {
			return "", false
		}
	}

	if !filepath.IsAbs(path) {
		return "", false
	}

	return filepath.Clean(path), true
}

// handleEnter handles the enter key
func (fp *FilePicker) handleEnter() (*FilePicker, tea.Cmd) {
	baseInput := strings.TrimSpace(fp.input.Value())
	isBaseInput := fp.isBaseInput(baseInput)

	// If we have a selected entry, open directories.
	if len(fp.filteredIdx) > 0 && fp.cursor >= 0 && fp.cursor < len(fp.filteredIdx) {
		entry := fp.entries[fp.filteredIdx[fp.cursor]]
		if entry.IsDir() {
			newPath := filepath.Join(fp.currentPath, entry.Name())
			fp.currentPath = newPath
			fp.input.SetValue(fp.inputBasePath())
			fp.input.CursorEnd()
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

	// If input looks like a path, try to open/select it.
	if baseInput != "" && !isBaseInput {
		path := baseInput
		if strings.HasPrefix(path, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				path = filepath.Join(home, path[1:])
			}
		} else if !filepath.IsAbs(path) {
			path = filepath.Join(fp.currentPath, path)
		}
		path = filepath.Clean(path)
		if info, err := os.Stat(path); err == nil {
			if info.IsDir() {
				if fp.directoriesOnly {
					if fp.multiSelect {
						return fp.multiSelectAdd(path)
					}
					fp.visible = false
					return fp, func() tea.Msg {
						return DialogResult{
							ID:        fp.id,
							Confirmed: true,
							Value:     path,
						}
					}
				}
				fp.currentPath = path
				fp.input.SetValue(fp.inputBasePath())
				fp.input.CursorEnd()
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
		return fp, nil
	}

	// Otherwise, select current directory
	return fp.confirmCurrentDirectory()
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
	if fp.cursor >= 0 && len(fp.filteredIdx) > 0 && fp.cursor < len(fp.filteredIdx) {
		entry := fp.entries[fp.filteredIdx[fp.cursor]]
		if entry.IsDir() {
			// Navigate directly into the directory (like Enter does)
			newPath := filepath.Join(fp.currentPath, entry.Name())
			fp.currentPath = newPath
			fp.input.SetValue(fp.inputBasePath())
			fp.input.CursorEnd()
			fp.loadDirectory()
		} else {
			fp.input.SetValue(entry.Name())
			fp.applyFilter()
		}
		return
	}
	// Fallback: try to navigate from typed path
	fp.handleOpenFromInput()
}
