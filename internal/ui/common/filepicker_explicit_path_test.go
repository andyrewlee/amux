package common

import (
	"os"
	"path/filepath"
	"testing"
)

// explicitPathFixture builds a picker over a populated directory whose first
// row ("aaa") intentionally differs from the typed target, so a test fails
// whenever row selection wins over an explicit path.
func explicitPathFixture(t *testing.T, directoriesOnly bool) (*FilePicker, string) {
	t.Helper()
	base := t.TempDir()
	mkdirAll(t, filepath.Join(base, "aaa"))
	mkdirAll(t, filepath.Join(base, "zzz"))
	fp := NewFilePicker("id", base, directoriesOnly)
	fp.Show()
	pumpPicker(fp)
	if len(fp.entries) != 2 || fp.entries[0].Name() != "aaa" {
		t.Fatalf("expected populated listing starting at aaa, got %v", fp.entries)
	}
	return fp, base
}

// typeValue simulates textinput replacement: set the value and re-run the
// filter exactly like the Update loop does after each keystroke, so the
// populated filteredIdx (not a cleared one) is what Enter sees.
func typeValue(fp *FilePicker, value string) {
	fp.input.SetValue(value)
	fp.input.CursorEnd()
	fp.applyFilter()
}

// pumpEnter runs Enter's async resolve round-trip and returns the resulting
// DialogResult when the picker confirmed something. Row selection emits the
// result directly; a resolved path emits it from the follow-up Update cmd.
func pumpEnter(t *testing.T, fp *FilePicker) (DialogResult, bool) {
	t.Helper()
	_, cmd := fp.handleEnter()
	if cmd == nil {
		return DialogResult{}, false
	}
	for _, msg := range pumpMsgs(cmd) {
		if dr, ok := msg.(DialogResult); ok {
			return dr, true
		}
		_, next := fp.Update(msg)
		for _, result := range pumpMsgs(next) {
			if dr, ok := result.(DialogResult); ok {
				return dr, true
			}
		}
	}
	pumpPicker(fp)
	return DialogResult{}, false
}

func TestFilePickerExplicitPathEnterPrefersTypedPath(t *testing.T) {
	t.Run("absolute directory confirms over highlighted row", func(t *testing.T) {
		fp, base := explicitPathFixture(t, true)
		target := t.TempDir()
		typeValue(fp, target)

		result, ok := pumpEnter(t, fp)
		if !ok || !result.Confirmed || result.Value != target {
			t.Fatalf("Enter result = %+v ok=%v, want confirmed %q", result, ok, target)
		}
		if fp.currentPath != base {
			t.Fatalf("currentPath = %q, want unchanged %q", fp.currentPath, base)
		}
	})

	t.Run("absolute file confirms in file-capable picker", func(t *testing.T) {
		fp, _ := explicitPathFixture(t, false)
		external := t.TempDir()
		target := filepath.Join(external, "note.txt")
		writeFile(t, target)
		typeValue(fp, target)

		result, ok := pumpEnter(t, fp)
		if !ok || !result.Confirmed || result.Value != target {
			t.Fatalf("Enter result = %+v ok=%v, want confirmed %q", result, ok, target)
		}
	})

	t.Run("absolute file is rejected by directories-only picker", func(t *testing.T) {
		fp, base := explicitPathFixture(t, true)
		external := t.TempDir()
		target := filepath.Join(external, "note.txt")
		writeFile(t, target)
		typeValue(fp, target)

		result, ok := pumpEnter(t, fp)
		if ok {
			t.Fatalf("expected no confirmation for a file in directories-only mode, got %+v", result)
		}
		if fp.currentPath != base || fp.input.Value() != target || !fp.visible {
			t.Fatalf("state changed: path=%q input=%q visible=%v", fp.currentPath, fp.input.Value(), fp.visible)
		}
	})

	t.Run("nested relative path resolves against current directory", func(t *testing.T) {
		fp, base := explicitPathFixture(t, true)
		deep := filepath.Join(base, "aaa", "deep")
		mkdirAll(t, deep)
		typeValue(fp, filepath.Join("aaa", "deep"))

		result, ok := pumpEnter(t, fp)
		if !ok || !result.Confirmed || result.Value != deep {
			t.Fatalf("Enter result = %+v ok=%v, want confirmed %q", result, ok, deep)
		}
	})

	t.Run("dot-relative path is explicit", func(t *testing.T) {
		fp, base := explicitPathFixture(t, true)
		target := filepath.Join(base, "zzz")
		typeValue(fp, "."+string(os.PathSeparator)+"zzz")

		result, ok := pumpEnter(t, fp)
		if !ok || !result.Confirmed || result.Value != target {
			t.Fatalf("Enter result = %+v ok=%v, want confirmed %q", result, ok, target)
		}
	})

	t.Run("nonexistent explicit path changes nothing", func(t *testing.T) {
		fp, base := explicitPathFixture(t, true)
		missing := filepath.Join(t.TempDir(), "gone")
		typeValue(fp, missing)
		cursorBefore := fp.cursor

		if _, ok := pumpEnter(t, fp); ok {
			t.Fatalf("expected no confirmation for %q", missing)
		}
		if fp.currentPath != base || fp.input.Value() != missing || !fp.visible || fp.cursor != cursorBefore {
			t.Fatalf("state changed: path=%q input=%q visible=%v cursor=%d",
				fp.currentPath, fp.input.Value(), fp.visible, fp.cursor)
		}
	})
}

func TestFilePickerExplicitPathAutocompletePrefersTypedPath(t *testing.T) {
	t.Run("tab navigates absolute directory instead of row", func(t *testing.T) {
		fp, base := explicitPathFixture(t, true)
		target := t.TempDir()
		typeValue(fp, target)

		pumpPicker(fp, fp.handleAutocomplete())
		if fp.currentPath != target {
			t.Fatalf("currentPath = %q, want navigated to %q", fp.currentPath, target)
		}
		// Tab navigates; it must not confirm.
		if !fp.visible {
			t.Fatalf("picker closed after Tab navigation")
		}
		if fp.currentPath == base {
			t.Fatalf("unchanged")
		}
	})

	t.Run("tab on nonexistent explicit path leaves rows untouched", func(t *testing.T) {
		fp, base := explicitPathFixture(t, true)
		missing := filepath.Join(t.TempDir(), "gone")
		typeValue(fp, missing)

		pumpPicker(fp, fp.handleAutocomplete())
		if fp.currentPath != base {
			t.Fatalf("currentPath = %q, want unchanged %q", fp.currentPath, base)
		}
	})
}

func TestFilePickerFuzzyInputStillSelectsRows(t *testing.T) {
	t.Run("bare name filters and opens the row", func(t *testing.T) {
		fp, base := explicitPathFixture(t, true)
		typeValue(fp, "zzz")

		result, ok := pumpEnter(t, fp)
		if ok {
			t.Fatalf("expected navigation not confirmation, got %+v", result)
		}
		want := filepath.Join(base, "zzz")
		if fp.currentPath != want {
			t.Fatalf("currentPath = %q, want %q", fp.currentPath, want)
		}
	})

	t.Run("one-component suffix of the base path stays fuzzy", func(t *testing.T) {
		fp, base := explicitPathFixture(t, true)
		typeValue(fp, fp.inputBasePath()+"zzz")

		result, ok := pumpEnter(t, fp)
		if ok {
			t.Fatalf("expected navigation not confirmation, got %+v", result)
		}
		want := filepath.Join(base, "zzz")
		if fp.currentPath != want {
			t.Fatalf("currentPath = %q, want %q", fp.currentPath, want)
		}
	})

	t.Run("leading dot in a simple name stays fuzzy", func(t *testing.T) {
		base := t.TempDir()
		mkdirAll(t, filepath.Join(base, "aaa"))
		writeFile(t, filepath.Join(base, ".env"))
		fp := NewFilePicker("id", base, false)
		fp.showHidden = true
		fp.Show()
		pumpPicker(fp)
		typeValue(fp, ".env")

		result, ok := pumpEnter(t, fp)
		if !ok || !result.Confirmed {
			t.Fatalf("expected .env to confirm as a file, got %+v ok=%v", result, ok)
		}
		if result.Value != filepath.Join(base, ".env") {
			t.Fatalf("confirmed %q, want %q", result.Value, filepath.Join(base, ".env"))
		}
	})
}
