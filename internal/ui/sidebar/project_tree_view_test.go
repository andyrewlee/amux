package sidebar

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// helpItemTexts is the set of "key" tokens helpLines always renders. They
// survive style rendering as literal substrings, so we can assert on them even
// though the surrounding bytes are ANSI escapes.
var helpItemTexts = []struct{ key, desc string }{
	{"k/↑", "up"},
	{"j/↓", "down"},
	{"h/←", "collapse"},
	{"l/→", "expand"},
	{"enter/o", "open"},
	{".", "hidden"},
	{"r", "refresh"},
}

func TestProjectTreeHelpItem(t *testing.T) {
	m := NewProjectTree()

	tests := []struct {
		name string
		key  string
		desc string
	}{
		{name: "simple", key: "k", desc: "up"},
		{name: "arrow key", key: "k/↑", desc: "up"},
		{name: "empty key", key: "", desc: "noop"},
		{name: "empty desc", key: "x", desc: ""},
		{name: "both empty", key: "", desc: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.helpItem(tt.key, tt.desc)
			// helpItem renders "<key>" + ":<desc>". Both the raw key and the
			// ":<desc>" fragment must appear in the (styled) output.
			if tt.key != "" && !strings.Contains(got, tt.key) {
				t.Fatalf("helpItem(%q,%q) = %q, missing key %q", tt.key, tt.desc, got, tt.key)
			}
			if !strings.Contains(got, ":"+tt.desc) {
				t.Fatalf("helpItem(%q,%q) = %q, missing %q", tt.key, tt.desc, got, ":"+tt.desc)
			}
		})
	}
}

func TestProjectTreeHelpLinesContainsAllItems(t *testing.T) {
	m := NewProjectTree()
	// A wide width keeps everything on as few lines as possible, but every item
	// must still be present somewhere in the joined output.
	joined := strings.Join(m.helpLines(120), "\n")
	for _, item := range helpItemTexts {
		if !strings.Contains(joined, item.key) {
			t.Fatalf("helpLines missing key %q in:\n%s", item.key, joined)
		}
		if !strings.Contains(joined, ":"+item.desc) {
			t.Fatalf("helpLines missing desc %q in:\n%s", item.desc, joined)
		}
	}
}

func TestProjectTreeHelpLinesWrapByWidth(t *testing.T) {
	m := NewProjectTree()

	tests := []struct {
		name      string
		width     int
		wantLines int
		// reason documents the wrapping boundary being exercised.
		reason string
	}{
		{name: "very narrow puts each item on its own line", width: 4, wantLines: 7, reason: "7 items, none fit together"},
		{name: "wide fits everything on one line", width: 500, wantLines: 1, reason: "all 7 items fit"},
		{name: "zero width single joined line", width: 0, wantLines: 1, reason: "WrapHelpItems joins with no wrap"},
		{name: "negative width single joined line", width: -10, wantLines: 1, reason: "WrapHelpItems joins with no wrap"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.helpLines(tt.width)
			if len(got) != tt.wantLines {
				t.Fatalf("helpLines(%d) produced %d lines, want %d (%s):\n%#v",
					tt.width, len(got), tt.wantLines, tt.reason, got)
			}
		})
	}
}

func TestProjectTreeHelpLinesWideNarrowsToWiderResult(t *testing.T) {
	m := NewProjectTree()
	// More width must never yield more lines than less width: wrapping is
	// monotonic in the available width for these fixed items.
	narrow := len(m.helpLines(10))
	wide := len(m.helpLines(120))
	if wide > narrow {
		t.Fatalf("expected wider width to produce <= lines, narrow(10)=%d wide(120)=%d", narrow, wide)
	}
}

func TestProjectTreeHelpLineCount(t *testing.T) {
	tests := []struct {
		name      string
		width     int
		showHints bool
		wantZero  bool
	}{
		{name: "hints off returns zero", width: 40, showHints: false, wantZero: true},
		{name: "hints on positive", width: 40, showHints: true, wantZero: false},
		{name: "hints on narrow positive", width: 4, showHints: true, wantZero: false},
		{name: "hints on zero width still positive", width: 0, showHints: true, wantZero: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewProjectTree()
			m.SetSize(tt.width, 20)
			m.SetShowKeymapHints(tt.showHints)

			got := m.helpLineCount()
			if tt.wantZero {
				if got != 0 {
					t.Fatalf("helpLineCount() = %d, want 0", got)
				}
				return
			}
			if got < 1 {
				t.Fatalf("helpLineCount() = %d, want >= 1", got)
			}
			// helpLineCount must agree with helpLines for the clamped width it
			// uses internally (width < 1 is clamped to 1).
			width := tt.width
			if width < 1 {
				width = 1
			}
			if want := len(m.helpLines(width)); got != want {
				t.Fatalf("helpLineCount() = %d, want len(helpLines(%d)) = %d", got, width, want)
			}
		})
	}
}

func TestProjectTreeViewNoWorkspace(t *testing.T) {
	m := NewProjectTree()
	m.SetSize(40, 10)
	m.SetShowKeymapHints(false)

	out := m.View()
	if !strings.Contains(out, "No workspace selected") {
		t.Fatalf("expected no-workspace placeholder, got:\n%q", out)
	}
}

func TestProjectTreeViewEmptyDirectory(t *testing.T) {
	// A workspace whose root has no entries (after seeding then clearing) yields
	// an empty flat list and the "Empty directory" placeholder.
	m := newSeededProjectTree(t)
	// Force the empty-directory branch by clearing the flat list while keeping a
	// non-nil workspace.
	m.flatNodes = nil
	m.SetSize(40, 10)
	m.SetShowKeymapHints(false)

	out := m.View()
	if !strings.Contains(out, "Empty directory") {
		t.Fatalf("expected empty-directory placeholder, got:\n%q", out)
	}
}

func TestProjectTreeViewRendersNodeNames(t *testing.T) {
	m := newSeededProjectTree(t)
	m.SetSize(40, 20)
	m.SetShowKeymapHints(false)

	out := m.View()
	for _, name := range []string{"alpha", "beta", "one.txt", "two.txt"} {
		if !strings.Contains(out, name) {
			t.Fatalf("View missing node %q in:\n%s", name, out)
		}
	}
}

func TestProjectTreeViewClampsToHeight(t *testing.T) {
	m := newSeededProjectTree(t)
	m.SetShowKeymapHints(false)

	tests := []struct {
		name   string
		height int
	}{
		{name: "height larger than node count", height: 20},
		{name: "height equal to node count", height: 4},
		{name: "height smaller than node count", height: 2},
		{name: "single line height", height: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.SetSize(40, tt.height)
			out := m.View()
			lines := strings.Count(out, "\n") + 1
			if lines > tt.height {
				t.Fatalf("View produced %d lines, want <= height %d:\n%q", lines, tt.height, out)
			}
		})
	}
}

func TestProjectTreeViewScrollsToCursor(t *testing.T) {
	m := newSeededProjectTree(t)
	m.SetShowKeymapHints(false)
	// Only one content row is visible, forcing scrolling.
	m.SetSize(40, 1)

	// Cursor on the last node must scroll it into view; the rendered line should
	// contain that node's name and not the first node's name.
	m.cursor = len(m.flatNodes) - 1
	out := m.View()
	last := m.flatNodes[len(m.flatNodes)-1].Name
	if !strings.Contains(out, last) {
		t.Fatalf("expected last node %q visible after scroll, got:\n%q", last, out)
	}
	if m.scrollOffset == 0 {
		t.Fatalf("expected scrollOffset to advance for bottom cursor, got %d", m.scrollOffset)
	}

	// Moving the cursor back to the top must scroll back up.
	m.cursor = 0
	out = m.View()
	if !strings.Contains(out, m.flatNodes[0].Name) {
		t.Fatalf("expected first node visible after scrolling up, got:\n%q", out)
	}
	if m.scrollOffset != 0 {
		t.Fatalf("expected scrollOffset reset to 0 for top cursor, got %d", m.scrollOffset)
	}
}

func TestProjectTreeViewMarksCursorRow(t *testing.T) {
	m := newSeededProjectTree(t)
	m.SetSize(40, 20)
	m.SetShowKeymapHints(false)
	m.cursor = 1

	out := m.View()
	lines := strings.Split(out, "\n")
	// The cursor row uses the filled cursor icon; the others use the empty one.
	// Exactly one rendered node row should carry the active cursor glyph.
	cursorGlyph := common.Icons.Cursor
	count := 0
	for _, line := range lines {
		if strings.Contains(line, cursorGlyph) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one active-cursor row, got %d in:\n%s", count, out)
	}
}

func TestProjectTreeViewTruncatesLongNames(t *testing.T) {
	m := newSeededProjectTree(t)
	m.SetShowKeymapHints(false)
	// A very narrow width forces name truncation with an ellipsis. Inject a long
	// name on the first visible node.
	m.flatNodes[0].Name = "this-is-an-extremely-long-file-name-that-cannot-fit"
	m.flatNodes[0].IsDir = false
	m.SetSize(20, 20)

	out := m.View()
	if !strings.Contains(out, "...") {
		t.Fatalf("expected ellipsis for truncated long name, got:\n%q", out)
	}
}

func TestProjectTreeViewHonorsHelpHints(t *testing.T) {
	m := newSeededProjectTree(t)
	m.SetSize(40, 20)

	m.SetShowKeymapHints(false)
	withoutHints := m.View()
	for _, item := range helpItemTexts {
		if strings.Contains(withoutHints, ":"+item.desc) {
			t.Fatalf("did not expect help desc %q when hints disabled:\n%s", item.desc, withoutHints)
		}
	}

	m.SetShowKeymapHints(true)
	withHints := m.View()
	if !strings.Contains(withHints, ":refresh") {
		t.Fatalf("expected help bar when hints enabled, got:\n%s", withHints)
	}
}

func TestProjectTreeRenderWithHelpPadsToHeight(t *testing.T) {
	m := NewProjectTree()
	m.SetSize(10, 5)
	m.SetShowKeymapHints(false)

	// Single-line content with hints off must be padded with blank lines up to
	// the model height.
	out := m.renderWithHelp("content")
	lines := strings.Count(out, "\n") + 1
	if lines != 5 {
		t.Fatalf("renderWithHelp padded to %d lines, want height 5:\n%q", lines, out)
	}
	if !strings.HasPrefix(out, "content") {
		t.Fatalf("expected content at top, got:\n%q", out)
	}
}

func TestProjectTreeRenderWithHelpEmptyContent(t *testing.T) {
	m := NewProjectTree()
	m.SetSize(10, 3)
	m.SetShowKeymapHints(false)

	out := m.renderWithHelp("")
	lines := strings.Count(out, "\n") + 1
	if lines > 3 {
		t.Fatalf("renderWithHelp(empty) produced %d lines, want <= height 3:\n%q", lines, out)
	}
}

func TestProjectTreeRenderWithHelpClampsOverflow(t *testing.T) {
	m := NewProjectTree()
	m.SetSize(10, 2)
	m.SetShowKeymapHints(false)

	// Content taller than the height must be clamped to exactly m.height lines.
	tall := strings.Join([]string{"a", "b", "c", "d", "e"}, "\n")
	out := m.renderWithHelp(tall)
	lines := strings.Count(out, "\n") + 1
	if lines != 2 {
		t.Fatalf("renderWithHelp clamped to %d lines, want height 2:\n%q", lines, out)
	}
}

func TestProjectTreeRenderWithHelpZeroHeightSkipsClamp(t *testing.T) {
	m := NewProjectTree()
	m.SetSize(10, 0)
	m.SetShowKeymapHints(false)

	// With height 0 the final clamp is skipped (the guard is height > 0), so the
	// original content is returned unchanged.
	out := m.renderWithHelp("single")
	if out != "single" {
		t.Fatalf("renderWithHelp with zero height = %q, want %q", out, "single")
	}
}

func TestProjectTreeRenderWithHelpClampsWidthFloor(t *testing.T) {
	m := NewProjectTree()
	// Width below 1 is floored to 1 internally; this must not panic and must
	// still render help when hints are on.
	m.SetSize(0, 10)
	m.SetShowKeymapHints(true)

	out := m.renderWithHelp("x")
	if !strings.Contains(out, ":refresh") {
		t.Fatalf("expected help rendered even at floored width, got:\n%q", out)
	}
}

func TestProjectTreeRenderWithHelpIncludesHelpBar(t *testing.T) {
	m := NewProjectTree()
	m.SetSize(120, 10)
	m.SetShowKeymapHints(true)

	out := m.renderWithHelp("body")
	if !strings.HasPrefix(out, "body") {
		t.Fatalf("expected content first, got:\n%q", out)
	}
	for _, item := range helpItemTexts {
		if !strings.Contains(out, ":"+item.desc) {
			t.Fatalf("renderWithHelp missing help desc %q in:\n%s", item.desc, out)
		}
	}
}
