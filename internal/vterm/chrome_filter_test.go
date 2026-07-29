package vterm

import (
	"strings"
	"testing"
)

// chromeLine builds a width-sized row from text, blank-padded on the right.
func chromeLine(text string, width int) []Cell {
	line := MakeBlankLine(width)
	i := 0
	for _, r := range text {
		if i >= width {
			break
		}
		line[i] = Cell{Rune: r, Width: 1}
		i++
	}
	return line
}

func chromeFrameText(frame [][]Cell) []string {
	out := make([]string, 0, len(frame))
	for _, row := range frame {
		var b strings.Builder
		for _, c := range row {
			if c.Rune == 0 {
				b.WriteRune(' ')
				continue
			}
			b.WriteRune(c.Rune)
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}

func TestIsRuleRow(t *testing.T) {
	const width = 20
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"full width rule", strings.Repeat("─", width), true},
		{"inset rule", strings.Repeat("─", 14), true},
		{"rule too short", strings.Repeat("─", 5), false},
		{"prose", "The medieval period", false},
		{"blank", "", false},
		{"mixed glyphs", strings.Repeat("─", 10) + strings.Repeat("━", 8), false},
		{"repeated letter is not a rule", strings.Repeat("x", width), false},
		{"repeated digit is not a rule", strings.Repeat("1", width), false},
		{"ascii dashes are a rule", strings.Repeat("-", width), true},
		{"equals separator is a rule", strings.Repeat("=", width), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRuleRow(chromeLine(tc.text, width), width); got != tc.want {
				t.Fatalf("isRuleRow(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestTrimCapturedChromeDropsPromptBox(t *testing.T) {
	const width = 20
	rule := strings.Repeat("─", width)
	frame := [][]Cell{
		chromeLine("first paragraph", width),
		chromeLine("second line", width),
		chromeLine("", width),
		chromeLine(rule, width),
		chromeLine("❯ ", width),
		chromeLine(rule, width),
		chromeLine("  plan mode on", width),
		chromeLine("", width),
	}
	got := chromeFrameText(trimCapturedChrome(frame, width))
	want := []string{"first paragraph", "second line", "", rule, "❯"}
	if len(got) != len(want) {
		t.Fatalf("trimCapturedChrome returned %d rows (%q), want %d (%q)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTrimCapturedChromeKeepsFrameWithoutRule(t *testing.T) {
	const width = 20
	frame := [][]Cell{
		chromeLine("only prose here", width),
		chromeLine("and more prose", width),
	}
	if got := trimCapturedChrome(frame, width); len(got) != len(frame) {
		t.Fatalf("frame without a rule was trimmed to %d rows, want %d", len(got), len(frame))
	}
}

func TestTrimCapturedChromeIgnoresDistantRule(t *testing.T) {
	const width = 20
	frame := [][]Cell{chromeLine(strings.Repeat("─", width), width)}
	for i := 0; i < chromeSearchDepth+2; i++ {
		frame = append(frame, chromeLine("prose line", width))
	}
	if got := trimCapturedChrome(frame, width); len(got) != len(frame) {
		t.Fatalf("rule above the search depth trimmed the frame to %d rows, want %d", len(got), len(frame))
	}
}

// A normal scrollback-backed program must keep every scrolled line, rules
// included: the trim is only for synthesized alt-screen history.
func TestScrollUpKeepsRulesOnNormalScreen(t *testing.T) {
	const width = 20
	vt := New(width, 3)
	rule := strings.Repeat("─", width) + "\r\n"
	vt.Write([]byte("prose\r\n" + rule + "❯ prompt\r\nmore\r\n"))

	var sawRule bool
	for _, row := range vt.Scrollback {
		if isRuleRow(row, width) {
			sawRule = true
		}
	}
	if !sawRule {
		t.Fatalf("normal-screen scrollback dropped a rule row; scrollback=%q", chromeFrameText(vt.Scrollback))
	}
}

// Synthesized alt-screen history drops the trailing prompt box.
func TestScrollUpTrimsChromeInSynthesizedHistory(t *testing.T) {
	const width = 20
	vt := New(width, 6)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))
	if !vt.AltScreen {
		t.Fatal("expected alt screen")
	}
	rule := strings.Repeat("─", width)
	vt.Write([]byte("prose one\r\nprose two\r\n" + rule + "\r\n❯ \r\n" + rule + "\r\n"))
	// Force a scroll so the frame is displaced into synthesized history.
	vt.Write([]byte("\r\n\r\n"))

	for _, row := range vt.Scrollback {
		if isRuleRow(row, width) {
			t.Fatalf("synthesized history kept a rule row; scrollback=%q", chromeFrameText(vt.Scrollback))
		}
	}
}

// A partial scroll displaces rows from the middle of the transcript, where a
// trailing rule is content. Only a whole-region scroll may be trimmed.
func TestScrollUpPartialScrollKeepsEverything(t *testing.T) {
	const width = 20
	vt := New(width, 6)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))
	rule := strings.Repeat("\u2500", width)
	// Fill the screen so the rule is mid-transcript, then push it off the top
	// one line at a time — each write scrolls a single row, never the region.
	vt.Write([]byte("a\r\nb\r\nc\r\n" + rule + "\r\nd\r\ne\r\n"))
	for _, line := range []string{"f", "g", "h", "i", "j"} {
		vt.Write([]byte(line + "\r\n"))
	}

	var sawRule bool
	for _, row := range vt.Scrollback {
		if isRuleRow(row, width) {
			sawRule = true
		}
	}
	if !sawRule {
		t.Fatalf("a one-line scroll dropped a rule that was transcript; scrollback=%q", chromeFrameText(vt.Scrollback))
	}
}
