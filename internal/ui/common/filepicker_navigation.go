package common

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// directoryLoadedMsg delivers an os.ReadDir result. path pins the result to
// the directory it was issued for — a result for anything but the current
// path is stale and must be discarded.
type directoryLoadedMsg struct {
	path    string
	entries []os.DirEntry
	err     error
}

// resolveOp distinguishes what a pathResolvedMsg result should do.
type resolveOp int

const (
	// resolveEnter is Enter on a typed path: confirm a hit, or navigate when
	// the hit is a directory and the picker is not directories-only.
	resolveEnter resolveOp = iota
	// resolveOpenDir is the autocomplete fallback: navigate into the typed
	// path only when it resolves to a directory.
	resolveOpenDir
)

// pathResolvedMsg delivers an os.Stat result for a typed input path. input is
// the text at issue time; the result is stale when the input has moved on.
type pathResolvedMsg struct {
	input string
	path  string
	op    resolveOp
	info  os.FileInfo
	err   error
}

// markNeedsLoad clears the visible listing and flags a reload. The actual
// os.ReadDir runs inside dueLoadCmd's tea.Cmd — never on the Update
// goroutine — so a slow mount stalls nothing but this picker's listing.
func (fp *FilePicker) markNeedsLoad() {
	fp.entries = nil
	fp.filteredIdx = nil
	fp.cursor = 0
	fp.scrollOffset = 0
	fp.needsLoad = true
}

// dueLoadCmd issues the pending directory read once. Update calls it after
// every handled message, which is what makes deferred opens and drops work:
// any navigation marks needsLoad and the next message round-trip performs
// the read off-goroutine.
func (fp *FilePicker) dueLoadCmd() tea.Cmd {
	if !fp.visible || !fp.needsLoad || fp.loadInFlight {
		return nil
	}
	fp.loadInFlight = true
	return fp.loadDirectoryCmd(fp.currentPath)
}

func (fp *FilePicker) loadDirectoryCmd(path string) tea.Cmd {
	return func() tea.Msg {
		entries, err := os.ReadDir(path)
		return directoryLoadedMsg{path: path, entries: entries, err: err}
	}
}

func resolvePathCmd(input, path string, op resolveOp) tea.Cmd {
	return func() tea.Msg {
		info, err := os.Stat(path)
		return pathResolvedMsg{input: input, path: path, op: op, info: info, err: err}
	}
}

// finishDirectoryLoad applies a fresh ReadDir result on the UI goroutine:
// filter hidden/files per picker settings, sort dirs-first alphabetical, and
// rebuild the filtered view. An error leaves the listing empty, matching the
// previous synchronous behavior.
func (fp *FilePicker) finishDirectoryLoad(entries []os.DirEntry, err error) {
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

	// Clamp the cursor to the last valid row (not one past it) so the selection
	// stays visible as the filter narrows, then re-sync the scroll window so a
	// prior scroll offset can't leave the (now shorter) list rendered empty.
	if fp.cursor >= len(fp.filteredIdx) {
		fp.cursor = len(fp.filteredIdx) - 1
	}
	if fp.cursor < 0 {
		fp.cursor = 0
	}
	fp.ensureVisible()
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
			fp.markNeedsLoad()
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
	// The input naming the currently shown directory means "go up". No stat
	// needed: currentPath is a directory we already listed, and ascending is
	// still correct if it has since been removed.
	if filepath.Clean(path) != filepath.Clean(fp.currentPath) {
		return false
	}

	parent := filepath.Dir(path)
	if parent == path {
		return false
	}

	fp.currentPath = parent
	fp.input.SetValue(fp.inputBasePath())
	fp.input.CursorEnd()
	fp.markNeedsLoad()
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
			fp.markNeedsLoad()
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
		// The stat runs off the Update goroutine; the result applies itself
		// via applyResolvedPath unless the input moved on meanwhile.
		return fp, resolvePathCmd(fp.input.Value(), path, resolveEnter)
	}

	// Otherwise, select current directory
	return fp.confirmCurrentDirectory()
}

// applyResolvedPath applies a fresh pathResolvedMsg. Stale results — the
// input changed since the stat was issued — are dropped without effect.
func (fp *FilePicker) applyResolvedPath(msg pathResolvedMsg) tea.Cmd {
	if fp.input.Value() != msg.input || msg.err != nil || msg.info == nil {
		return nil
	}

	switch msg.op {
	case resolveOpenDir:
		if !msg.info.IsDir() {
			return nil
		}
		fp.currentPath = msg.path
		fp.input.SetValue("")
		fp.markNeedsLoad()
		return nil

	case resolveEnter:
		if msg.info.IsDir() {
			if fp.directoriesOnly {
				fp.visible = false
				path := msg.path
				return func() tea.Msg {
					return DialogResult{ID: fp.id, Confirmed: true, Value: path}
				}
			}
			fp.currentPath = msg.path
			fp.input.SetValue(fp.inputBasePath())
			fp.input.CursorEnd()
			fp.markNeedsLoad()
			return nil
		}
		if !fp.directoriesOnly {
			fp.visible = false
			path := msg.path
			return func() tea.Msg {
				return DialogResult{ID: fp.id, Confirmed: true, Value: path}
			}
		}
	}
	return nil
}

// handleOpenFromInput navigates into the path typed in the input when it is a
// directory. The stat is async; the result navigates via applyResolvedPath.
// Returns nil (no work) for empty input.
func (fp *FilePicker) handleOpenFromInput() tea.Cmd {
	input := strings.TrimSpace(fp.input.Value())
	if input == "" {
		return nil
	}

	path := input
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[1:])
		}
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(fp.currentPath, path)
	}
	path = filepath.Clean(path)

	return resolvePathCmd(fp.input.Value(), path, resolveOpenDir)
}

func (fp *FilePicker) handleAutocomplete() tea.Cmd {
	if fp.cursor >= 0 && len(fp.filteredIdx) > 0 && fp.cursor < len(fp.filteredIdx) {
		entry := fp.entries[fp.filteredIdx[fp.cursor]]
		if entry.IsDir() {
			// Navigate directly into the directory (like Enter does)
			newPath := filepath.Join(fp.currentPath, entry.Name())
			fp.currentPath = newPath
			fp.input.SetValue(fp.inputBasePath())
			fp.input.CursorEnd()
			fp.markNeedsLoad()
		} else {
			fp.input.SetValue(entry.Name())
			fp.applyFilter()
		}
		return nil
	}
	// Fallback: try to navigate from typed path
	return fp.handleOpenFromInput()
}
