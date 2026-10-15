package diff

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/git"
)

// testDiffResult builds a loaded diff result with recognizable content.
func testDiffResult() *git.DiffResult {
	return &git.DiffResult{
		Lines: []git.DiffLine{
			{Kind: git.DiffLineHeader, Content: "@@ -1,3 +1,4 @@"},
			{Kind: git.DiffLineContext, Content: " package main"},
			{Kind: git.DiffLineDelete, Content: "-old line"},
			{Kind: git.DiffLineAdd, Content: "+new line"},
		},
		Hunks: []git.Hunk{{StartLine: 0}},
	}
}

// TestContentVersion_MutationsBump pins the dirtiness invariant the
// compose-time diff gate rests on: every input updateDiffViewer (or a
// setter) can mutate must move the fingerprint, or the gate would reuse a
// stale drawable.
func TestContentVersion_MutationsBump(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(m *Model)
	}{
		{"scroll", func(m *Model) { m.scroll = 3 }},
		{"hunkIdx", func(m *Model) { m.hunkIdx = 1 }},
		{"wrap", func(m *Model) { m.wrap = !m.wrap }},
		{"focused", func(m *Model) { m.focused = !m.focused }},
		{"width", func(m *Model) { m.width++ }},
		{"height", func(m *Model) { m.height++ }},
		{"stylesRev", func(m *Model) { m.stylesRev++ }},
		{"searching", func(m *Model) { m.searching = true }},
		{"query", func(m *Model) { m.query = "needle" }},
		{"matchIdx", func(m *Model) { m.matchIdx = 4 }},
		{"searchWrapped", func(m *Model) { m.searchWrapped = true }},
		{"matchCount", func(m *Model) { m.matches = append(m.matches, diffMatch{lineIdx: 0}) }},
		{"loading", func(m *Model) { m.loading = !m.loading }},
		{"errStr", func(m *Model) { m.err = errors.New("boom") }},
		{"loadID", func(m *Model) { m.loadID++ }},
		{"change path", func(m *Model) { m.change = &git.Change{Path: "other.go"} }},
		{"change kind", func(m *Model) { m.change = &git.Change{Path: "main.go", Kind: git.ChangeDeleted} }},
		{"mode", func(m *Model) { m.mode = git.DiffModeBranch }},
	}
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			m := New(nil, &git.Change{Path: "main.go", Kind: git.ChangeModified}, git.DiffModeUnstaged, 80, 24)
			m.loading = false
			m.diff = testDiffResult()
			before := m.ContentVersion()
			tt.mutate(m)
			if got := m.ContentVersion(); got == before {
				t.Fatalf("%s did not move ContentVersion (%d -> %d)", tt.name, before, got)
			}
		})
	}
}

// TestContentVersion_StableInputsStayClean asserts the cheap side: identical
// state fingerprints identically, so a stable diff hits the gate.
func TestContentVersion_StableInputsStayClean(t *testing.T) {
	m := New(nil, &git.Change{Path: "main.go", Kind: git.ChangeModified}, git.DiffModeUnstaged, 80, 24)
	m.loading = false
	m.diff = testDiffResult()
	if got := m.ContentVersion(); got != m.ContentVersion() {
		t.Fatalf("unchanged state moved ContentVersion")
	}
}

// TestStrippedContent_ReusedAcrossRecomputes proves the cache contract: a
// second query edit must not re-strip (re-allocate) the per-line text.
func TestStrippedContent_ReusedAcrossRecomputes(t *testing.T) {
	m := New(nil, &git.Change{Path: "main.go"}, git.DiffModeUnstaged, 80, 24)
	m.loading = false
	m.diff = testDiffResult()

	m.query = "line"
	m.recomputeMatches()
	first := m.strippedLines
	if len(first) == 0 {
		t.Fatal("recomputeMatches did not populate the stripped cache")
	}
	if len(m.matches) == 0 {
		t.Fatal("expected matches for 'line'")
	}

	m.query = "new"
	m.recomputeMatches()
	if len(m.strippedLines) != len(first) || &m.strippedLines[0] != &first[0] {
		t.Fatal("recomputeMatches re-allocated the stripped cache for the same diff")
	}
	if len(m.matches) == 0 {
		t.Fatal("expected matches for 'new'")
	}
}

// TestStrippedContent_InvalidatesWithDiff proves a swapped diff pointer
// cannot serve the previous diff's stripped lines.
func TestStrippedContent_InvalidatesWithDiff(t *testing.T) {
	m := New(nil, &git.Change{Path: "main.go"}, git.DiffModeUnstaged, 80, 24)
	m.loading = false
	m.diff = testDiffResult()
	m.query = "line"
	m.recomputeMatches()
	first := m.strippedLines

	m.diff = &git.DiffResult{
		Lines: []git.DiffLine{{Kind: git.DiffLineAdd, Content: "+replacement"}},
	}
	m.recomputeMatches()
	if len(m.strippedLines) == len(first) && len(first) > 0 && &m.strippedLines[0] == &first[0] {
		t.Fatal("stripped cache survived a diff-pointer swap")
	}
	if m.strippedLines[0] != "+replacement" {
		t.Fatalf("stripped[0] = %q, want %q", m.strippedLines[0], "+replacement")
	}
}

// TestStrippedContent_StripsAnsi pins that match spans land on stripped
// coordinates (the same space visual rows segment in).
func TestStrippedContent_StripsAnsi(t *testing.T) {
	m := New(nil, &git.Change{Path: "main.go"}, git.DiffModeUnstaged, 80, 24)
	m.loading = false
	m.diff = &git.DiffResult{
		Lines: []git.DiffLine{
			{Kind: git.DiffLineAdd, Content: "+\x1b[31mneedle\x1b[0m tail"},
		},
	}
	m.query = "needle"
	m.recomputeMatches()
	if len(m.matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(m.matches))
	}
	got := m.matches[0]
	if got.startRune != 1 || got.endRune != 7 {
		t.Fatalf("match span = (%d,%d), want (1,7) on stripped text", got.startRune, got.endRune)
	}
}

// BenchmarkRecomputeMatches measures the per-query-edit cost over a
// realistic diff: line stripping is amortized by the cache, so allocations
// should reflect match bookkeeping only — not one Strip per line.
func BenchmarkRecomputeMatches(b *testing.B) {
	m := New(nil, &git.Change{Path: "main.go"}, git.DiffModeUnstaged, 120, 40)
	m.loading = false
	lines := make([]git.DiffLine, 4000)
	for i := range lines {
		switch i % 4 {
		case 0:
			lines[i] = git.DiffLine{Kind: git.DiffLineHeader, Content: "@@ -1,3 +1,4 @@ hunk"}
		case 1:
			lines[i] = git.DiffLine{Kind: git.DiffLineAdd, Content: "+added content line"}
		case 2:
			lines[i] = git.DiffLine{Kind: git.DiffLineDelete, Content: "-removed content line"}
		default:
			lines[i] = git.DiffLine{Kind: git.DiffLineContext, Content: " context line"}
		}
	}
	m.diff = &git.DiffResult{Lines: lines}
	m.query = "content"
	m.recomputeMatches() // warm the stripped cache
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.recomputeMatches()
	}
}
