package common

import (
	"testing"
)

// LiteralSearch returns rune spans into the ORIGINAL text while matching on
// its folded form — the coordinate contract both search engines highlight
// in. Length-changing case mappings (Ⱥ→ⱥ expands two bytes to three,
// K→k shrinks three to one) must map through folded rune boundaries, never
// slice the original at a folded byte offset.
func TestLiteralSearchRuneSpans(t *testing.T) {
	cases := []struct {
		name  string
		query string
		text  string
		want  []RuneSpan
	}{
		{"empty query", "", "anything", nil},
		{"absent query", "zzz", "alpha beta", nil},
		{"query longer than text", "needle", "nee", nil},
		{"ascii single", "needle", "a needle here", []RuneSpan{{Start: 2, End: 8}}},
		{"ascii case-insensitive", "NEEDLE", "a Needle here", []RuneSpan{{Start: 2, End: 8}}},
		{"ascii repeated non-overlapping", "aa", "aa aa aaa", []RuneSpan{{Start: 0, End: 2}, {Start: 3, End: 5}, {Start: 6, End: 8}}},
		{"ascii match to end", "end", "the end", []RuneSpan{{Start: 4, End: 7}}},
		{"expanding at start", "ⱥ", "Ⱥa", []RuneSpan{{Start: 0, End: 1}}},
		{"expanding after prefix", "ⱥ", "qȺa", []RuneSpan{{Start: 1, End: 2}}},
		{"expanding match at end", "xⱥ", "xȺ", []RuneSpan{{Start: 0, End: 2}}},
		{"expanding query folds too", "XȺ", "xȺ", []RuneSpan{{Start: 0, End: 2}}},
		{"shrinking at start", "k", "Ka", []RuneSpan{{Start: 0, End: 1}}},
		{"shrinking after prefix", "k", "aKb", []RuneSpan{{Start: 1, End: 2}}},
		{"expand then shrink in one match", "ⱥk", "aȺK", []RuneSpan{{Start: 1, End: 3}}},
		{"unicode prefix before match", "hit", "Ünïcödé hit ✓", []RuneSpan{{Start: 8, End: 11}}},
		{"repeated expanding matches", "ⱥ", "Ⱥ x Ⱥ", []RuneSpan{{Start: 0, End: 1}, {Start: 4, End: 5}}},
		{"combining marks", "é", "cafe\u0301 x", []RuneSpan{}},
		{"combining sequence literal", "e\u0301", "cafe\u0301 x", []RuneSpan{{Start: 3, End: 5}}},
		{"empty text", "a", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewLiteralSearch(tc.query).RuneSpans(tc.text)
			if len(got) != len(tc.want) {
				t.Fatalf("RuneSpans(%q in %q) = %v, want %v", tc.query, tc.text, got, tc.want)
			}
			for i, span := range got {
				if span != tc.want[i] {
					t.Fatalf("span %d = %+v, want %+v", i, span, tc.want[i])
				}
				// The span must slice valid original runes — the property
				// the folded-offset bug broke.
				rs := []rune(tc.text)
				if span.Start < 0 || span.End > len(rs) || span.Start >= span.End {
					t.Fatalf("span %+v is not a valid rune range in %q", span, tc.text)
				}
			}
		})
	}
}
