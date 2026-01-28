package common

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

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

	dialogX, dialogY, _, _ := fp.dialogBounds(fp.lastContentHeight)
	_, _, contentOffsetX, contentOffsetY := fp.dialogFrame()
	clickX := dialogX + contentOffsetX + hit.X + 1
	clickY := dialogY + contentOffsetY + hit.Y

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

	dialogX, dialogY, _, _ := fp.dialogBounds(fp.lastContentHeight)
	_, _, contentOffsetX, contentOffsetY := fp.dialogFrame()
	clickX := dialogX + contentOffsetX + hit.X + 1
	clickY := dialogY + contentOffsetY + hit.Y

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

	dialogX, dialogY, _, _ := fp.dialogBounds(fp.lastContentHeight)
	_, _, contentOffsetX, contentOffsetY := fp.dialogFrame()
	clickX := dialogX + contentOffsetX + hit.X + 1
	clickY := dialogY + contentOffsetY + hit.Y

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

	dialogX, dialogY, _, _ := fp.dialogBounds(fp.lastContentHeight)
	_, _, contentOffsetX, contentOffsetY := fp.dialogFrame()
	clickX := dialogX + contentOffsetX + hit.X + 1
	clickY := dialogY + contentOffsetY + hit.Y

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

	dialogX, dialogY, _, _ := fp.dialogBounds(fp.lastContentHeight)
	_, _, contentOffsetX, contentOffsetY := fp.dialogFrame()
	clickX := dialogX + contentOffsetX + hit.X + 1
	clickY := dialogY + contentOffsetY + hit.Y

	_, cmd := fp.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
	if cmd == nil {
		t.Fatalf("expected command from button click with multiple long directories")
	}
}
