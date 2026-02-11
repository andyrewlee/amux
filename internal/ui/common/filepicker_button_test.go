package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// testFilePickerScreenCoords computes screen coordinates for clicking a hit region,
// using the same positioning logic as the click handler (measure actual rendered output).
func testFilePickerScreenCoords(fp *FilePicker, hit HitRegion) (clickX, clickY int) {
	lines := fp.renderLines()
	content := strings.Join(lines, "\n")
	dialogView := fp.dialogStyle().Render(content)
	dialogW, dialogH := viewDimensions(dialogView)
	dialogX := max(0, (fp.width-dialogW)/2)
	dialogY := max(0, (fp.height-dialogH)/2)
	_, _, contentOffsetX, contentOffsetY := fp.dialogFrame()
	return dialogX + contentOffsetX + hit.X + 1, dialogY + contentOffsetY + hit.Y
}

func TestFilePickerButtonClickWithLongPath(t *testing.T) {
	// Create a deeply nested path that would exceed content width
	tmp := t.TempDir()
	deepPath := filepath.Join(tmp, "very-long-directory-name-that-is-quite-lengthy", "another-long-name", "and-more")
	if err := os.MkdirAll(deepPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fp := NewFilePicker("id", deepPath, true)
	fp.SetSize(120, 40)
	fp.Show()

	fp.renderLines()
	if len(fp.buttonHits) == 0 {
		t.Fatalf("expected button hits to be populated")
	}

	var hit HitRegion
	found := false
	for _, btn := range fp.buttonHits {
		if btn.ID == "open" {
			hit = btn
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected open button hit region")
	}

	clickX, clickY := testFilePickerScreenCoords(fp, hit)

	_, cmd := fp.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
	if cmd == nil {
		t.Fatalf("expected command from button click with long path")
	}
}

func TestFilePickerButtonClickWithLongDirectoryName(t *testing.T) {
	tmp := t.TempDir()
	// Create a directory with a very long name that would wrap
	longName := "Creative Cloud Files Personal Account user@example.com ABCD1234567890XYZ@ServiceID"
	longDir := filepath.Join(tmp, longName)
	if err := os.MkdirAll(longDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fp := NewFilePicker("id", tmp, true)
	fp.SetSize(120, 40)
	fp.Show()

	fp.renderLines()
	if len(fp.buttonHits) == 0 {
		t.Fatalf("expected button hits to be populated")
	}

	var hit HitRegion
	found := false
	for _, btn := range fp.buttonHits {
		if btn.ID == "open" {
			hit = btn
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected open button hit region")
	}

	clickX, clickY := testFilePickerScreenCoords(fp, hit)

	_, cmd := fp.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
	if cmd == nil {
		t.Fatalf("expected command from button click with long directory name in list")
	}
}

func TestFilePickerButtonClickNoSubdirectories(t *testing.T) {
	// Create a directory with only files, no subdirectories
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "file1.txt"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "file2.txt"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	fp := NewFilePicker("id", tmp, true) // directoriesOnly = true
	fp.SetSize(120, 40)
	fp.Show()

	// Should show "No subdirectories" message
	fp.renderLines()
	if len(fp.filteredIdx) != 0 {
		t.Fatalf("expected no filtered entries for directory with only files, got %d", len(fp.filteredIdx))
	}

	if len(fp.buttonHits) == 0 {
		t.Fatalf("expected button hits to be populated even with no subdirectories")
	}

	var hit HitRegion
	found := false
	for _, btn := range fp.buttonHits {
		if btn.ID == "open" {
			hit = btn
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected open button hit region")
	}

	clickX, clickY := testFilePickerScreenCoords(fp, hit)

	_, cmd := fp.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
	if cmd == nil {
		t.Fatalf("expected command from button click when no subdirectories shown")
	}

	msg := cmd()
	result, ok := msg.(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", msg)
	}
	if !result.Confirmed || result.Value != tmp {
		t.Fatalf("unexpected dialog result: confirmed=%v value=%q", result.Confirmed, result.Value)
	}
}

func TestFilePickerCancelButtonClick(t *testing.T) {
	tmp := t.TempDir()
	fp := NewFilePicker("id", tmp, true)
	fp.SetSize(120, 40)
	fp.Show()

	fp.renderLines()

	var hit HitRegion
	found := false
	for _, btn := range fp.buttonHits {
		if btn.ID == "cancel" {
			hit = btn
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected cancel button hit region")
	}

	clickX, clickY := testFilePickerScreenCoords(fp, hit)

	newPicker, cmd := fp.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
	if cmd == nil {
		t.Fatalf("expected command from cancel button click")
	}
	fp = newPicker
	if fp.visible {
		t.Fatalf("expected file picker to be hidden after cancel")
	}

	msg := cmd()
	result, ok := msg.(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", msg)
	}
	if result.Confirmed {
		t.Fatalf("expected Confirmed=false for cancel, got true")
	}
}

func TestFilePickerMultipleLongDirectories(t *testing.T) {
	tmp := t.TempDir()
	// Create multiple directories with long names
	longNames := []string{
		"First-Very-Long-Directory-Name-That-Exceeds-Normal-Width",
		"Second-Long-Directory-Name-Also-Quite-Lengthy-Indeed",
		"Third-Directory-With-Extended-Name-For-Testing-Purposes",
	}
	for _, name := range longNames {
		if err := os.MkdirAll(filepath.Join(tmp, name), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	fp := NewFilePicker("id", tmp, true)
	fp.SetSize(120, 40)
	fp.Show()

	fp.renderLines()

	// Verify all entries are truncated (each should be on one line)
	if len(fp.rowHits) != 3 {
		t.Fatalf("expected 3 row hits, got %d", len(fp.rowHits))
	}

	// Verify button is still clickable
	var hit HitRegion
	for _, btn := range fp.buttonHits {
		if btn.ID == "open" {
			hit = btn
			break
		}
	}

	clickX, clickY := testFilePickerScreenCoords(fp, hit)

	_, cmd := fp.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
	if cmd == nil {
		t.Fatalf("expected command from button click with multiple long directories")
	}
}

func TestFilePickerMultiSelectButtonClicks(t *testing.T) {
	// Simulates the edit repos overlay with multiple selected repos
	tmp := t.TempDir()
	fp := NewFilePicker("edit-repos", tmp, true)
	fp.SetMultiSelect(true)
	fp.SetPrimaryActionLabel("Add repo")
	fp.SetSize(120, 40)
	fp.Show()

	// Pre-populate with selected repos (simulating edit repos overlay)
	fp.AddSelectedPath("/Users/test/projects/frontend")
	fp.AddSelectedPath("/Users/test/projects/backend")
	fp.AddSelectedPath("/Users/test/projects/shared-libs")

	fp.renderLines()

	// Test Done button click
	t.Run("done button", func(t *testing.T) {
		var hit HitRegion
		found := false
		for _, btn := range fp.buttonHits {
			if btn.ID == "done" {
				hit = btn
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected done button hit region")
		}

		clickX, clickY := testFilePickerScreenCoords(fp, hit)
		_, cmd := fp.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
		if cmd == nil {
			t.Fatalf("expected command from done button click with %d selected repos", len(fp.selectedPaths))
		}
	})

	// Re-show for cancel test
	fp.Show()
	fp.AddSelectedPath("/Users/test/projects/frontend")
	fp.AddSelectedPath("/Users/test/projects/backend")
	fp.AddSelectedPath("/Users/test/projects/shared-libs")
	fp.renderLines()

	t.Run("cancel button", func(t *testing.T) {
		var hit HitRegion
		found := false
		for _, btn := range fp.buttonHits {
			if btn.ID == "cancel" {
				hit = btn
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected cancel button hit region")
		}

		clickX, clickY := testFilePickerScreenCoords(fp, hit)
		_, cmd := fp.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
		if cmd == nil {
			t.Fatalf("expected command from cancel button click with %d selected repos", len(fp.selectedPaths))
		}
	})

	// Re-show for add repo test
	fp.Show()
	fp.AddSelectedPath("/Users/test/projects/frontend")
	fp.AddSelectedPath("/Users/test/projects/backend")
	fp.AddSelectedPath("/Users/test/projects/shared-libs")
	fp.renderLines()

	t.Run("add repo button", func(t *testing.T) {
		var hit HitRegion
		found := false
		for _, btn := range fp.buttonHits {
			if btn.ID == "open" {
				hit = btn
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected open button hit region")
		}

		countBefore := len(fp.selectedPaths)
		clickX, clickY := testFilePickerScreenCoords(fp, hit)
		newFp, _ := fp.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
		fp = newFp
		// In multi-select, "Add repo" adds current path to selected list (no command returned)
		if len(fp.selectedPaths) != countBefore+1 {
			t.Fatalf("expected selectedPaths to grow from %d to %d, got %d",
				countBefore, countBefore+1, len(fp.selectedPaths))
		}
	})
}
