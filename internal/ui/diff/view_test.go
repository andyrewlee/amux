package diff

import (
	"errors"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/git"
)

// newSizedModel returns a Model with a usable viewport but no diff/loading state,
// so individual tests can opt into a specific View() branch.
func newSizedModel() *Model {
	return &Model{
		change: &git.Change{Path: "foo.go"},
		mode:   git.DiffModeUnstaged,
		width:  80,
		height: 24,
	}
}

// TestViewVariants drives every branch of Model.View() and asserts a stable
// substring of the rendered output. Substrings are kept to stable words that
// survive lipgloss styling (the styled text within a single Render call is
// contiguous, so plain words are not split by ANSI escapes).
func TestViewVariants(t *testing.T) {
	tests := []struct {
		name    string
		build   func() *Model
		wantSub string
	}{
		{
			name: "loading",
			build: func() *Model {
				m := newSizedModel()
				m.loading = true
				return m
			},
			wantSub: "Loading diff...",
		},
		{
			name: "error",
			build: func() *Model {
				m := newSizedModel()
				m.err = errors.New("boom")
				return m
			},
			wantSub: "Error: boom",
		},
		{
			name: "empty-nil-diff",
			build: func() *Model {
				m := newSizedModel()
				m.diff = nil
				return m
			},
			wantSub: "No file selected",
		},
		{
			name: "binary",
			build: func() *Model {
				m := newSizedModel()
				m.diff = &git.DiffResult{Binary: true}
				return m
			},
			wantSub: "Binary file - cannot display diff",
		},
		{
			name: "large",
			build: func() *Model {
				m := newSizedModel()
				m.diff = &git.DiffResult{Large: true}
				return m
			},
			wantSub: "File too large to display",
		},
		{
			name: "no-changes-empty-flag",
			build: func() *Model {
				m := newSizedModel()
				m.diff = &git.DiffResult{Empty: true}
				return m
			},
			wantSub: "No changes to display",
		},
		{
			name: "no-changes-nil-lines",
			build: func() *Model {
				m := newSizedModel()
				m.diff = &git.DiffResult{Lines: nil}
				return m
			},
			wantSub: "No changes to display",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := tt.build().View()
			if !strings.Contains(out, tt.wantSub) {
				t.Fatalf("View() = %q, want substring %q", out, tt.wantSub)
			}
		})
	}
}

// TestViewDiffRendersContentAndStats exercises the renderDiff path: a normal
// diff with add/delete/context lines must surface its content, the +N/-N stat,
// hunk count, and the footer keybindings.
func TestViewDiffRendersContentAndStats(t *testing.T) {
	m := newSizedModel()
	m.diff = &git.DiffResult{
		Lines: []git.DiffLine{
			{Kind: git.DiffLineHeader, Content: "@@ -1,3 +1,4 @@"},
			{Kind: git.DiffLineContext, Content: "context line"},
			{Kind: git.DiffLineAdd, Content: "added apple"},
			{Kind: git.DiffLineAdd, Content: "added banana"},
			{Kind: git.DiffLineDelete, Content: "removed cherry"},
		},
		Hunks: []git.Hunk{{StartLine: 0}},
	}

	out := m.View()

	for _, want := range []string{
		"added apple",    // add content line
		"removed cherry", // delete content line
		"context line",   // context content line
		"+2",             // AddedLines stat
		"-1",             // DeletedLines stat
		"(1 hunks)",      // hunk count from renderDiff
		"j/k",            // footer keybinding
		"1/5",            // footer scroll position
		"hunk 1/1",       // footer hunk info
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("View() diff output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestViewMultibyteTruncation feeds a normal diff whose content line contains
// multibyte runes and is far longer than the viewport width, forcing the
// truncation path in renderLine. It pins current behavior: View() must not
// panic and must return a non-empty string.
//
// characterizes current behavior: renderLine truncates content by BYTE index
// (content[:contentWidth-3]), which can slice a multibyte UTF-8 codepoint in
// half and emit a replacement/mojibake rune rather than truncating on a rune
// boundary. The test asserts only no-panic + non-empty, so it stays green even
// though the byte-slice is a latent correctness bug (see report).
func TestViewMultibyteTruncation(t *testing.T) {
	// Build a line much wider than contentWidth (~80-6=74), leading with CJK
	// runes (3 bytes each in UTF-8) so the byte-slice boundary lands mid-rune.
	longContent := strings.Repeat("日本語", 50) + strings.Repeat("ABCDEF", 50)

	m := newSizedModel()
	m.diff = &git.DiffResult{
		Lines: []git.DiffLine{
			{Kind: git.DiffLineAdd, Content: longContent},
		},
	}

	var out string
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("View() panicked on multibyte truncation input: %v", r)
			}
		}()
		out = m.View()
	}()

	if out == "" {
		t.Fatal("View() returned empty string for multibyte diff")
	}
}

// TestViewMultibyteWrap covers the wrapLine path (also a byte-index slice) by
// enabling wrap on the same oversized multibyte content. Pins no-panic +
// non-empty; the byte-boundary wrapping shares the same latent rune bug.
func TestViewMultibyteWrap(t *testing.T) {
	longContent := strings.Repeat("日本語", 50) + strings.Repeat("ABCDEF", 50)

	m := newSizedModel()
	m.wrap = true
	m.diff = &git.DiffResult{
		Lines: []git.DiffLine{
			{Kind: git.DiffLineContext, Content: longContent},
		},
	}

	var out string
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("View() panicked on multibyte wrap input: %v", r)
			}
		}()
		out = m.View()
	}()

	if out == "" {
		t.Fatal("View() returned empty string for multibyte wrapped diff")
	}
}
