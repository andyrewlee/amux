package common

import (
	"testing"

	"github.com/andyrewlee/amux/internal/messages"
)

func TestHandleSelectTheme(t *testing.T) {
	d := NewSettingsDialog(themeAt(t, 0), "", "", "")
	if len(d.themes) < 2 {
		t.Skip("need at least two themes")
	}
	d.focusedItem = settingsItemTheme
	d.themeCursor = 1

	_, cmd := d.handleSelect()
	// Selecting commits the cursor's theme into d.theme.
	if d.theme != d.themes[1].ID {
		t.Fatalf("theme after select = %v, want %v", d.theme, d.themes[1].ID)
	}
	preview, ok := cmd().(ThemePreview)
	if !ok {
		t.Fatalf("expected ThemePreview, got %T", cmd())
	}
	if preview.Theme != d.themes[1].ID {
		t.Fatalf("preview.Theme = %v, want %v", preview.Theme, d.themes[1].ID)
	}
}

func TestHandleSelectThemeCursorOutOfRangeStillPreviews(t *testing.T) {
	d := NewSettingsDialog(themeAt(t, 0), "", "", "")
	d.focusedItem = settingsItemTheme
	d.themeCursor = len(d.themes) // out of range
	before := d.theme

	_, cmd := d.handleSelect()
	// Out-of-range cursor leaves d.theme untouched but still emits a preview.
	if d.theme != before {
		t.Fatalf("theme should be unchanged on out-of-range cursor, got %v", d.theme)
	}
	if _, ok := cmd().(ThemePreview); !ok {
		t.Fatalf("expected ThemePreview, got %T", cmd())
	}
}

func TestHandleSelectUpdateAvailableTriggersUpgrade(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.SetUpdateInfo("v1", "v2", true)
	d.Show()
	d.focusedItem = settingsItemUpdate

	_, cmd := d.handleSelect()
	if d.Visible() {
		t.Fatal("selecting available update should hide the dialog")
	}
	if cmd == nil {
		t.Fatal("expected a TriggerUpgrade command")
	}
	if _, ok := cmd().(messages.TriggerUpgrade); !ok {
		t.Fatalf("expected TriggerUpgrade, got %T", cmd())
	}
}

func TestHandleSelectUpdateUnavailableIsNoop(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.SetUpdateInfo("v1", "", false)
	d.Show()
	d.focusedItem = settingsItemUpdate

	_, cmd := d.handleSelect()
	if !d.Visible() {
		t.Fatal("selecting an unavailable update should not close the dialog")
	}
	if cmd != nil {
		t.Fatal("unavailable update should produce no command")
	}
}

func TestHandleSelectClose(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.Show()
	d.focusedItem = settingsItemClose

	_, cmd := d.handleSelect()
	if d.Visible() {
		t.Fatal("selecting Close should hide the dialog")
	}
	result, ok := cmd().(SettingsResult)
	if !ok {
		t.Fatalf("expected SettingsResult, got %T", cmd())
	}
	if result.Canceled {
		t.Fatal("Close SettingsResult should not be marked canceled")
	}
}

func TestHandleNextSectionSkipsUpdateWhenUnavailable(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.updateAvailable = false
	d.focusedItem = settingsItemTmuxSync

	_, _ = d.handleNextSection()
	// TmuxSync -> (skip Update) -> Close
	if d.focusedItem != settingsItemClose {
		t.Fatalf("focusedItem = %d, want settingsItemClose", d.focusedItem)
	}
}

func TestHandleNextSectionVisitsUpdateWhenAvailable(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.updateAvailable = true
	d.focusedItem = settingsItemTmuxSync

	_, _ = d.handleNextSection()
	if d.focusedItem != settingsItemUpdate {
		t.Fatalf("focusedItem = %d, want settingsItemUpdate", d.focusedItem)
	}
}

func TestHandleNextSectionWrapsFromClose(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.focusedItem = settingsItemClose

	_, _ = d.handleNextSection()
	if d.focusedItem != settingsItemTheme {
		t.Fatalf("focusedItem = %d, want settingsItemTheme (wrap)", d.focusedItem)
	}
}

func TestHandlePrevSectionSkipsUpdateWhenUnavailable(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.updateAvailable = false
	d.focusedItem = settingsItemClose

	_, _ = d.handlePrevSection()
	// Close -> Update(skip) -> TmuxSync
	if d.focusedItem != settingsItemTmuxSync {
		t.Fatalf("focusedItem = %d, want settingsItemTmuxSync", d.focusedItem)
	}
}

func TestHandlePrevSectionVisitsUpdateWhenAvailable(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.updateAvailable = true
	d.focusedItem = settingsItemClose

	_, _ = d.handlePrevSection()
	if d.focusedItem != settingsItemUpdate {
		t.Fatalf("focusedItem = %d, want settingsItemUpdate", d.focusedItem)
	}
}

func TestHandlePrevSectionWrapsFromTheme(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.focusedItem = settingsItemTheme

	_, _ = d.handlePrevSection()
	if d.focusedItem != settingsItemClose {
		t.Fatalf("focusedItem = %d, want settingsItemClose (wrap)", d.focusedItem)
	}
}

func TestHandleNextInThemeSectionCycles(t *testing.T) {
	d := NewSettingsDialog(themeAt(t, 0), "", "", "")
	d.focusedItem = settingsItemTheme
	d.themeCursor = len(d.themes) - 1 // at the end

	_, cmd := d.handleNext()
	// Wraps back to 0 and updates theme.
	if d.themeCursor != 0 {
		t.Fatalf("themeCursor = %d, want 0 (wrap)", d.themeCursor)
	}
	if d.theme != d.themes[0].ID {
		t.Fatalf("theme = %v, want %v", d.theme, d.themes[0].ID)
	}
	if _, ok := cmd().(ThemePreview); !ok {
		t.Fatalf("expected ThemePreview, got %T", cmd())
	}
}

func TestHandleNextOutsideThemeDelegatesToSection(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.focusedItem = settingsItemClose

	_, cmd := d.handleNext()
	// Delegates to handleNextSection, which wraps Close -> Theme and returns nil.
	if d.focusedItem != settingsItemTheme {
		t.Fatalf("focusedItem = %d, want settingsItemTheme", d.focusedItem)
	}
	if cmd != nil {
		t.Fatal("section move should not emit a command")
	}
}

func TestHandlePrevInThemeSectionCyclesBackward(t *testing.T) {
	d := NewSettingsDialog(themeAt(t, 0), "", "", "")
	d.focusedItem = settingsItemTheme
	d.themeCursor = 0 // at the start

	_, cmd := d.handlePrev()
	// Wraps to last theme.
	if d.themeCursor != len(d.themes)-1 {
		t.Fatalf("themeCursor = %d, want %d (wrap)", d.themeCursor, len(d.themes)-1)
	}
	if d.theme != d.themes[len(d.themes)-1].ID {
		t.Fatalf("theme = %v, want last theme", d.theme)
	}
	if _, ok := cmd().(ThemePreview); !ok {
		t.Fatalf("expected ThemePreview, got %T", cmd())
	}
}

func TestHandlePrevOutsideThemeDelegatesToSection(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.focusedItem = settingsItemClose

	_, cmd := d.handlePrev()
	// Delegates to handlePrevSection: Close -> (skip Update) -> TmuxSync.
	if d.focusedItem != settingsItemTmuxSync {
		t.Fatalf("focusedItem = %d, want settingsItemTmuxSync", d.focusedItem)
	}
	if cmd != nil {
		t.Fatal("section move should not emit a command")
	}
}

func TestHandleNextThemeRoundTrip(t *testing.T) {
	d := NewSettingsDialog(themeAt(t, 0), "", "", "")
	d.focusedItem = settingsItemTheme

	n := len(d.themes)
	// Cycling forward exactly n times returns to the starting theme.
	for i := 0; i < n; i++ {
		_, _ = d.handleNext()
	}
	if d.themeCursor != 0 {
		t.Fatalf("after %d forward cycles, cursor = %d, want 0", n, d.themeCursor)
	}
	if d.theme != d.themes[0].ID {
		t.Fatalf("after round trip theme = %v, want %v", d.theme, d.themes[0].ID)
	}
}
