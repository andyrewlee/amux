package common

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

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

	// If input looks like an absolute or relative path outside the current directory, don't filter.
	if rawQuery != "" && (strings.HasPrefix(rawQuery, "/") || strings.HasPrefix(rawQuery, "~") || strings.HasPrefix(rawQuery, ".")) && !withinCurrent {
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
	_ = input
	fp.applyFilter()
}

func (fp *FilePicker) confirmCurrentDirectory() (*FilePicker, tea.Cmd) {
	fp.visible = false
	return fp, func() tea.Msg {
		return DialogResult{
			ID:        fp.id,
			Confirmed: true,
			Value:     fp.currentPath,
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
	if fp.input.Value() == "" {
		parent := filepath.Dir(fp.currentPath)
		if parent != fp.currentPath {
			fp.currentPath = parent
			fp.input.SetValue(fp.inputBasePath())
			fp.input.CursorEnd()
			fp.loadDirectory()
			return true
		}
		return false
	}

	if fp.input.Position() != utf8.RuneCountInString(fp.input.Value()) {
		return false
	}

	path, ok := fp.resolveInputPath(fp.input.Value())
	if !ok {
		return false
	}
	if filepath.Clean(path) != filepath.Clean(fp.currentPath) {
		return false
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return false
	}

	parent := filepath.Dir(path)
	if parent == path {
		return false
	}

	fp.currentPath = parent
	fp.input.SetValue(fp.inputBasePath())
	fp.input.CursorEnd()
	fp.loadDirectory()
	return true
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
