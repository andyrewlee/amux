package common

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// themeAt returns the ID of the theme at index i in AvailableThemes, used to
// build expectations without hard-coding a specific theme order.
func themeAt(t *testing.T, i int) ThemeID {
	t.Helper()
	themes := AvailableThemes()
	if i < 0 || i >= len(themes) {
		t.Fatalf("theme index %d out of range (have %d themes)", i, len(themes))
	}
	return themes[i].ID
}

func TestNewSettingsDialogPlacesCursorOnCurrentTheme(t *testing.T) {
	themes := AvailableThemes()
	if len(themes) < 2 {
		t.Skip("need at least two themes for a meaningful cursor test")
	}

	// Pick the second theme so cursor != 0 proves the lookup ran.
	want := themes[1].ID
	d := NewSettingsDialog(want, "", "", "")

	if d.theme != want {
		t.Fatalf("theme = %v, want %v", d.theme, want)
	}
	if d.themeCursor != 1 {
		t.Fatalf("themeCursor = %d, want 1", d.themeCursor)
	}
	if d.focusedItem != settingsItemTheme {
		t.Fatalf("focusedItem = %d, want settingsItemTheme", d.focusedItem)
	}
	if len(d.themes) != len(themes) {
		t.Fatalf("themes len = %d, want %d", len(d.themes), len(themes))
	}
}

func TestNewSettingsDialogUnknownThemeDefaultsCursorToZero(t *testing.T) {
	// ThemeID("") is not a real theme, so the cursor lookup never matches.
	d := NewSettingsDialog(ThemeID("does-not-exist"), "", "", "")
	if d.themeCursor != 0 {
		t.Fatalf("themeCursor = %d, want 0 for unknown theme", d.themeCursor)
	}
	if d.theme != ThemeID("does-not-exist") {
		t.Fatalf("theme = %v, want the unknown id preserved", d.theme)
	}
}

func TestShowHideVisible(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	if d.Visible() {
		t.Fatal("new dialog should be hidden")
	}

	d.Show()
	if !d.Visible() {
		t.Fatal("Show should make the dialog visible")
	}

	d.Hide()
	if d.Visible() {
		t.Fatal("Hide should make the dialog invisible")
	}

	// Show is idempotent.
	d.Show()
	d.Show()
	if !d.Visible() {
		t.Fatal("repeated Show should leave dialog visible")
	}
}

func TestSetSize(t *testing.T) {
	tests := []struct {
		name string
		w, h int
	}{
		{"zero", 0, 0},
		{"typical", 80, 24},
		{"negative", -5, -10},
		{"large", 1000, 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewSettingsDialog(ThemeAyuDark, "", "", "")
			d.SetSize(tt.w, tt.h)
			if d.width != tt.w || d.height != tt.h {
				t.Fatalf("SetSize(%d,%d) => width=%d height=%d", tt.w, tt.h, d.width, d.height)
			}
		})
	}
}

func TestCursorAlwaysNil(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	if d.Cursor() != nil {
		t.Fatal("Cursor should always return nil")
	}
	d.Show()
	if d.Cursor() != nil {
		t.Fatal("Cursor should return nil even when visible")
	}
}

func TestSetSession(t *testing.T) {
	tests := []int{0, 1, 7, -3}
	for _, sess := range tests {
		d := NewSettingsDialog(ThemeAyuDark, "", "", "")
		d.SetSession(sess)
		if d.session != sess {
			t.Fatalf("SetSession(%d) => session=%d", sess, d.session)
		}
	}
}

func TestSetSessionFlowsIntoThemePreview(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.SetSession(42)
	d.Show()

	// Selecting the theme section emits a ThemePreview carrying the session.
	_, cmd := d.handleSelect()
	if cmd == nil {
		t.Fatal("expected a command from selecting the theme item")
	}
	preview, ok := cmd().(ThemePreview)
	if !ok {
		t.Fatalf("expected ThemePreview, got %T", cmd())
	}
	if preview.Session != 42 {
		t.Fatalf("preview.Session = %d, want 42", preview.Session)
	}
}

func TestSelectedThemeReturnsCurrent(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	if d.SelectedTheme() != ThemeAyuDark {
		t.Fatalf("SelectedTheme = %v, want %v", d.SelectedTheme(), ThemeAyuDark)
	}
}

func TestSetSelectedThemeKnownThemeMovesCursor(t *testing.T) {
	themes := AvailableThemes()
	if len(themes) < 2 {
		t.Skip("need at least two themes")
	}
	d := NewSettingsDialog(themes[0].ID, "", "", "")

	want := themes[1].ID
	d.SetSelectedTheme(want)
	if d.SelectedTheme() != want {
		t.Fatalf("SelectedTheme = %v, want %v", d.SelectedTheme(), want)
	}
	if d.themeCursor != 1 {
		t.Fatalf("themeCursor = %d, want 1", d.themeCursor)
	}
}

func TestSetSelectedThemeUnknownThemeLeavesCursor(t *testing.T) {
	d := NewSettingsDialog(themeAt(t, 0), "", "", "")
	// Move the cursor somewhere distinct first.
	if len(d.themes) > 1 {
		d.themeCursor = 1
	}
	before := d.themeCursor

	d.SetSelectedTheme(ThemeID("nope"))
	// theme is still updated to the requested value...
	if d.SelectedTheme() != ThemeID("nope") {
		t.Fatalf("SelectedTheme = %v, want the unknown id stored", d.SelectedTheme())
	}
	// ...but the cursor stays put because nothing matched.
	if d.themeCursor != before {
		t.Fatalf("themeCursor = %d, want unchanged %d", d.themeCursor, before)
	}
}

func TestUpdateWhenHiddenIsNoop(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	// Not shown: every key is a no-op.
	got, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got != d {
		t.Fatal("Update should return the same dialog pointer")
	}
	if cmd != nil {
		t.Fatal("hidden dialog should ignore input and return nil cmd")
	}
}

func TestUpdateEscClosesDialog(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.Show()

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if d.Visible() {
		t.Fatal("esc should hide the dialog")
	}
	if cmd == nil {
		t.Fatal("esc should emit a SettingsResult command")
	}
	result, ok := cmd().(SettingsResult)
	if !ok {
		t.Fatalf("expected SettingsResult, got %T", cmd())
	}
	if !result.Canceled {
		t.Fatal("esc SettingsResult should be marked canceled")
	}
}

func TestUpdateEnterSelectsTheme(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.Show()

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on theme item should emit ThemePreview")
	}
	if _, ok := cmd().(ThemePreview); !ok {
		t.Fatalf("expected ThemePreview, got %T", cmd())
	}
}

func TestUpdateTabAdvancesSection(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.Show()
	// From Theme, one Tab advances to the first tmux field.
	_, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if d.focusedItem != settingsItemTmuxServer {
		t.Fatalf("focusedItem after Tab = %d, want settingsItemTmuxServer", d.focusedItem)
	}
}

func TestUpdateShiftTabRetreatsSection(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.Show()
	// From theme, Shift+Tab wraps backwards to Close.
	_, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if d.focusedItem != settingsItemClose {
		t.Fatalf("focusedItem after Shift+Tab = %d, want settingsItemClose", d.focusedItem)
	}
}

func TestUpdateDownCyclesTheme(t *testing.T) {
	d := NewSettingsDialog(themeAt(t, 0), "", "", "")
	d.Show()

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if d.themeCursor != 1%len(d.themes) {
		t.Fatalf("themeCursor after down = %d, want %d", d.themeCursor, 1%len(d.themes))
	}
	if cmd == nil {
		t.Fatal("down within theme section should emit ThemePreview")
	}
	if _, ok := cmd().(ThemePreview); !ok {
		t.Fatalf("expected ThemePreview, got %T", cmd())
	}
}

func TestUpdateJKeyCyclesTheme(t *testing.T) {
	d := NewSettingsDialog(themeAt(t, 0), "", "", "")
	d.Show()

	_, cmd := d.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if d.themeCursor != 1%len(d.themes) {
		t.Fatalf("themeCursor after 'j' = %d, want %d", d.themeCursor, 1%len(d.themes))
	}
	if cmd == nil {
		t.Fatal("'j' within theme section should emit ThemePreview")
	}
}

func TestUpdateUpCyclesThemeBackwards(t *testing.T) {
	d := NewSettingsDialog(themeAt(t, 0), "", "", "")
	d.Show()
	// From cursor 0, up wraps to the last theme.
	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if d.themeCursor != len(d.themes)-1 {
		t.Fatalf("themeCursor after up = %d, want %d", d.themeCursor, len(d.themes)-1)
	}
	if cmd == nil {
		t.Fatal("up within theme section should emit ThemePreview")
	}
}

func TestUpdateKKeyCyclesThemeBackwards(t *testing.T) {
	d := NewSettingsDialog(themeAt(t, 0), "", "", "")
	d.Show()
	_, _ = d.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if d.themeCursor != len(d.themes)-1 {
		t.Fatalf("themeCursor after 'k' = %d, want %d", d.themeCursor, len(d.themes)-1)
	}
}

func TestUpdateUnhandledKeyReturnsNil(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.Show()
	before := d.focusedItem
	_, cmd := d.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if cmd != nil {
		t.Fatal("unhandled key should return nil cmd")
	}
	if d.focusedItem != before {
		t.Fatal("unhandled key should not move focus")
	}
}

func TestSettingsDialogEditsTmuxFields(t *testing.T) {
	typeInto := func(d *SettingsDialog, s string) {
		for _, r := range s {
			d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}

	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.Show()

	// Tab from Theme focuses each tmux field in turn; typing edits the focused
	// one. Characters like the letters here must not be swallowed as list motions.
	if _, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyTab}); d.focusedItem != settingsItemTmuxServer {
		t.Fatalf("focus after Tab = %d, want settingsItemTmuxServer", d.focusedItem)
	}
	typeInto(d, "amux-srv")

	if _, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyTab}); d.focusedItem != settingsItemTmuxConfig {
		t.Fatalf("focus after 2nd Tab = %d, want settingsItemTmuxConfig", d.focusedItem)
	}
	typeInto(d, "/tmp/tmux.conf")

	if _, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyTab}); d.focusedItem != settingsItemTmuxSync {
		t.Fatalf("focus after 3rd Tab = %d, want settingsItemTmuxSync", d.focusedItem)
	}
	// The sync field only accepts Go-duration characters, so 'x' is dropped.
	typeInto(d, "5x00ms")

	if got := d.TmuxServer(); got != "amux-srv" {
		t.Errorf("TmuxServer() = %q, want %q", got, "amux-srv")
	}
	if got := d.TmuxConfigPath(); got != "/tmp/tmux.conf" {
		t.Errorf("TmuxConfigPath() = %q, want %q", got, "/tmp/tmux.conf")
	}
	if got := d.TmuxSyncInterval(); got != "500ms" {
		t.Errorf("TmuxSyncInterval() = %q, want %q (invalid chars filtered)", got, "500ms")
	}

	// Backspace deletes the last rune of the focused field (sync interval).
	if _, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyBackspace}); d.TmuxSyncInterval() != "500m" {
		t.Errorf("after backspace TmuxSyncInterval() = %q, want %q", d.TmuxSyncInterval(), "500m")
	}
}
