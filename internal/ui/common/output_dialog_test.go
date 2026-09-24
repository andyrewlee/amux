package common

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestOutputDialog_ShowScrollClose(t *testing.T) {
	d := NewOutputDialog("Run output — ws", strings.Repeat("line\n", 100))
	d.SetSize(100, 20)
	d.Show()
	if !d.Visible() {
		t.Fatal("expected visible after Show")
	}
	if !strings.Contains(d.View(), "Run output — ws") {
		t.Fatal("view missing title")
	}
	// Page down moves the viewport.
	d.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if d.offset == 0 {
		t.Fatal("pgdown did not scroll")
	}
	// Esc closes and emits the result.
	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if d.Visible() {
		t.Fatal("expected hidden after esc")
	}
	if cmd == nil {
		t.Fatal("expected a result cmd on close")
	}
	if _, ok := cmd().(OutputDialogResult); !ok {
		t.Fatalf("expected OutputDialogResult, got %T", cmd())
	}
}

func TestOutputDialog_EmptyContent(t *testing.T) {
	d := NewOutputDialog("out", "")
	d.SetSize(80, 20)
	d.Show()
	if !strings.Contains(d.View(), "(no output)") {
		t.Fatal("expected a placeholder for empty content")
	}
}

func TestOutputDialog_NonKeyMsgIgnored(t *testing.T) {
	d := NewOutputDialog("out", "x")
	d.Show()
	got, cmd := d.Update(struct{ s string }{"not a key"})
	if cmd != nil || got != d {
		t.Fatal("non-key message should be ignored")
	}
}

// TestOutputDialog_FollowPinsToBottomOnRefresh covers the core follow-mode
// contract: `f` engages it, and every SetContent while engaged re-pins the
// viewport to the new tail.
func TestOutputDialog_FollowPinsToBottomOnRefresh(t *testing.T) {
	var content strings.Builder
	for i := range 50 {
		content.WriteString("line-" + string(rune('a'+i%26)) + "\n")
	}
	d := NewOutputDialog("out", content.String())
	d.SetSize(80, 12) // viewCap < len(lines) so there's somewhere to scroll
	d.Show()

	// Scroll away from the bottom, then engage follow — it snaps to bottom.
	d.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	if d.offset != 0 {
		t.Fatalf("g did not jump to top (offset=%d)", d.offset)
	}
	d.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if !d.Following() {
		t.Fatal("f did not engage follow mode")
	}
	if d.offset == 0 {
		t.Fatal("engaging follow did not snap to the bottom")
	}
	bottom := d.offset

	// A refresh with new lines keeps the view pinned at the (new) bottom.
	d.SetContent(content.String() + "new-tail-line\n")
	if !d.Following() {
		t.Fatal("refresh disengaged follow")
	}
	if d.offset <= bottom {
		t.Fatalf("followed refresh did not advance the pinned offset (was %d, now %d)", bottom, d.offset)
	}
	view := d.View()
	if !strings.Contains(view, "new-tail-line") {
		t.Fatalf("followed view missing the newest line, got:\n%s", view)
	}
	if !strings.Contains(view, "[following]") {
		t.Fatal("followed view missing the [following] indicator")
	}
}

// TestOutputDialog_ManualScrollDisengagesFollow pins the tail -f contract:
// scrolling back means reading — any scroll key drops follow, and a
// subsequent refresh updates content WITHOUT yanking the viewport.
func TestOutputDialog_ManualScrollDisengagesFollow(t *testing.T) {
	var content strings.Builder
	for range 50 {
		content.WriteString("line\n")
	}
	d := NewOutputDialog("out", content.String())
	d.SetSize(80, 12)
	d.Show()
	d.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if !d.Following() {
		t.Fatal("setup: f did not engage follow")
	}

	d.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if d.Following() {
		t.Fatal("manual scroll did not disengage follow")
	}
	here := d.offset
	d.SetContent(content.String() + "more\nmore\nmore\n")
	if d.offset != here {
		t.Fatalf("refresh while scrolled moved the viewport (%d -> %d); content must update, offset must not", here, d.offset)
	}

	// G (jump to bottom) doubles as follow-resume.
	d.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	if !d.Following() {
		t.Fatal("G did not re-engage follow")
	}
	d.SetContent(content.String() + "even-more\n")
	if d.offset == here {
		t.Fatal("re-engaged follow did not re-pin to bottom on refresh")
	}
}

// TestOutputDialog_FooterAdvertisesFollow keeps the discoverable hint.
func TestOutputDialog_FooterAdvertisesFollow(t *testing.T) {
	d := NewOutputDialog("out", "x")
	d.SetSize(80, 12)
	d.Show()
	if view := d.View(); !strings.Contains(view, "f follow") {
		t.Fatalf("footer missing the 'f follow' hint, got:\n%s", view)
	}
}

// TestOutputDialog_SanitizesContent proves subprocess output cannot carry
// escapes or concealed text into the rendered frame — content is sanitized
// at ingestion (SetContent and the constructor share the path).
func TestOutputDialog_SanitizesContent(t *testing.T) {
	poisoned := "ok\n\x1b[8msecret\x1b[0m\nspoof\x1b[2Jed line\n"
	d := NewOutputDialog("out", poisoned)
	if len(d.lines) != 3 {
		t.Fatalf("expected caller's 3 lines preserved, got %d: %q", len(d.lines), d.lines)
	}
	for i, line := range d.lines {
		if strings.Contains(line, "\x1b") {
			t.Fatalf("line %d carried a raw escape: %q", i, line)
		}
	}
	// Conceal markers are stripped — the text itself stays visible.
	if !strings.Contains(d.lines[1], "secret") {
		t.Fatalf("conceal-stripped text missing, got %q", d.lines[1])
	}

	// The same guarantees hold for the follow-mode refresh path.
	d.SetContent("fresh\x1b[31m line\x1b[0m")
	if strings.Contains(d.lines[0], "\x1b") || !strings.Contains(d.lines[0], "fresh line") {
		t.Fatalf("SetContent left unsanitized line: %q", d.lines[0])
	}
}

// TestOutputDialog_SanitizesTitle covers the title draw sink.
func TestOutputDialog_SanitizesTitle(t *testing.T) {
	d := NewOutputDialog("Run output — \x1b[8mws\x1b[0m", "x")
	d.SetSize(80, 12)
	d.Show()
	for _, line := range strings.Split(d.View(), "\n") {
		if strings.Contains(line, "\x1b[8m") {
			t.Fatalf("title escape reached the frame: %q", line)
		}
	}
	if !strings.Contains(d.View(), "ws") {
		t.Fatal("sanitized title text missing")
	}
}
