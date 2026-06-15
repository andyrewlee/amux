package sidebar

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// The functions under test here — View, helpItem, and helpLines — are pure
// rendering helpers: they read in-memory Model state and return strings. None of
// them exec an external process (git/tmux) or require a live Bubble Tea program,
// so they are exercised directly with real assertions on their output.

// lineCount counts the number of lines a rendered string occupies. A non-empty
// string with no trailing newline still occupies one line.
func lineCount(s string) int {
	if s == "" {
		return 1
	}
	return strings.Count(s, "\n") + 1
}

func TestViewWithoutStatusShowsPlaceholder(t *testing.T) {
	m := New()
	m.SetSize(40, 10)

	out := m.View()

	// renderChanges() returns the "No status loaded" placeholder when gitStatus
	// is nil, and View must surface it.
	if !strings.Contains(out, "No status loaded") {
		t.Fatalf("View() missing placeholder for nil status, got %q", out)
	}
}

func TestViewCleanTreeShowsCleanMessage(t *testing.T) {
	m := New()
	m.SetSize(40, 10)
	m.SetGitStatus(&git.StatusResult{Clean: true})

	out := m.View()

	if !strings.Contains(out, "Working tree clean") {
		t.Fatalf("View() missing clean message, got %q", out)
	}
}

func TestViewRendersBranchAndChangedFiles(t *testing.T) {
	m := New()
	m.SetSize(60, 12)
	m.SetWorkspace(&data.Workspace{Branch: "feature/widget"})
	m.SetGitStatus(&git.StatusResult{
		Unstaged: []git.Change{
			{Path: "alpha.go", Kind: git.ChangeModified},
			{Path: "beta.go", Kind: git.ChangeModified},
		},
	})

	out := m.View()

	if !strings.Contains(out, "feature/widget") {
		t.Fatalf("View() should render the workspace branch, got %q", out)
	}
	if !strings.Contains(out, "2 changed files") {
		t.Fatalf("View() should render the changed-file count, got %q", out)
	}
	if !strings.Contains(out, "alpha.go") || !strings.Contains(out, "beta.go") {
		t.Fatalf("View() should render every changed file path, got %q", out)
	}
}

func TestViewNeverExceedsHeight(t *testing.T) {
	tests := []struct {
		name   string
		height int
	}{
		{name: "tiny height clamps multi-line body", height: 1},
		{name: "small height", height: 3},
		{name: "exact height", height: 6},
		{name: "generous height", height: 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			m.SetSize(50, tt.height)
			// Enough files to overflow the small heights so the clamp branch runs.
			changes := make([]git.Change, 0, 8)
			for _, p := range []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go", "h.go"} {
				changes = append(changes, git.Change{Path: p, Kind: git.ChangeModified})
			}
			m.SetWorkspace(&data.Workspace{Branch: "main"})
			m.SetGitStatus(&git.StatusResult{Unstaged: changes})

			out := m.View()
			if got := lineCount(out); got > tt.height {
				t.Fatalf("View() produced %d lines, must not exceed height %d; out=%q", got, tt.height, out)
			}
		})
	}
}

func TestViewPadsToHeightWhenContentIsShort(t *testing.T) {
	m := New()
	m.SetSize(40, 12)
	// A clean tree produces only a couple of lines; View pads with blank lines
	// up to the configured height (minus help lines, which are hidden by default).
	m.SetGitStatus(&git.StatusResult{Clean: true})

	out := m.View()

	if got := lineCount(out); got != 12 {
		t.Fatalf("View() should pad short content to full height 12, got %d lines; out=%q", got, out)
	}
}

func TestViewZeroHeightDoesNotClampOrPanic(t *testing.T) {
	m := New()
	// Height 0 disables the line-trimming clamp (guarded by m.height > 0) and
	// must not panic. The body still renders.
	m.SetSize(40, 0)
	m.SetGitStatus(&git.StatusResult{
		Unstaged: []git.Change{{Path: "only.go", Kind: git.ChangeModified}},
	})

	out := m.View()
	if !strings.Contains(out, "only.go") {
		t.Fatalf("View() with zero height should still render content, got %q", out)
	}
}

func TestViewClampsWidthBelowOne(t *testing.T) {
	// width < 1 forces the contentWidth = 1 fallback used to wrap help lines.
	// With hints enabled this exercises the narrow-width help path without panicking.
	m := New()
	m.SetSize(0, 20)
	m.SetShowKeymapHints(true)
	m.SetGitStatus(&git.StatusResult{Clean: true})

	out := m.View()
	if out == "" {
		t.Fatal("View() with zero width should still return non-empty output")
	}
}

func TestViewShowsHelpLinesOnlyWhenHintsEnabled(t *testing.T) {
	tests := []struct {
		name      string
		showHints bool
		wantHelp  bool
	}{
		{name: "hints enabled renders help keys", showHints: true, wantHelp: true},
		{name: "hints disabled hides help keys", showHints: false, wantHelp: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			m.SetSize(80, 20)
			m.SetShowKeymapHints(tt.showHints)
			m.SetGitStatus(&git.StatusResult{Clean: true})

			out := m.View()

			// "filter" only appears in the help bar for a clean tree, so its
			// presence is a reliable proxy for help lines being rendered.
			gotHelp := strings.Contains(out, "filter")
			if gotHelp != tt.wantHelp {
				t.Fatalf("View() help presence = %v, want %v; out=%q", gotHelp, tt.wantHelp, out)
			}
		})
	}
}

func TestViewWithHelpReservesRowsForHints(t *testing.T) {
	m := New()
	m.SetSize(80, 20)
	m.SetShowKeymapHints(true)
	m.SetGitStatus(&git.StatusResult{Clean: true})

	out := m.View()

	// Total output must still respect the height budget even with help lines
	// appended, and must contain the help content.
	if got := lineCount(out); got > 20 {
		t.Fatalf("View() with hints exceeded height: %d lines > 20", got)
	}
	if !strings.Contains(out, "up") || !strings.Contains(out, "down") {
		t.Fatalf("View() with hints should render help descriptions, got %q", out)
	}
}

func TestHelpItemContainsKeyAndDescription(t *testing.T) {
	tests := []struct {
		name string
		key  string
		desc string
	}{
		{name: "typical", key: "j/↓", desc: "down"},
		{name: "empty key", key: "", desc: "down"},
		{name: "empty desc", key: "k", desc: ""},
		{name: "both empty", key: "", desc: ""},
		{name: "unicode glyphs", key: "↑", desc: "上"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			got := m.helpItem(tt.key, tt.desc)
			want := common.RenderHelpItem(m.styles, tt.key, tt.desc)
			// helpItem is a thin wrapper over common.RenderHelpItem; the contract
			// is that it delegates with the model's own styles.
			if got != want {
				t.Fatalf("helpItem(%q,%q) = %q, want %q", tt.key, tt.desc, got, want)
			}
			if tt.key != "" && !strings.Contains(got, tt.key) {
				t.Fatalf("helpItem output %q missing key %q", got, tt.key)
			}
			if tt.desc != "" && !strings.Contains(got, tt.desc) {
				t.Fatalf("helpItem output %q missing desc %q", got, tt.desc)
			}
		})
	}
}

func TestHelpItemUsesModelStyles(t *testing.T) {
	m := New()
	// Override one help style with a recognizable string so we can prove
	// helpItem threads the model's styles through to the renderer.
	custom := common.DefaultStyles()
	custom.HelpKey = custom.HelpKey.SetString("KEYMARK")
	m.SetStyles(custom)

	got := m.helpItem("x", "do x")
	if !strings.Contains(got, "KEYMARK") {
		t.Fatalf("helpItem should use the model's styles, got %q", got)
	}
}

func TestHelpLinesContainsAllBindings(t *testing.T) {
	m := New()
	// A wide width keeps every item on a single line so we can assert on order
	// and presence without worrying about wrapping.
	lines := m.helpLines(200)

	if len(lines) != 1 {
		t.Fatalf("expected all help items to fit on one wide line, got %d lines: %#v", len(lines), lines)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"up", "down", "filter"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("helpLines(200) missing %q, got %q", want, joined)
		}
	}
}

func TestHelpLinesWrapsOnNarrowWidth(t *testing.T) {
	m := New()
	wide := m.helpLines(200)
	narrow := m.helpLines(8)

	// The same set of items must wrap into more rows as the width shrinks, while
	// preserving the underlying descriptions across the wrapped lines.
	if len(narrow) <= len(wide) {
		t.Fatalf("expected narrow width to wrap into more lines: narrow=%d wide=%d", len(narrow), len(wide))
	}
	joined := strings.Join(narrow, "\n")
	for _, want := range []string{"up", "down", "filter"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("wrapped helpLines lost %q, got %#v", want, narrow)
		}
	}
}

func TestHelpLinesNonPositiveWidthReturnsSingleLine(t *testing.T) {
	tests := []struct {
		name  string
		width int
	}{
		{name: "zero width", width: 0},
		{name: "negative width", width: -10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			lines := m.helpLines(tt.width)
			// common.WrapHelpItems collapses to a single joined line when width
			// is non-positive; helpLines must return exactly that.
			if len(lines) != 1 {
				t.Fatalf("helpLines(%d) should return one line, got %d: %#v", tt.width, len(lines), lines)
			}
			for _, want := range []string{"up", "down", "filter"} {
				if !strings.Contains(lines[0], want) {
					t.Fatalf("helpLines(%d) missing %q, got %q", tt.width, want, lines[0])
				}
			}
		})
	}
}

func TestHelpLinesMatchHelpLineCount(t *testing.T) {
	// helpLineCount is the layout helper that View depends on for spacing; it must
	// agree with the actual number of lines helpLines produces at a given width.
	tests := []int{8, 20, 200}
	for _, width := range tests {
		m := New()
		m.SetSize(width, 20)
		m.SetShowKeymapHints(true)

		lines := m.helpLines(width)
		if got := m.helpLineCount(); got != len(lines) {
			t.Fatalf("helpLineCount() = %d but helpLines(%d) produced %d lines", got, width, len(lines))
		}
	}
}
