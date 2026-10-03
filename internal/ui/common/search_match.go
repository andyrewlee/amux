package common

import (
	"strings"
	"unicode/utf8"
)

// RuneSpan is a half-open match range [Start, End) as rune indices into a
// line's original (unfolded) text — the coordinate search highlights in.
type RuneSpan struct {
	Start, End int
}

// LiteralSearch is a prepared literal, case-insensitive substring search:
// the query is folded once up front, and RuneSpans finds its
// non-overlapping occurrences in each candidate line. Folding uses the same
// simple strings.ToLower semantics as the loops this replaces — no regex,
// normalization, or multi-character case folding.
type LiteralSearch struct {
	folded string
}

// NewLiteralSearch folds query once for repeated RuneSpans calls.
func NewLiteralSearch(query string) LiteralSearch {
	return LiteralSearch{folded: strings.ToLower(query)}
}

// RuneSpans returns the query's non-overlapping matches in text, as rune
// spans into text itself. Matching runs on the folded text, but a folded
// byte offset is never used to index the original: rune-for-rune folding
// can change a rune's BYTE length (two-byte U+023A Ⱥ lowercases to
// three-byte U+2C65 ⱥ, three-byte U+212A K to one-byte k), which would
// slice the original out of bounds or highlight the wrong runes.
func (s LiteralSearch) RuneSpans(text string) []RuneSpan {
	if s.folded == "" {
		return nil
	}
	folded := strings.ToLower(text)
	if isASCII(text) {
		// One byte is one rune: folded byte offsets already are original
		// rune indices, so ordinary logs pay no position-map allocation.
		var spans []RuneSpan
		for off := 0; off+len(s.folded) <= len(folded); {
			i := strings.Index(folded[off:], s.folded)
			if i < 0 {
				break
			}
			start := off + i
			spans = append(spans, RuneSpan{Start: start, End: start + len(s.folded)})
			off = start + len(s.folded) // non-overlapping matches only
		}
		return spans
	}

	// Folding preserves rune count, so each folded rune boundary maps to the
	// same rune index in the original. Record one entry per boundary — not
	// per byte — then translate match offsets through it instead of
	// rescanning the prefix for every match.
	bounds := make(map[int]int, utf8.RuneCountInString(folded)+1)
	runeIdx := 0
	for byteOff := range folded {
		bounds[byteOff] = runeIdx
		runeIdx++
	}
	bounds[len(folded)] = runeIdx
	runeAt := func(off int) int {
		if r, ok := bounds[off]; ok {
			return r
		}
		// Match offsets land on folded rune boundaries for valid UTF-8;
		// count folded runes defensively rather than ever indexing the
		// original at a folded byte position.
		return utf8.RuneCountInString(folded[:off])
	}

	var spans []RuneSpan
	for off := 0; off+len(s.folded) <= len(folded); {
		i := strings.Index(folded[off:], s.folded)
		if i < 0 {
			break
		}
		byteStart := off + i
		byteEnd := byteStart + len(s.folded)
		spans = append(spans, RuneSpan{Start: runeAt(byteStart), End: runeAt(byteEnd)})
		off = byteEnd // non-overlapping matches only
	}
	return spans
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
