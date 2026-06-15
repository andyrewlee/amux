package sidebar

import (
	"strings"
	"testing"
)

// Tests for StatusLine, HelpLines, View, TerminalOrigin and TerminalSize live
// here (split from terminal_render_test.go to respect the 500-line file cap).
// Shared helpers (newTerminalModelWithWorkspace, scrolledVTerm) and the tab-bar
// and layer tests live in terminal_render_test.go in this same package.

func TestStatusLineNoTerminal(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	if got := m.StatusLine(); got != "" {
		t.Fatalf("expected empty status line with no terminal, got %q", got)
	}
}

func TestStatusLineNilVTerm(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, &TerminalTab{ID: generateTerminalTabID(), Name: "T", State: &TerminalState{Running: true}})
	if got := m.StatusLine(); got != "" {
		t.Fatalf("expected empty status line with nil VTerm, got %q", got)
	}
}

func TestStatusLineRunningHasNoStatus(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	tab := newWorkspaceTab(t, "Terminal 1")
	tab.State.Running = true
	seedTabs(t, m, tab)
	if got := m.StatusLine(); got != "" {
		t.Fatalf("expected empty status line for a running, non-scrolled terminal, got %q", got)
	}
}

func TestStatusLineScrolled(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	tab := newWorkspaceTab(t, "Terminal 1")
	tab.State.Running = true
	tab.State.VTerm = scrolledVTerm(t)
	seedTabs(t, m, tab)

	got := m.StatusLine()
	if !strings.Contains(got, "SCROLL:") {
		t.Fatalf("expected SCROLL status when scrolled, got %q", got)
	}
	if !strings.Contains(got, "lines up") {
		t.Fatalf("expected scroll position in status, got %q", got)
	}
}

func TestStatusLineDetached(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	tab := newWorkspaceTab(t, "Terminal 1")
	tab.State.Detached = true
	tab.State.Running = true
	seedTabs(t, m, tab)

	if got := m.StatusLine(); !strings.Contains(got, "DETACHED") {
		t.Fatalf("expected DETACHED status, got %q", got)
	}
}

func TestStatusLineStopped(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	tab := newWorkspaceTab(t, "Terminal 1")
	tab.State.Running = false
	tab.State.Detached = false
	seedTabs(t, m, tab)

	if got := m.StatusLine(); !strings.Contains(got, "STOPPED") {
		t.Fatalf("expected STOPPED status, got %q", got)
	}
}

func TestStatusLineScrolledTakesPrecedenceOverDetached(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	tab := newWorkspaceTab(t, "Terminal 1")
	tab.State.Detached = true
	tab.State.VTerm = scrolledVTerm(t)
	seedTabs(t, m, tab)

	// Scroll state is checked before Detached, so SCROLL must win.
	got := m.StatusLine()
	if !strings.Contains(got, "SCROLL:") {
		t.Fatalf("expected SCROLL to take precedence over DETACHED, got %q", got)
	}
	if strings.Contains(got, "DETACHED") {
		t.Fatalf("did not expect DETACHED in scrolled status, got %q", got)
	}
}

func TestHelpLinesHiddenWhenHintsOff(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	m.height = 40
	// showKeymapHints defaults to false.
	if lines := m.HelpLines(80); lines != nil {
		t.Fatalf("expected nil help lines when hints disabled, got %v", lines)
	}
}

func TestHelpLinesShownWhenHintsOn(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))
	m.height = 40
	m.showKeymapHints = true

	lines := m.HelpLines(80)
	if len(lines) == 0 {
		t.Fatal("expected non-empty help lines when hints enabled")
	}
	joined := strings.Join(lines, " ")
	if !strings.Contains(joined, "new term") {
		t.Fatalf("expected 'new term' hint, got %q", joined)
	}
}

func TestHelpLinesClampedToHeight(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))
	m.showKeymapHints = true
	// Tiny width forces many wrapped help lines; tiny height clamps the count.
	m.height = 3 // maxHelpHeight = height - tabBarHeight - statusLineReserve = 1
	lines := m.HelpLines(1)
	maxHelpHeight := m.height - tabBarHeight - statusLineReserve
	if len(lines) > maxHelpHeight {
		t.Fatalf("expected help lines clamped to %d, got %d", maxHelpHeight, len(lines))
	}
}

func TestHelpLinesNegativeWidthIsClamped(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))
	m.showKeymapHints = true
	m.height = 40
	// width < 1 is clamped to 1 internally; must not panic and must return lines.
	lines := m.HelpLines(-5)
	if len(lines) == 0 {
		t.Fatal("expected help lines even with negative width")
	}
}

func TestHelpLinesIncludeMultiTabHints(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m,
		newWorkspaceTab(t, "Terminal 1"),
		newWorkspaceTab(t, "Terminal 2"),
	)
	m.showKeymapHints = true
	m.height = 40

	joined := strings.Join(m.HelpLines(200), " ")
	for _, want := range []string{"next", "prev", "close"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected multi-tab hint %q, got %q", want, joined)
		}
	}
}

func TestHelpLinesSingleTabOmitsMultiTabHints(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))
	m.showKeymapHints = true
	m.height = 40

	joined := strings.Join(m.HelpLines(200), " ")
	if strings.Contains(joined, ":next") {
		t.Fatalf("did not expect 'next' multi-tab hint with a single tab, got %q", joined)
	}
}

func TestTerminalOrigin(t *testing.T) {
	tests := []struct {
		name             string
		offsetX, offsetY int
		wantX, wantY     int
	}{
		{name: "zero origin offsets by tab bar", offsetX: 0, offsetY: 0, wantX: 0, wantY: 1},
		{name: "non-zero origin", offsetX: 4, offsetY: 7, wantX: 4, wantY: 8},
		{name: "negative offsets preserved", offsetX: -2, offsetY: -3, wantX: -2, wantY: -2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTerminalModel()
			m.offsetX = tt.offsetX
			m.offsetY = tt.offsetY
			gotX, gotY := m.TerminalOrigin()
			if gotX != tt.wantX || gotY != tt.wantY {
				t.Fatalf("TerminalOrigin() = (%d,%d), want (%d,%d)", gotX, gotY, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestTerminalSize(t *testing.T) {
	tests := []struct {
		name             string
		width, height    int
		hints            bool
		wantW            int
		wantHeightAtMost int
	}{
		// height - tabBarHeight(1) - statusLineReserve(1) - len(helpLines)
		{name: "typical size", width: 80, height: 30, hints: false, wantW: 80, wantHeightAtMost: 28},
		{name: "width clamped to 1", width: 0, height: 30, hints: false, wantW: 1, wantHeightAtMost: 28},
		{name: "height floored to 1", width: 80, height: 1, hints: false, wantW: 80, wantHeightAtMost: 1},
		{name: "negative height floored to 1", width: 80, height: -10, hints: false, wantW: 80, wantHeightAtMost: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTerminalModel()
			m.showKeymapHints = tt.hints
			m.width = tt.width
			m.height = tt.height
			w, h := m.TerminalSize()
			if w != tt.wantW {
				t.Fatalf("TerminalSize() width = %d, want %d", w, tt.wantW)
			}
			if h < 1 {
				t.Fatalf("TerminalSize() height = %d, want >= 1", h)
			}
			if h > tt.wantHeightAtMost {
				t.Fatalf("TerminalSize() height = %d, want <= %d", h, tt.wantHeightAtMost)
			}
		})
	}
}

func TestViewNoWorkspaceShowsNoTerminal(t *testing.T) {
	m := NewTerminalModel() // nil workspace
	m.width = 40
	m.height = 10
	out := m.View()
	if !strings.Contains(out, "No terminal") {
		t.Fatalf("expected 'No terminal' in view with no workspace, got %q", out)
	}
}

func TestViewWorkspaceNoTabsShowsNewButton(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	m.width = 40
	m.height = 10
	out := m.View()
	if !strings.Contains(out, "+ New") {
		t.Fatalf("expected '+ New' button in view for empty workspace, got %q", out)
	}
}

func TestViewRendersTabAndTerminal(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	tab := newWorkspaceTab(t, "Terminal 1")
	tab.State.Running = true
	seedTabs(t, m, tab)
	m.width = 40
	m.height = 10

	out := m.View()
	if !strings.Contains(out, "Terminal 1") {
		t.Fatalf("expected tab name in view, got %q", out)
	}
	// View must never exceed m.height lines.
	if got := len(strings.Split(out, "\n")); got > m.height {
		t.Fatalf("view produced %d lines, exceeds height %d", got, m.height)
	}
}

func TestViewRespectsHeightCap(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))
	m.width = 20
	m.height = 5

	out := m.View()
	lines := strings.Split(out, "\n")
	if len(lines) > m.height {
		t.Fatalf("expected at most %d lines, got %d", m.height, len(lines))
	}
}

func TestViewShowsScrollIndicator(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	tab := newWorkspaceTab(t, "Terminal 1")
	tab.State.Running = true
	tab.State.VTerm = scrolledVTerm(t)
	seedTabs(t, m, tab)
	m.width = 80
	m.height = 30

	out := m.View()
	if !strings.Contains(out, "SCROLL:") {
		t.Fatalf("expected SCROLL indicator in scrolled view, got %q", out)
	}
}

func TestViewZeroHeightDoesNotPanic(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))
	m.width = 10
	m.height = 0

	out := m.View()
	// With height 0 the cap loop is skipped; assert it still returns a string
	// containing the tab bar rather than panicking.
	if !strings.Contains(out, "Terminal 1") {
		t.Fatalf("expected tab name even at zero height, got %q", out)
	}
}
