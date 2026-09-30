package compositor

// Width-engine conformance test.
//
// Three Unicode width engines feed this render pipeline, and they must agree
// on every glyph the app can actually emit or display, or cells misalign with
// no compiler warning. The ownership split is:
//
//   - displaywidth (clipperhouse) — terminal CELL math: rune→cell-width while
//     parsing VT output into the grid (internal/vterm) and anything that must
//     match what a real terminal advances the cursor by. Owns cell truth.
//   - ansi.StringWidth — cell width of possibly-ANSI-styled content (it strips
//     escapes): diff-viewer wrap math and styled-string measurement.
//   - lipgloss.Width — chrome layout strings: tab bars, dashboard rows,
//     borders, help lines.
//
// When adding a width-consuming call site, pick per that rule — and if the
// input is a raw codepoint destined for the terminal grid, displaywidth is the
// only correct oracle. This test pins three-way agreement over the corpus of
// reachable input so a future dependency bump that splits the engines fails
// here loudly instead of shipping misaligned borders.
//
// Corpus classes: the exact glyph set internal/ui/theme emits, box-drawing,
// accented/combining Latin, CJK/fullwidth/Hangul, emoji (single, VS16,
// keycap, flag, skin-tone, ZWJ chains), ambiguous-width symbols, and ASCII.
// At authoring time all three engines agreed on every entry — a failure here
// is a real divergence in reachable input, not a test bug.

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/clipperhouse/displaywidth"

	"github.com/andyrewlee/amux/internal/ui/theme"
)

func widthEnginesAgree(t *testing.T, s string) {
	t.Helper()
	dw := displaywidth.String(s)
	an := ansi.StringWidth(s)
	lg := lipgloss.Width(s)
	if dw != an || an != lg {
		t.Errorf("width divergence on %q: displaywidth=%d ansi.StringWidth=%d lipgloss.Width=%d", s, dw, an, lg)
	}
}

func TestWidthEnginesConformOnUICorpus(t *testing.T) {
	uiIcons := []string{
		theme.Icons.Clean, theme.Icons.Dirty, theme.Icons.Running, theme.Icons.Idle,
		theme.Icons.Add, theme.Icons.Delete, theme.Icons.Edit, theme.Icons.Close,
		theme.Icons.Cursor, theme.Icons.CursorEmpty, theme.Icons.ArrowRight, theme.Icons.ArrowDown,
		theme.Icons.Project, theme.Icons.Workspace, theme.Icons.Agent, theme.Icons.Terminal,
		theme.Icons.Folder, theme.Icons.File, theme.Icons.Git, theme.Icons.Home,
		theme.Icons.DirOpen, theme.Icons.DirClosed,
	}
	uiIcons = append(uiIcons, theme.Icons.Spinner...)

	corpus := map[string][]string{
		"ui icon set (theme.Icons)": uiIcons,
		"ascii and punctuation": {
			"a", "Z", "0", "9", "!@#$%^&*()", "hello world", "file-name_v2.go",
			"[Add project]", "(requires a selected workspace)",
		},
		"box drawing and tree glyphs": {
			"─", "│", "┌", "┐", "└", "┘", "├", "┤", "┬", "┴", "┼",
			"╭", "╮", "╰", "╯", "═", "║",
		},
		"accented and combining latin": {
			"é", "ü", "ñ", "é", "ḍ̄", "ﬁ",
		},
		"cjk, fullwidth, hangul": {
			"中", "こんにちは", "Ｗ", "한", "한글", "你好世界",
		},
		"emoji": {
			"⚠", "✗", "👍", "🚀", "👨‍💻", "🏳️‍🌈", "🧑🏿‍🤝‍🧑🏻",
			"👍🏽", "☀", "☀️", "❤", "❤️", "1️⃣", "🇺🇸",
		},
		"ambiguous-width and symbols": {
			"·", "±", "§", "€", "≠", "™", "©", "®", "ə", "١٢٣", "𝔘", "𝟘",
		},
	}

	for class, glyphs := range corpus {
		for _, g := range glyphs {
			t.Run(class+"/"+g, func(t *testing.T) { widthEnginesAgree(t, g) })
		}
	}
}
