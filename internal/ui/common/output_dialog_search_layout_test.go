package common

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Unicode-fold and narrow-layout coverage for the OutputDialog search
// machinery — the main behavior tests live in output_dialog_search_test.go.

// Case-insensitive matching happens on the folded text, but the recorded
// spans index original runes — a lowercase mapping whose BYTE length differs
// from its source rune's must never slice the original at a folded offset.
// U+023A Ⱥ is two UTF-8 bytes and lowercases to three-byte U+2C65 ⱥ (folded
// offsets run ahead of the original's — a match's folded end can exceed the
// line's byte length entirely); U+212A K is three bytes and lowercases to
// one-byte k.
func TestOutputDialogSearchUnicodeByteLengthChanges(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		query string
		want  []outputDialogMatch
	}{
		{"expanding at line start", "Ⱥa line", "ⱥ", []outputDialogMatch{{line: 0, startRune: 0, endRune: 1}}},
		{"expanding after ascii prefix", "qȺa", "ⱥ", []outputDialogMatch{{line: 0, startRune: 1, endRune: 2}}},
		{"expanding match at line end", "xȺ", "xⱥ", []outputDialogMatch{{line: 0, startRune: 0, endRune: 2}}},
		{"shrinking at line start", "Ka", "k", []outputDialogMatch{{line: 0, startRune: 0, endRune: 1}}},
		{"shrinking after ascii prefix", "aKb", "k", []outputDialogMatch{{line: 0, startRune: 1, endRune: 2}}},
		{"expand then shrink in one match", "aȺK", "ⱥk", []outputDialogMatch{{line: 0, startRune: 1, endRune: 3}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := searchableDialog(tc.line + "\n")
			d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
			typeQuery(d, tc.query)
			if len(d.matches) != len(tc.want) {
				t.Fatalf("matches = %v, want %v", d.matches, tc.want)
			}
			for i, m := range d.matches {
				if m != tc.want[i] {
					t.Fatalf("match %d = %+v, want %+v", i, m, tc.want[i])
				}
			}
			d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if d.matchIdx != 0 {
				t.Fatalf("selected match idx = %d, want 0", d.matchIdx)
			}
			if v := d.View(); v == "" {
				t.Fatal("View() rendered nothing for an accepted match")
			}
		})
	}
}

// The editing footer's echo must be truncated by display cells, not runes:
// a rune-count slice against a cell budget can start at a negative index.
func TestOutputDialogSearchWideQueryEditingFooter(t *testing.T) {
	hint := "  esc done · enter accept"
	hintW := lipgloss.Width(hint)
	cases := []struct {
		name  string
		query string
		w     int
	}{
		// Four CJK runes are eight cells; with a nine-cell echo budget the
		// six-rune echo is sliced at a negative index by rune-count logic.
		{"cjk echo over cell budget", strings.Repeat("界", 4), hintW + 2 + 9},
		{"emoji query", "🔍🔍abc", hintW + 2 + 5},
		{"combining marks and zwj", "e\u0301\u0301👨\u200d👩\u200d👧", hintW + 2 + 4},
		{"zero width footer", "abc", 0},
		{"one cell footer", "abc", 1},
		{"two cell footer", "abc", 2},
		{"three cell footer", "abcdef", 3},
		{"hint alone fills width", "abc", hintW},
		{"hint plus one cell", "abc", hintW + 1},
		{"one cell echo budget", "abcdef", hintW + 2 + 1},
		{"ascii query wide", "needle", hintW + 2 + 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := searchableDialog("needle line\n")
			d.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
			typeQuery(d, tc.query)
			if !d.editing {
				t.Fatal("footer case must stay in editing mode")
			}
			footer := d.renderFooter(tc.w, 1, lipgloss.NewStyle())
			if tc.w >= 0 {
				if got := ansi.StringWidth(ansi.Strip(footer)); got > tc.w {
					t.Fatalf("footer width %d exceeds budget %d:\n%q", got, tc.w, footer)
				}
			}
			if v := d.View(); v == "" {
				t.Fatal("View() rendered nothing while editing")
			}
		})
	}
}
