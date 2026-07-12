package vterm

import (
	"testing"

	"github.com/clipperhouse/displaywidth"
	"github.com/mattn/go-runewidth"
)

// TestRunewidthDisplaywidthDivergenceProbe is a report-only canary. The vterm
// emulator sizes cells with mattn/go-runewidth (see putChar), while the
// lipgloss/ultraviolet render stack composed around it measures with
// clipperhouse/displaywidth. The two carry independent width tables that must
// agree glyph-for-glyph or the rendered terminal drifts out of alignment with
// the surrounding UI. This test never fails on a mismatch: it forces
// EastAsianWidth=false on both tables (mirroring internal/e2e/main_test.go),
// walks the practically reachable code points, and logs every divergence plus a
// running total. Re-run and compare the total when bumping runewidth,
// displaywidth, ultraviolet, or Go's Unicode tables.
func TestRunewidthDisplaywidthDivergenceProbe(t *testing.T) {
	// Force deterministic widths across locales, then restore prior state so
	// sibling vterm tests observe the package defaults they were written for.
	prevRunewidth := runewidth.EastAsianWidth
	prevCondition := runewidth.DefaultCondition.EastAsianWidth
	prevDisplaywidth := displaywidth.DefaultOptions.EastAsianWidth
	runewidth.EastAsianWidth = false
	runewidth.DefaultCondition.EastAsianWidth = false
	runewidth.CreateLUT()
	displaywidth.DefaultOptions.EastAsianWidth = false
	t.Cleanup(func() {
		runewidth.EastAsianWidth = prevRunewidth
		runewidth.DefaultCondition.EastAsianWidth = prevCondition
		runewidth.CreateLUT()
		displaywidth.DefaultOptions.EastAsianWidth = prevDisplaywidth
	})

	divergent := 0

	// Single runes: runewidth's per-rune width against displaywidth's.
	checkRune := func(r rune) {
		rw := runewidth.RuneWidth(r)
		dw := displaywidth.Rune(r)
		if rw != dw {
			divergent++
			t.Logf("rune U+%04X %q: runewidth=%d displaywidth=%d", r, string(r), rw, dw)
		}
	}

	// ASCII printable.
	for r := rune(0x20); r <= 0x7E; r++ {
		checkRune(r)
	}
	// Emoji blocks.
	for r := rune(0x1F300); r <= 0x1F6FF; r++ {
		checkRune(r)
	}
	for r := rune(0x1F900); r <= 0x1F9FF; r++ {
		checkRune(r)
	}
	// Ambiguous-width symbol samples.
	for _, r := range []rune{'§', '±', '×', '÷', '°', '¿'} {
		checkRune(r)
	}
	// Greek and Cyrillic letter ranges (frequently ambiguous width).
	for r := rune('α'); r <= 'ω'; r++ {
		checkRune(r)
	}
	for r := rune('А'); r <= 'я'; r++ {
		checkRune(r)
	}
	// Box-drawing block.
	for r := rune(0x2500); r <= 0x257F; r++ {
		checkRune(r)
	}

	// Multi-rune grapheme clusters: displaywidth measures the whole cluster,
	// while putChar sums runewidth over the cluster's runes (combining and
	// zero-width joiners report width 0 and attach to the base cell). Compare
	// those two effective widths.
	// Spelled with explicit escapes (VS16 U+FE0F and ZWJ U+200D are invisible).
	clusters := []struct {
		name  string
		value string
	}{
		{name: "heart + VS16", value: "\u2764\uFE0F"},
		{name: "sun + VS16", value: "\u2600\uFE0F"},
		{name: "woman technologist (ZWJ)", value: "\U0001F469\u200D\U0001F4BB"},
		{name: "family MWG (ZWJ)", value: "\U0001F468\u200D\U0001F469\u200D\U0001F467"},
	}
	for _, c := range clusters {
		dw := displaywidth.String(c.value)
		rw := 0
		for _, r := range c.value {
			rw += runewidth.RuneWidth(r)
		}
		if rw != dw {
			divergent++
			t.Logf("cluster %s %q (% X): summed-runewidth=%d displaywidth=%d",
				c.name, c.value, []byte(c.value), rw, dw)
		}
	}

	t.Logf("total divergent: %d", divergent)
}
