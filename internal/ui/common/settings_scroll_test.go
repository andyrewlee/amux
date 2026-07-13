package common

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestSettingsRenderHeaderFooterInvariant guards the structural assumption
// composeVisibleLines relies on: renderLines() always starts with exactly
// settingsHeaderLines fixed lines ("Settings" + a blank) and always ends
// with exactly settingsFooterLines fixed lines (a blank + "[Close]"),
// whether or not the optional update hint/affordance are present.
func TestSettingsRenderHeaderFooterInvariant(t *testing.T) {
	assertShape := func(t *testing.T, lines []string) {
		t.Helper()
		if len(lines) < settingsHeaderLines+settingsFooterLines {
			t.Fatalf("renderLines() returned %d lines, want at least %d", len(lines), settingsHeaderLines+settingsFooterLines)
		}
		if !strings.Contains(lines[0], "Settings") {
			t.Errorf("line 0 = %q, want the Settings title", lines[0])
		}
		if lines[1] != "" {
			t.Errorf("line 1 = %q, want a blank header separator", lines[1])
		}
		last := lines[len(lines)-1]
		if !strings.Contains(last, "[Close]") {
			t.Errorf("last line = %q, want [Close]", last)
		}
		if lines[len(lines)-2] != "" {
			t.Errorf("second-to-last line = %q, want a blank footer separator", lines[len(lines)-2])
		}
	}

	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	assertShape(t, d.renderLines())

	// The update affordance adds a line to the body; the header/footer shape
	// must not move.
	d.SetUpdateInfo("v1", "v2", true)
	assertShape(t, d.renderLines())

	// The update hint also adds a body line.
	d.SetUpdateInfo("v1", "", false)
	d.SetUpdateHint("Installed via Homebrew - update with brew upgrade amux")
	assertShape(t, d.renderLines())
}

// TestSettingsViewClampsBodyToHeight confirms View() never renders taller
// than the dialog's assigned height once the body no longer fits, and that
// [Close] plus the bottom border are still the final two rendered lines.
func TestSettingsViewClampsBodyToHeight(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	const height = 15 // short enough that the 19-theme body must scroll
	d.SetSize(120, height)
	d.Show()

	lines := d.composeVisibleLines()
	_, frameY, _, _ := d.dialogFrame()
	total := len(lines) + frameY
	if total > height {
		t.Fatalf("composed dialog height = %d, want <= assigned height %d", total, height)
	}

	if !strings.Contains(lines[len(lines)-1], "[Close]") {
		t.Fatalf("expected last composed line to be [Close], got %q", lines[len(lines)-1])
	}

	view := d.View()
	if !strings.Contains(view, "[Close]") {
		t.Fatal("expected [Close] to be rendered by View()")
	}
	if !strings.Contains(view, "╰") {
		t.Fatal("expected the dialog's bottom border to be rendered by View()")
	}
}

// TestSettingsViewScrollsThemeCursorIntoView drives themeCursor to the last
// theme (far past a short dialog's visible window) and confirms View()
// scrolls the body so that theme's name is visible, while [Close] remains
// visible throughout.
func TestSettingsViewScrollsThemeCursorIntoView(t *testing.T) {
	d := NewSettingsDialog(themeAt(t, 0), "", "", "")
	if len(d.themes) < 8 {
		t.Skip("need enough themes to force scrolling at a short height")
	}
	d.SetSize(120, 15)
	d.Show()
	d.focusedItem = settingsItemTheme

	// Wrap backward from the first theme to the last: far below a 15-row
	// dialog's visible window.
	_, _ = d.handlePrev()

	view := d.View()
	lastTheme := d.themes[len(d.themes)-1].Name
	if !strings.Contains(view, lastTheme) {
		t.Fatalf("expected focused theme %q to be scrolled into view, got:\n%s", lastTheme, view)
	}
	if !strings.Contains(view, "[Close]") {
		t.Fatal("expected [Close] to remain visible while the body is scrolled")
	}
}

// TestSettingsScrollOffsetAdvancesAndRetreatsWithNavigation exercises the
// up/down theme navigation (handleNext/handlePrev) at a height too short to
// show every theme, confirming scrollOffset advances as focus moves past the
// visible window and retreats back to 0 once focus returns to the top.
func TestSettingsScrollOffsetAdvancesAndRetreatsWithNavigation(t *testing.T) {
	d := NewSettingsDialog(themeAt(t, 0), "", "", "")
	if len(d.themes) < 8 {
		t.Skip("need enough themes to force scrolling at a short height")
	}
	d.SetSize(120, 15)
	d.Show()

	d.View() // establish baseline: cursor 0 is within the initial window
	if d.scrollOffset != 0 {
		t.Fatalf("initial scrollOffset = %d, want 0", d.scrollOffset)
	}

	for i := 0; i < len(d.themes)-1; i++ {
		_, _ = d.handleNext()
	}
	d.View()
	if d.scrollOffset == 0 {
		t.Fatal("expected scrollOffset to advance once the focused theme scrolled past the visible window")
	}

	for i := 0; i < len(d.themes)-1; i++ {
		_, _ = d.handlePrev()
	}
	d.View()
	// Theme 0's row is body index 1 (body index 0 is the non-focusable
	// "Theme" section label), so the minimal offset that keeps it visible
	// while scrolling up from further down the list is 1, not 0.
	if d.scrollOffset != 1 {
		t.Fatalf("scrollOffset after returning to theme 0 = %d, want 1", d.scrollOffset)
	}
	if !strings.Contains(d.View(), d.themes[0].Name) {
		t.Fatalf("expected theme 0 (%q) to be visible after returning to it", d.themes[0].Name)
	}
}

// TestSettingsViewCloseAlwaysVisibleAtAnyHeight focuses Close directly
// (bypassing the navigation handlers, the way handleSelect/handleClick
// would leave it) across a range of heights and confirms [Close] and the
// dialog's bottom border are always part of the rendered output.
func TestSettingsViewCloseAlwaysVisibleAtAnyHeight(t *testing.T) {
	for _, h := range []int{9, 10, 15, 20, 24, 36, 60} {
		d := NewSettingsDialog(ThemeAyuDark, "", "", "")
		d.SetSize(120, h)
		d.Show()
		d.focusedItem = settingsItemClose

		view := d.View()
		if !strings.Contains(view, "[Close]") {
			t.Errorf("height=%d: expected [Close] visible, got:\n%s", h, view)
		}
		if !strings.Contains(view, "╰") {
			t.Errorf("height=%d: expected the bottom border rune present, got:\n%s", h, view)
		}
	}
}

// TestSettingsClickOnCloseWorksWhenBodyIsScrolled exercises handleClick
// through composeVisibleLines/remapHitRegions at a height short enough that
// the body is scrolled, confirming a click on [Close] (always the last
// composed line, in the fixed footer) still resolves correctly.
func TestSettingsClickOnCloseWorksWhenBodyIsScrolled(t *testing.T) {
	d := NewSettingsDialog(themeAt(t, 0), "", "", "")
	d.SetSize(120, 15)
	d.Show()

	lines := d.composeVisibleLines()
	contentHeight := len(lines)
	dialogX, dialogY, _, _ := d.dialogBounds(contentHeight)
	_, _, contentOffsetX, contentOffsetY := d.dialogFrame()

	closeLocalY := len(lines) - 1
	msg := tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      dialogX + contentOffsetX,
		Y:      dialogY + contentOffsetY + closeLocalY,
	}

	cmd := d.handleClick(msg)
	if d.Visible() {
		t.Fatal("clicking [Close] should hide the dialog")
	}
	if cmd == nil {
		t.Fatal("expected a SettingsResult command from clicking [Close]")
	}
}
