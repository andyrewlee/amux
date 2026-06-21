package common

import (
	"os"
	"path/filepath"
	"testing"
)

// mkdirAll is a small helper that fails the test on error so the table-driven
// cases below stay terse.
func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}
}

// writeFile creates an empty regular file, failing the test on error.
func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func TestFilePickerApplyFilterClampsCursorAndScroll(t *testing.T) {
	tmp := t.TempDir()
	mkdirAll(t, filepath.Join(tmp, "alpha"))
	mkdirAll(t, filepath.Join(tmp, "beta"))
	mkdirAll(t, filepath.Join(tmp, "gamma"))

	fp := NewFilePicker("id", tmp, true)
	fp.Show()
	fp.cursor = len(fp.filteredIdx) - 1
	fp.scrollOffset = len(fp.filteredIdx) - 1
	fp.input.SetValue("alpha")

	fp.applyFilter()

	if len(fp.filteredIdx) != 1 {
		t.Fatalf("filteredIdx length = %d, want 1", len(fp.filteredIdx))
	}
	if fp.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 after filtering to one item", fp.cursor)
	}
	if fp.scrollOffset != 0 {
		t.Fatalf("scrollOffset = %d, want 0 after cursor clamp", fp.scrollOffset)
	}
}

func TestFilePickerHandleOpenFromInput(t *testing.T) {
	t.Run("empty input is a no-op", func(t *testing.T) {
		tmp := t.TempDir()
		fp := NewFilePicker("id", tmp, true)
		fp.Show()
		fp.input.SetValue("")

		if fp.handleOpenFromInput() {
			t.Fatalf("expected empty input to return false")
		}
		if fp.currentPath != tmp {
			t.Fatalf("expected current path unchanged %q, got %q", tmp, fp.currentPath)
		}
	})

	t.Run("whitespace-only input is a no-op", func(t *testing.T) {
		tmp := t.TempDir()
		fp := NewFilePicker("id", tmp, true)
		fp.Show()
		fp.input.SetValue("   ")

		if fp.handleOpenFromInput() {
			t.Fatalf("expected whitespace-only input to return false")
		}
		if fp.currentPath != tmp {
			t.Fatalf("expected current path unchanged %q, got %q", tmp, fp.currentPath)
		}
	})

	t.Run("absolute directory path navigates and clears input", func(t *testing.T) {
		tmp := t.TempDir()
		child := filepath.Join(tmp, "child")
		mkdirAll(t, child)

		fp := NewFilePicker("id", tmp, true)
		fp.Show()
		fp.input.SetValue(child)

		if !fp.handleOpenFromInput() {
			t.Fatalf("expected absolute directory to be opened")
		}
		if fp.currentPath != child {
			t.Fatalf("expected current path %q, got %q", child, fp.currentPath)
		}
		if fp.input.Value() != "" {
			t.Fatalf("expected input cleared after navigation, got %q", fp.input.Value())
		}
	})

	t.Run("input is trimmed before navigating", func(t *testing.T) {
		tmp := t.TempDir()
		child := filepath.Join(tmp, "child")
		mkdirAll(t, child)

		fp := NewFilePicker("id", tmp, true)
		fp.Show()
		fp.input.SetValue("  " + child + "  ")

		if !fp.handleOpenFromInput() {
			t.Fatalf("expected surrounding whitespace to be trimmed and path opened")
		}
		if fp.currentPath != child {
			t.Fatalf("expected current path %q, got %q", child, fp.currentPath)
		}
	})

	t.Run("relative directory path joins current path", func(t *testing.T) {
		tmp := t.TempDir()
		child := filepath.Join(tmp, "sub")
		mkdirAll(t, child)

		fp := NewFilePicker("id", tmp, true)
		fp.Show()
		fp.input.SetValue("sub")

		if !fp.handleOpenFromInput() {
			t.Fatalf("expected relative directory to be opened")
		}
		if fp.currentPath != child {
			t.Fatalf("expected current path %q, got %q", child, fp.currentPath)
		}
		if fp.input.Value() != "" {
			t.Fatalf("expected input cleared, got %q", fp.input.Value())
		}
	})

	t.Run("home-relative path expands tilde", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("no home directory available: %v", err)
		}
		// "~" alone should resolve to the home directory itself.
		fp := NewFilePicker("id", t.TempDir(), true)
		fp.Show()
		fp.input.SetValue("~")

		if !fp.handleOpenFromInput() {
			t.Fatalf("expected ~ to open the home directory")
		}
		if fp.currentPath != home {
			t.Fatalf("expected current path %q, got %q", home, fp.currentPath)
		}
	})

	t.Run("path to a regular file is rejected", func(t *testing.T) {
		tmp := t.TempDir()
		file := filepath.Join(tmp, "note.txt")
		writeFile(t, file)

		fp := NewFilePicker("id", tmp, true)
		fp.Show()
		fp.input.SetValue(file)

		if fp.handleOpenFromInput() {
			t.Fatalf("expected a regular file path to return false")
		}
		if fp.currentPath != tmp {
			t.Fatalf("expected current path unchanged %q, got %q", tmp, fp.currentPath)
		}
		// Input must be preserved when navigation does not happen.
		if fp.input.Value() != file {
			t.Fatalf("expected input preserved %q, got %q", file, fp.input.Value())
		}
	})

	t.Run("nonexistent path is rejected", func(t *testing.T) {
		tmp := t.TempDir()
		missing := filepath.Join(tmp, "does-not-exist")

		fp := NewFilePicker("id", tmp, true)
		fp.Show()
		fp.input.SetValue(missing)

		if fp.handleOpenFromInput() {
			t.Fatalf("expected nonexistent path to return false")
		}
		if fp.currentPath != tmp {
			t.Fatalf("expected current path unchanged %q, got %q", tmp, fp.currentPath)
		}
	})
}

func TestFilePickerHandleAutocomplete(t *testing.T) {
	t.Run("navigates into selected directory entry", func(t *testing.T) {
		tmp := t.TempDir()
		mkdirAll(t, filepath.Join(tmp, "alpha"))

		fp := NewFilePicker("id", tmp, true)
		fp.Show()
		// Filtered list has exactly one entry pointing at "alpha"; cursor is 0.
		if len(fp.filteredIdx) != 1 {
			t.Fatalf("expected one entry, got %d", len(fp.filteredIdx))
		}
		fp.cursor = 0

		fp.handleAutocomplete()

		want := filepath.Join(tmp, "alpha")
		if fp.currentPath != want {
			t.Fatalf("expected current path %q, got %q", want, fp.currentPath)
		}
		// After navigating, input is reset to the new base path.
		if fp.input.Value() != fp.inputBasePath() {
			t.Fatalf("expected input base path %q, got %q", fp.inputBasePath(), fp.input.Value())
		}
	})

	t.Run("fills input with selected file name and does not navigate", func(t *testing.T) {
		tmp := t.TempDir()
		writeFile(t, filepath.Join(tmp, "report.txt"))

		// directoriesOnly=false so files appear in the entry list.
		fp := NewFilePicker("id", tmp, false)
		fp.Show()
		if len(fp.filteredIdx) != 1 {
			t.Fatalf("expected one entry, got %d", len(fp.filteredIdx))
		}
		fp.cursor = 0

		fp.handleAutocomplete()

		if fp.currentPath != tmp {
			t.Fatalf("expected current path unchanged %q, got %q", tmp, fp.currentPath)
		}
		if fp.input.Value() != "report.txt" {
			t.Fatalf("expected input %q, got %q", "report.txt", fp.input.Value())
		}
	})

	t.Run("selecting a file re-filters to that file", func(t *testing.T) {
		tmp := t.TempDir()
		writeFile(t, filepath.Join(tmp, "apple.txt"))
		writeFile(t, filepath.Join(tmp, "banana.txt"))

		fp := NewFilePicker("id", tmp, false)
		fp.Show()
		if len(fp.entries) != 2 {
			t.Fatalf("expected two entries, got %d", len(fp.entries))
		}
		// Point the cursor at "apple.txt" regardless of ordering.
		for i, idx := range fp.filteredIdx {
			if fp.entries[idx].Name() == "apple.txt" {
				fp.cursor = i
				break
			}
		}

		fp.handleAutocomplete()

		if fp.input.Value() != "apple.txt" {
			t.Fatalf("expected input %q, got %q", "apple.txt", fp.input.Value())
		}
		// applyFilter ran with "apple.txt": only that file should remain.
		if len(fp.filteredIdx) != 1 {
			t.Fatalf("expected filter to narrow to one entry, got %d", len(fp.filteredIdx))
		}
		if got := fp.entries[fp.filteredIdx[0]].Name(); got != "apple.txt" {
			t.Fatalf("expected filtered entry %q, got %q", "apple.txt", got)
		}
	})

	t.Run("falls back to typed path when no entry is selected", func(t *testing.T) {
		tmp := t.TempDir()
		child := filepath.Join(tmp, "target")
		mkdirAll(t, child)

		fp := NewFilePicker("id", tmp, true)
		fp.Show()
		// No selectable entry: empty filtered list and out-of-range cursor.
		fp.filteredIdx = nil
		fp.cursor = -1
		fp.input.SetValue(child)

		fp.handleAutocomplete()

		if fp.currentPath != child {
			t.Fatalf("expected fallback navigation to %q, got %q", child, fp.currentPath)
		}
		if fp.input.Value() != "" {
			t.Fatalf("expected input cleared by fallback, got %q", fp.input.Value())
		}
	})

	t.Run("fallback with unusable input is a safe no-op", func(t *testing.T) {
		tmp := t.TempDir()
		fp := NewFilePicker("id", tmp, true)
		fp.Show()
		fp.filteredIdx = nil
		fp.cursor = -1
		fp.input.SetValue("")

		// Must not panic and must not navigate anywhere.
		fp.handleAutocomplete()

		if fp.currentPath != tmp {
			t.Fatalf("expected current path unchanged %q, got %q", tmp, fp.currentPath)
		}
	})

	t.Run("cursor past end of filtered list falls back to typed path", func(t *testing.T) {
		tmp := t.TempDir()
		child := filepath.Join(tmp, "deep")
		mkdirAll(t, child)

		fp := NewFilePicker("id", tmp, true)
		fp.Show()
		// One real entry exists, but the cursor points beyond it, so the
		// selection branch is skipped and the fallback path is taken.
		fp.cursor = len(fp.filteredIdx)
		fp.input.SetValue(child)

		fp.handleAutocomplete()

		if fp.currentPath != child {
			t.Fatalf("expected fallback navigation to %q, got %q", child, fp.currentPath)
		}
	})
}
