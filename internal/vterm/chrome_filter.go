package vterm

import "unicode"

// Synthesized alt-screen history and the chrome problem.
//
// A chat agent that lives on the alternate screen (Claude Code) never scrolls
// and never clears its transcript: it repaints the whole screen in place. It
// therefore leaves no real scrollback, so amux synthesizes history from whole
// captured frames (see SCROLLING.md and captureScreenToScrollback).
//
// Every one of those frames carries the agent's chrome — the prompt box, its
// horizontal rules, and the mode/status footer beneath them. Capturing frames
// verbatim bakes a copy of that chrome into the transcript each time, so
// scrolling back reads as prose interrupted by stale prompt boxes and
// full-width rules starting in the left gutter.
//
// A prompt box is anchored by a full-width horizontal rule, and a rule is
// unambiguous: no line of prose is one box-drawing glyph repeated across most
// of the terminal. Everything from the last such rule to the end of the frame
// is chrome, and dropping it leaves the transcript. An agent that draws no rule
// keeps its frame captured verbatim, exactly as before.

const (
	// chromeRuleMinCoverage is the fraction of the terminal width a single
	// repeated glyph must span to count as a rule. Claude Code's rules span the
	// full width; the margin absorbs agents that inset theirs.
	chromeRuleMinCoverage = 0.6

	// chromeSearchDepth bounds how far above the last content row a rule may
	// sit and still anchor the trailing chrome. A prompt box plus its footer is
	// a handful of rows; searching further risks cutting real transcript.
	chromeSearchDepth = 8
)

// trimCapturedChrome returns the prefix of a captured frame that is transcript,
// dropping the agent's trailing chrome block and any blank padding beneath it.
// Frames with no trailing rule are returned unchanged.
func trimCapturedChrome(lines [][]Cell, width int) [][]Cell {
	last := lastNonBlankRow(lines)
	if last < 0 {
		return lines
	}
	start := last - chromeSearchDepth + 1
	if start < 0 {
		start = 0
	}
	// The lowest rule anchors the chrome: a prompt box draws one above and one
	// below its input row, and the footer sits under the lower one.
	for i := last; i >= start; i-- {
		if isRuleRow(lines[i], width) {
			return lines[:i]
		}
	}
	return lines
}

// isRuleRow reports whether a row is a horizontal rule: a single punctuation
// or box-drawing glyph repeated across most of the terminal width.
//
// The glyph must not be alphanumeric. Prose and program output do produce long
// runs of one character — a row of "xxxxxxxx" or a bare "========" separator —
// and treating those as chrome would drop every line beneath them.
func isRuleRow(line []Cell, width int) bool {
	if width <= 0 || len(line) == 0 {
		return false
	}
	var glyph string
	count := 0
	for _, c := range line {
		content := c.GraphemeCluster
		if content == "" {
			if c.Rune == 0 || c.Rune == ' ' {
				continue
			}
			content = string(c.Rune)
		} else if content == " " {
			continue
		}
		if !isRuleGlyph(content) {
			return false
		}
		if glyph == "" {
			glyph = content
		} else if content != glyph {
			return false
		}
		count++
	}
	if glyph == "" {
		return false
	}
	return float64(count) >= chromeRuleMinCoverage*float64(width)
}

// isRuleGlyph reports whether a grapheme may form a horizontal rule. Letters
// and digits are excluded; everything else (box drawing, dashes, punctuation)
// qualifies.
func isRuleGlyph(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// lastNonBlankRow returns the index of the last row holding visible content, or
// -1 when every row is blank.
func lastNonBlankRow(screen [][]Cell) int {
	for i := len(screen) - 1; i >= 0; i-- {
		if !lineIsBlank(screen[i]) {
			return i
		}
	}
	return -1
}

// lineIsBlank reports whether a line holds no visible content.
func lineIsBlank(line []Cell) bool {
	for _, c := range line {
		if c.Rune != 0 && c.Rune != ' ' {
			return false
		}
		if c.GraphemeCluster != "" && c.GraphemeCluster != " " {
			return false
		}
	}
	return true
}
