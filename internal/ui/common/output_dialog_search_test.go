package common

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Search tests cover the OutputDialog query field per the spike contract:
// `/` edits a literal case-insensitive query over the retained sanitized
// snapshot, `enter` accepts + jumps to the first match, `n`/`N` cycle with
// wrap feedback, `esc` exits edit mode before it can close the dialog, and
// edit mode consumes ALL input including paste. Only free-form flavors opt
// in via SetSearchable — workspace status stays non-searchable.

func searchableDialog(content string) *OutputDialog {
	d := NewOutputDialog("out", content)
	d.SetSearchable(true)
	d.SetSize(100, 12)
	d.Show()
	return d
}

func typeQuery(d *OutputDialog, s string) {
	for _, r := range s {
		d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// footerOf returns the view — footer assertions search it directly. Styled
// status strings render as contiguous runs, so plain Contains works on
// match counts and markers; content lines get split by the highlight style
// and must be checked separately.
func footerOf(d *OutputDialog) string { return d.View() }

func TestOutputDialog_SearchSlashOpensEdit(t *testing.T) {
	d := searchableDialog("hello\nworld\n")
	if d.Editing() {
		t.Fatal("editing before /")
	}
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !d.Editing() {
		t.Fatal("/ did not enter edit mode")
	}
	if !strings.Contains(d.View(), "/") {
		t.Fatal("editing view missing query echo")
	}
}

func TestOutputDialog_SearchSlashInertWhenNotSearchable(t *testing.T) {
	d := NewOutputDialog("status", "hello\n")
	d.SetSize(100, 12)
	d.Show()
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if d.Editing() {
		t.Fatal("/ entered edit mode on a non-searchable dialog")
	}
	if strings.Contains(d.View(), "search") {
		t.Fatalf("non-searchable footer advertises search:\n%s", d.View())
	}
}

// Command keys are literal text while editing: f/j/k/g/n/a append to the
// query instead of scrolling, following, or closing.
func TestOutputDialog_SearchEditConsumesCommandKeys(t *testing.T) {
	d := searchableDialog("alpha\nbeta\ngamma\n")
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeQuery(d, "fjkgnad")
	if d.query != "fjkgnad" {
		t.Fatalf("query = %q, want %q", d.query, "fjkgnad")
	}
	if !d.Visible() || !d.Editing() {
		t.Fatal("dialog closed or left edit mode on command chars")
	}
	if d.offset != 0 {
		t.Fatal("edit-mode keys scrolled the view")
	}
}

func TestOutputDialog_SearchAcceptJumpsAndHighlights(t *testing.T) {
	var b strings.Builder
	for range 40 {
		b.WriteString("plain line\n")
	}
	b.WriteString("first needle here\n")
	for range 40 {
		b.WriteString("plain line\n")
	}
	d := searchableDialog(b.String())
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeQuery(d, "needle")
	if _, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("edit-mode enter emitted a result cmd — accept must not close")
	}
	if d.Editing() || !d.Visible() {
		t.Fatal("accept must leave edit mode but keep the dialog open")
	}
	if len(d.matches) != 1 || d.matchIdx != 0 {
		t.Fatalf("matches=%d idx=%d, want 1/0", len(d.matches), d.matchIdx)
	}
	if !strings.Contains(d.View(), "match 1/1") {
		t.Fatalf("footer missing match count:\n%s", footerOf(d))
	}
	view := d.View()
	if !strings.Contains(view, "first ") || !strings.Contains(view, " here") {
		t.Fatalf("view after jump missing the match line (offset=%d):\n%s", d.offset, view)
	}
	// The selected match span is rendered in reverse video.
	if !strings.Contains(view, "\x1b[7m") {
		t.Fatalf("selected match not highlighted (no reverse video):\n%s", view)
	}
}

func TestOutputDialog_SearchFirstEscExitsEditSecondCloses(t *testing.T) {
	d := searchableDialog("needle\n")
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeQuery(d, "nee")
	if _, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd != nil {
		t.Fatal("edit-mode esc emitted a result cmd — it must not close")
	}
	if d.Editing() {
		t.Fatal("first esc did not leave edit mode")
	}
	if !d.Visible() {
		t.Fatal("first esc closed the dialog")
	}
	if d.query != "nee" {
		t.Fatalf("esc cleared the query, got %q", d.query)
	}
	if _, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd == nil {
		t.Fatal("second esc did not close")
	}
	if d.Visible() {
		t.Fatal("second esc did not hide the dialog")
	}
}

func TestOutputDialog_SearchBrowseEnterStillCloses(t *testing.T) {
	d := searchableDialog("needle\n")
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeQuery(d, "needle")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("browse-mode enter did not emit the close result")
	}
	if d.Visible() {
		t.Fatal("browse-mode enter did not close")
	}
}

func TestOutputDialog_SearchCycleAndWrap(t *testing.T) {
	d := searchableDialog("hit one\nfiller\nhit two\nfiller\nhit three\n")
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeQuery(d, "hit")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(d.matches) != 3 {
		t.Fatalf("want 3 matches, got %d", len(d.matches))
	}
	for _, want := range []int{1, 2, 0} {
		d.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
		if d.matchIdx != want {
			t.Fatalf("after n matchIdx=%d, want %d", d.matchIdx, want)
		}
	}
	if !strings.Contains(footerOf(d), "(wrapped)") {
		t.Fatalf("footer missing wrapped marker:\n%s", footerOf(d))
	}
	d.Update(tea.KeyPressMsg{Code: 'N', Text: "N"})
	if d.matchIdx != 2 {
		t.Fatalf("N from wrapped 0 = %d, want 2", d.matchIdx)
	}
	// A manual scroll clears the wrapped marker.
	d.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if strings.Contains(footerOf(d), "(wrapped)") {
		t.Fatal("wrapped marker survived a manual scroll")
	}
}

func TestOutputDialog_SearchNoMatch(t *testing.T) {
	d := searchableDialog("alpha\nbeta\n")
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeQuery(d, "zzz")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(d.matches) != 0 || d.matchIdx != -1 {
		t.Fatalf("matches=%v idx=%d", d.matches, d.matchIdx)
	}
	if !strings.Contains(footerOf(d), "0 matches") {
		t.Fatalf("footer missing '0 matches':\n%s", footerOf(d))
	}
	// n/N stay inert with no matches — no panic, no scroll.
	d.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	d.Update(tea.KeyPressMsg{Code: 'N', Text: "N"})
	if d.offset != 0 {
		t.Fatal("n/N moved the viewport with no matches")
	}
}

func TestOutputDialog_SearchEmptyQueryClears(t *testing.T) {
	d := searchableDialog("needle\n")
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(d.matches) != 0 || d.query != "" || d.matchIdx != -1 {
		t.Fatalf("empty query left search state: q=%q m=%d i=%d", d.query, len(d.matches), d.matchIdx)
	}
	if strings.Contains(footerOf(d), "match") {
		t.Fatalf("footer still shows match state:\n%s", footerOf(d))
	}
}

func TestOutputDialog_SearchEditKeys(t *testing.T) {
	d := searchableDialog("x\n")
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeQuery(d, "hello world")
	d.Update(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if d.query != "hello " {
		t.Fatalf("ctrl+w = %q, want %q", d.query, "hello ")
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if d.query != "hello" {
		t.Fatalf("backspace = %q, want %q", d.query, "hello")
	}
	d.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if d.query != "" {
		t.Fatalf("ctrl+u = %q, want empty", d.query)
	}
}

func TestOutputDialog_SearchDuplicateLinesDistinctMatches(t *testing.T) {
	d := searchableDialog("same\nsame\nother\nsame\n")
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeQuery(d, "same")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(d.matches) != 3 {
		t.Fatalf("want 3 distinct matches, got %d", len(d.matches))
	}
	if d.matches[0].line == d.matches[1].line {
		t.Fatal("duplicate lines collapsed into one match")
	}
	if !strings.Contains(footerOf(d), "match 1/3") {
		t.Fatalf("footer missing count for duplicates:\n%s", footerOf(d))
	}
}

func TestOutputDialog_SearchRefreshRecomputes(t *testing.T) {
	d := searchableDialog("foo\nfoo\n")
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeQuery(d, "foo")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(d.matches) != 2 {
		t.Fatalf("setup: want 2 matches, got %d", len(d.matches))
	}
	// New content brings a third instance — matches recompute live.
	d.SetContent("foo\nfoo\nfoo\n")
	if len(d.matches) != 3 {
		t.Fatalf("refresh did not recompute: %d matches", len(d.matches))
	}
	// Content with no instances clears the selection.
	d.SetContent("bar\n")
	if len(d.matches) != 0 || d.matchIdx != -1 {
		t.Fatalf("refresh to no-match content: m=%d i=%d", len(d.matches), d.matchIdx)
	}
}

// Selection repair: when a refresh evicts the selected match's line, the
// selection lands on the nearest surviving match at-or-after it.
func TestOutputDialog_SearchSelectionRepairOnEviction(t *testing.T) {
	d := searchableDialog("needle-a\nx\nneedle-b\nx\nneedle-c\n")
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeQuery(d, "needle")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.Update(tea.KeyPressMsg{Code: 'n', Text: "n"}) // select needle-b (line 2)
	if got := d.matches[d.matchIdx].line; got != 2 {
		t.Fatalf("setup: selected line %d, want 2", got)
	}
	d.SetContent("x\nneedle-b\nx\nneedle-c\n") // needle-a evicted, rest shift up
	// Prior line 2 ("needle-b") is gone; the nearest surviving match
	// at-or-after it is needle-c at the new line 3.
	if got := d.matches[d.matchIdx].line; got != 3 {
		t.Fatalf("selection did not repair to nearest surviving match (line %d)", got)
	}
	// Evict the tail too — no match at-or-after survives; selection clears.
	d.SetContent("x\nx\n")
	if d.matchIdx != -1 {
		t.Fatalf("selection survived with no matches at-or-after (idx %d)", d.matchIdx)
	}
	// When the selected line itself survives a refresh, selection is kept.
	d.SetContent("needle-z\n")
	if d.matchIdx != 0 {
		t.Fatalf("recompute after content swap: idx %d, want 0", d.matchIdx)
	}
	d.SetContent("pad\nneedle-z\n")
	if got := d.matches[d.matchIdx].line; got != 1 {
		t.Fatalf("surviving line not preserved (line %d, want 1)", got)
	}
}

// Dropped history is never searchable: the match index only ever covers the
// retained snapshot in d.lines.
func TestOutputDialog_SearchOnlyRetainedSnapshot(t *testing.T) {
	d := searchableDialog("evicted needle\nretained needle\n")
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeQuery(d, "needle")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(d.matches) != 2 {
		t.Fatalf("setup: want 2 matches, got %d", len(d.matches))
	}
	d.SetContent("retained needle\n") // first line is gone from the snapshot
	if len(d.matches) != 1 {
		t.Fatalf("evicted line still searchable: %d matches", len(d.matches))
	}
}

// A search jump is a manual scroll: follow disengages, and `f` resumes it.
func TestOutputDialog_SearchJumpPausesFollowResumesWithF(t *testing.T) {
	var b strings.Builder
	for range 50 {
		b.WriteString("plain\n")
	}
	b.WriteString("tail needle\n")
	d := searchableDialog(b.String())
	d.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if !d.Following() {
		t.Fatal("setup: follow did not engage")
	}
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeQuery(d, "plain")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.Following() {
		t.Fatal("search jump did not disengage follow")
	}
	d.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if !d.Following() {
		t.Fatal("f did not resume follow after search")
	}
	// Follow + query: footer keeps both indicators.
	if f := footerOf(d); !strings.Contains(f, "following") || !strings.Contains(f, "match") {
		t.Fatalf("following+query footer missing indicators:\n%s", f)
	}
}

// Paste while editing is query text — it is consumed by the dialog and can
// never reach the terminal (the app gates paste routing on Editing()).
func TestOutputDialog_SearchPaste(t *testing.T) {
	d := searchableDialog("needle\n")
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	d.Update(tea.PasteMsg{Content: "nee"})
	if d.query != "nee" {
		t.Fatalf("paste did not enter query: %q", d.query)
	}
	if !d.Editing() {
		t.Fatal("plain paste exited edit mode")
	}
	// Pasted newline acts as enter — paste-then-accept is one gesture.
	d.Update(tea.PasteMsg{Content: "dle\nextra"})
	if d.Editing() {
		t.Fatal("pasted newline did not accept the query")
	}
	if d.query != "needle" {
		t.Fatalf("paste-with-newline query = %q, want %q", d.query, "needle")
	}
}

// Case-insensitive matching and rune-span highlighting on non-ASCII content.
func TestOutputDialog_SearchUnicodeCaseInsensitive(t *testing.T) {
	d := searchableDialog("Ünïcödé needle ✓\nplain\n")
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeQuery(d, "NEEDLE")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(d.matches) != 1 {
		t.Fatalf("case-insensitive search found %d matches", len(d.matches))
	}
	m := d.matches[0]
	rs := []rune(d.lines[0])
	if got := string(rs[m.startRune:m.endRune]); got != "needle" {
		t.Fatalf("match span = %q, want %q", got, "needle")
	}
}

// Narrow layouts must still render — echo truncation keeps the footer sane.
func TestOutputDialog_SearchNarrowLayout(t *testing.T) {
	d := NewOutputDialog("out", "needle\n")
	d.SetSearchable(true)
	d.SetSize(60, 10) // narrowest preset
	d.Show()
	d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeQuery(d, strings.Repeat("q", 200))
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(footerOf(d), "0 matches") {
		t.Fatalf("narrow footer missing status:\n%s", d.View())
	}
}

// Close-behavior regression: esc and enter close unchanged when search is
// enabled but no query is active.
func TestOutputDialog_SearchCloseRegression(t *testing.T) {
	d := searchableDialog("x\n")
	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := cmd().(OutputDialogResult); !ok {
		t.Fatalf("enter result = %T, want OutputDialogResult", cmd())
	}
	d2 := searchableDialog("x\n")
	if _, cmd := d2.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd == nil {
		t.Fatal("esc did not close a searchable dialog")
	}
}
