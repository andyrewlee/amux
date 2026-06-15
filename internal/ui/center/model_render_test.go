package center

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// addWorkspaceWithTabs wires a workspace and its tabs into the model and makes
// it the active workspace. It returns the workspace ID for further assertions.
func addWorkspaceWithTabs(t *testing.T, m *Model, name string, tabs ...*Tab) string {
	t.Helper()
	ws := newTestWorkspace(name, "/repo/"+name)
	wsID := string(ws.ID())
	m.tabs.ByWorkspace[wsID] = tabs
	if len(tabs) > 0 {
		m.tabs.ActiveByWorkspace[wsID] = 0
	}
	m.SetWorkspace(ws)
	return wsID
}

func TestTabBarViewEmptyShowsNewAgentAffordance(t *testing.T) {
	m := newTestModel()

	// No workspace, no tabs: the tab bar collapses to the lone "New agent"
	// affordance and registers a single plus hit region.
	got := ansi.Strip(m.TabBarView())
	if !strings.Contains(got, "New agent") {
		t.Fatalf("expected empty tab bar to render the New agent affordance, got %q", got)
	}
	if strings.Contains(got, "+ New") {
		t.Fatalf("expected empty tab bar not to render the populated +-New button, got %q", got)
	}
	if len(m.tabHits) != 1 || m.tabHits[0].kind != tabHitPlus {
		t.Fatalf("expected exactly one plus hit region for empty tab bar, got %+v", m.tabHits)
	}
}

func TestTabBarViewWithTabsRendersNamesAndPlusButton(t *testing.T) {
	m := newTestModel()
	m.SetSize(80, 24)
	addWorkspaceWithTabs(t, m,
		"ws",
		&Tab{ID: TabID("a"), Assistant: "claude", Name: "alpha", Running: true},
		&Tab{ID: TabID("b"), Assistant: "codex", Name: "beta", Running: true},
	)

	got := ansi.Strip(m.TabBarView())
	for _, want := range []string{"alpha", "beta", "+ New"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected tab bar to contain %q, got %q", want, got)
		}
	}

	// One tab + one close hit per tab, plus the trailing plus button.
	var tabHitCount, closeHitCount, plusHitCount int
	for _, h := range m.tabHits {
		switch h.kind {
		case tabHitTab:
			tabHitCount++
		case tabHitClose:
			closeHitCount++
		case tabHitPlus:
			plusHitCount++
		}
	}
	if tabHitCount != 2 {
		t.Fatalf("expected 2 tab hit regions, got %d (%+v)", tabHitCount, m.tabHits)
	}
	if closeHitCount != 2 {
		t.Fatalf("expected 2 close hit regions, got %d", closeHitCount)
	}
	if plusHitCount != 1 {
		t.Fatalf("expected 1 plus hit region, got %d", plusHitCount)
	}
}

func TestTabBarViewFallsBackToAssistantWhenNameEmpty(t *testing.T) {
	m := newTestModel()
	m.SetSize(80, 24)
	addWorkspaceWithTabs(t, m,
		"ws",
		&Tab{ID: TabID("a"), Assistant: "claude", Running: true},
	)

	got := ansi.Strip(m.TabBarView())
	if !strings.Contains(got, "claude") {
		t.Fatalf("expected unnamed tab to fall back to its assistant name, got %q", got)
	}
}

func TestHelpLinesNilWhenHintsDisabled(t *testing.T) {
	m := newTestModel()
	addWorkspaceWithTabs(t, m,
		"ws",
		&Tab{ID: TabID("a"), Assistant: "claude", Running: true},
	)
	m.SetShowKeymapHints(false)

	if lines := m.HelpLines(80); lines != nil {
		t.Fatalf("expected nil help lines when hints disabled, got %#v", lines)
	}
}

func TestHelpLinesClampsNonPositiveWidth(t *testing.T) {
	m := newTestModel()
	m.SetShowKeymapHints(true)
	addWorkspaceWithTabs(t, m,
		"ws",
		&Tab{ID: TabID("a"), Assistant: "claude", Running: true},
	)

	for _, width := range []int{0, -5} {
		// width<1 is clamped to 1, which forces every item onto its own line
		// rather than panicking or returning nothing.
		lines := m.HelpLines(width)
		if len(lines) == 0 {
			t.Fatalf("width %d: expected help lines, got none", width)
		}
		joined := ansi.Strip(strings.Join(lines, "\n"))
		if !strings.Contains(joined, "new agent tab") {
			t.Fatalf("width %d: expected the new-agent help item, got %q", width, joined)
		}
	}
}

func TestHelpLinesIncludeWorkspaceAndTabItems(t *testing.T) {
	m := newTestModel()
	m.SetShowKeymapHints(true)
	addWorkspaceWithTabs(t, m,
		"ws",
		&Tab{ID: TabID("a"), Assistant: "claude", Running: true},
	)

	joined := ansi.Strip(strings.Join(m.HelpLines(200), "\n"))
	for _, want := range []string{
		"new agent tab",
		"close",
		"detach",
		"reattach",
		"restart",
		"prev",
		"next",
		"jump tab",
		"scroll up",
		"scroll down",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected help text to contain %q, got %q", want, joined)
		}
	}
}

func TestHelpLinesWorkspaceOnlyWhenNoTabs(t *testing.T) {
	m := newTestModel()
	m.SetShowKeymapHints(true)
	// Workspace present but with zero tabs: only the new-agent item shows.
	addWorkspaceWithTabs(t, m, "ws")

	joined := ansi.Strip(strings.Join(m.HelpLines(200), "\n"))
	if !strings.Contains(joined, "new agent tab") {
		t.Fatalf("expected new-agent help item with a workspace present, got %q", joined)
	}
	for _, absent := range []string{"close", "detach", "scroll up"} {
		if strings.Contains(joined, absent) {
			t.Fatalf("expected tab-only help item %q to be absent without tabs, got %q", absent, joined)
		}
	}
}

func TestHelpLinesEmptyWithoutWorkspaceOrTabs(t *testing.T) {
	m := newTestModel()
	m.SetShowKeymapHints(true)

	// No workspace and no tabs: WrapHelpItems returns a single empty line.
	lines := m.HelpLines(200)
	if len(lines) != 1 || lines[0] != "" {
		t.Fatalf("expected a single empty help line with no workspace/tabs, got %#v", lines)
	}
}

func TestHelpLinesWrapsAcrossMultipleLinesWhenNarrow(t *testing.T) {
	m := newTestModel()
	m.SetShowKeymapHints(true)
	addWorkspaceWithTabs(t, m,
		"ws",
		&Tab{ID: TabID("a"), Assistant: "claude", Running: true},
	)

	wide := m.HelpLines(1000)
	narrow := m.HelpLines(20)
	if len(narrow) <= len(wide) {
		t.Fatalf("expected narrow width to wrap into more lines than wide; wide=%d narrow=%d", len(wide), len(narrow))
	}
	for _, line := range narrow {
		if lipgloss.Width(line) > 20 && strings.Count(ansi.Strip(line), ":") > 1 {
			t.Fatalf("expected wrapped lines to respect width budget, got over-wide line %q", line)
		}
	}
}

func TestRenderEmptyContainsTitleButtonAndHelp(t *testing.T) {
	m := newTestModel()

	got := ansi.Strip(m.renderEmpty())
	for _, want := range []string{
		"No agents running",
		"New agent",
		"C-Spc t a:new agent",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected empty state to contain %q, got %q", want, got)
		}
	}
	// The empty state opens with vertical padding (two blank lines) so the
	// title is not glued to the tab bar.
	if !strings.HasPrefix(got, "\n\n") {
		t.Fatalf("expected empty state to start with leading blank lines, got %q", got)
	}
}

func TestTerminalViewportMatchesTerminalMetrics(t *testing.T) {
	m := newTestModel()
	m.SetSize(120, 40)

	tm := m.terminalMetrics()
	x, y, w, h := m.TerminalViewport()
	if x != tm.ContentStartX || y != tm.ContentStartY {
		t.Fatalf("viewport origin mismatch: got (%d,%d), want (%d,%d)", x, y, tm.ContentStartX, tm.ContentStartY)
	}
	if w != tm.Width || h != tm.Height {
		t.Fatalf("viewport size mismatch: got (%d,%d), want (%d,%d)", w, h, tm.Width, tm.Height)
	}
	// Origin is fixed by the border (1) + padding/tab-bar (1) layout constants.
	if x != 2 || y != 2 {
		t.Fatalf("expected fixed content origin (2,2), got (%d,%d)", x, y)
	}
}

func TestTerminalViewportShrinksWithHelpLines(t *testing.T) {
	m := newTestModel()
	m.SetSize(80, 40)

	m.SetShowKeymapHints(false)
	_, _, _, noHints := m.TerminalViewport()

	m.SetShowKeymapHints(true)
	addWorkspaceWithTabs(t, m,
		"ws",
		&Tab{ID: TabID("a"), Assistant: "claude", Running: true},
	)
	_, _, _, withHints := m.TerminalViewport()

	if withHints >= noHints {
		t.Fatalf("expected help lines to reduce viewport height: noHints=%d withHints=%d", noHints, withHints)
	}
}

func TestTerminalViewportClampsTinyAndZeroSizes(t *testing.T) {
	m := newTestModel()

	// Zero size: terminalMetrics applies its sane fallbacks (80x24) rather
	// than returning degenerate or negative dimensions.
	m.SetSize(0, 0)
	x, y, w, h := m.TerminalViewport()
	if x != 2 || y != 2 {
		t.Fatalf("expected fixed origin even at zero size, got (%d,%d)", x, y)
	}
	if w <= 0 || h <= 0 {
		t.Fatalf("expected positive fallback dimensions at zero size, got (%d,%d)", w, h)
	}

	// A tiny-but-positive width below the readability floor also falls back.
	m.SetSize(3, 2)
	_, _, w2, h2 := m.TerminalViewport()
	if w2 <= 0 || h2 <= 0 {
		t.Fatalf("expected positive fallback dimensions at tiny size, got (%d,%d)", w2, h2)
	}
}

func TestViewChromeOnlyStartsWithTabBar(t *testing.T) {
	m := newTestModel()
	m.SetSize(80, 24)
	m.SetShowKeymapHints(true)
	addWorkspaceWithTabs(t, m,
		"ws",
		&Tab{ID: TabID("a"), Assistant: "claude", Name: "alpha", Running: true},
	)

	chrome := m.ViewChromeOnly()
	firstLine := ansi.Strip(strings.SplitN(chrome, "\n", 2)[0])
	if !strings.Contains(firstLine, "alpha") {
		t.Fatalf("expected chrome to start with the tab bar, got first line %q", firstLine)
	}
	// Help text lives at the bottom of the chrome.
	if !strings.Contains(ansi.Strip(chrome), "new agent tab") {
		t.Fatalf("expected chrome to include help lines, got %q", ansi.Strip(chrome))
	}
}

func TestViewChromeOnlyOmitsHelpWhenHintsDisabled(t *testing.T) {
	m := newTestModel()
	m.SetSize(80, 24)
	m.SetShowKeymapHints(false)
	addWorkspaceWithTabs(t, m,
		"ws",
		&Tab{ID: TabID("a"), Assistant: "claude", Name: "alpha", Running: true},
	)

	chrome := ansi.Strip(m.ViewChromeOnly())
	if strings.Contains(chrome, "new agent tab") {
		t.Fatalf("expected no help lines when hints disabled, got %q", chrome)
	}
}

func TestViewChromeOnlyMatchesViewHeight(t *testing.T) {
	// ViewChromeOnly documents that its layout must match View() exactly so the
	// VTermLayer overlays line up. Both must produce the same total line count.
	m := newTestModel()
	m.SetSize(100, 30)
	m.SetShowKeymapHints(true)
	addWorkspaceWithTabs(t, m,
		"ws",
		&Tab{ID: TabID("a"), Assistant: "claude", Name: "alpha", Running: true},
	)

	viewLines := strings.Count(m.View(), "\n")
	chromeLines := strings.Count(m.ViewChromeOnly(), "\n")
	if viewLines != chromeLines {
		t.Fatalf("expected View and ViewChromeOnly to share line counts: view=%d chrome=%d", viewLines, chromeLines)
	}
}

func TestViewChromeOnlyHandlesZeroHeight(t *testing.T) {
	m := newTestModel()
	m.SetSize(40, 0)
	m.SetShowKeymapHints(true)

	// innerHeight clamps to 0; the chrome still renders the tab bar without
	// panicking or producing negative padding.
	chrome := m.ViewChromeOnly()
	if chrome == "" {
		t.Fatal("expected non-empty chrome even at zero height (tab bar present)")
	}
	first := ansi.Strip(strings.SplitN(chrome, "\n", 2)[0])
	if !strings.Contains(first, "New agent") {
		t.Fatalf("expected tab bar on the first chrome line at zero height, got %q", first)
	}
}

func TestViewChromeOnlyPadsToTargetWidth(t *testing.T) {
	m := newTestModel()
	m.SetSize(60, 20)
	m.SetShowKeymapHints(false)
	addWorkspaceWithTabs(t, m,
		"ws",
		&Tab{ID: TabID("a"), Assistant: "claude", Name: "alpha", Running: true},
	)

	chrome := m.ViewChromeOnly()
	lines := strings.Split(chrome, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected multiple chrome lines for a tall pane, got %d", len(lines))
	}
	// The filler lines (everything after the tab bar) are spaces padded to the
	// content width so the VTermLayer can overwrite a uniform region.
	contentWidth := m.contentWidth()
	for i := 1; i < len(lines); i++ {
		stripped := ansi.Strip(lines[i])
		if stripped == "" {
			continue
		}
		if strings.TrimSpace(stripped) != "" {
			// Non-empty, non-space lines are status/help, not filler; skip.
			continue
		}
		if lipgloss.Width(lines[i]) != contentWidth {
			t.Fatalf("expected filler line %d padded to content width %d, got width %d", i, contentWidth, lipgloss.Width(lines[i]))
		}
	}
}
