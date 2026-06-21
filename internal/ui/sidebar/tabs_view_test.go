package sidebar

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRenderTabBarHighlightsActiveTab(t *testing.T) {
	tests := []struct {
		name   string
		active SidebarTab
	}{
		{name: "changes active", active: TabChanges},
		{name: "project active", active: TabProject},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestTabbedSidebar(t)
			s.activeTab = tc.active

			bar := s.renderTabBar()

			if !strings.Contains(bar, "Changes") {
				t.Fatalf("tab bar missing Changes label: %q", bar)
			}
			if !strings.Contains(bar, "Project") {
				t.Fatalf("tab bar missing Project label: %q", bar)
			}
			// renderTabBar must register exactly two clickable hit regions.
			if len(s.tabHits) != 2 {
				t.Fatalf("expected 2 tab hits, got %d", len(s.tabHits))
			}
			if s.tabHits[0].kind != tabHitChanges {
				t.Fatalf("first hit kind = %d, want tabHitChanges", s.tabHits[0].kind)
			}
			if s.tabHits[1].kind != tabHitProject {
				t.Fatalf("second hit kind = %d, want tabHitProject", s.tabHits[1].kind)
			}
			// Hit regions must be laid out left-to-right without gaps that
			// would make the Project tab unclickable.
			c, p := s.tabHits[0].region, s.tabHits[1].region
			if c.X != 0 {
				t.Fatalf("Changes hit should start at x=0, got %d", c.X)
			}
			if p.X != c.X+c.Width {
				t.Fatalf("Project hit x=%d should follow Changes (x=%d w=%d)", p.X, c.X, c.Width)
			}
			if c.Width <= 0 || p.Width <= 0 {
				t.Fatalf("hit widths must be positive, got changes=%d project=%d", c.Width, p.Width)
			}
		})
	}
}

func TestRenderTabBarResetsHitsAcrossCalls(t *testing.T) {
	s := newTestTabbedSidebar(t)

	s.renderTabBar()
	s.renderTabBar()
	s.renderTabBar()

	// Hits are reset (sliced to zero) each call, so repeated renders must not
	// accumulate stale regions.
	if len(s.tabHits) != 2 {
		t.Fatalf("expected 2 tab hits after repeated renders, got %d", len(s.tabHits))
	}
}

func TestViewEmptyWhenNoHeight(t *testing.T) {
	tests := []struct {
		name   string
		height int
	}{
		{name: "zero height", height: 0},
		{name: "negative height", height: -3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestTabbedSidebar(t)
			s.SetSize(40, tc.height)

			if got := s.View(); got != "" {
				t.Fatalf("expected empty View for height %d, got %q", tc.height, got)
			}
		})
	}
}

func TestViewTabBarOnlyWhenContentHeightNonPositive(t *testing.T) {
	s := newTestTabbedSidebar(t)
	// height == 1 -> contentHeight == 0 -> View returns just the tab bar.
	s.SetSize(40, 1)

	view := s.View()
	bar := s.renderTabBar()

	if view != bar {
		t.Fatalf("expected View to equal tab bar only, got view=%q bar=%q", view, bar)
	}
	if strings.Contains(view, "\n") {
		t.Fatalf("tab-bar-only view should be single line, got %q", view)
	}
}

func TestViewRendersTabBarPlusContent(t *testing.T) {
	for _, active := range []SidebarTab{TabChanges, TabProject} {
		s := newTestTabbedSidebar(t)
		s.activeTab = active
		s.SetSize(40, 20)

		view := s.View()
		if view == "" {
			t.Fatalf("expected non-empty view for active tab %d", active)
		}
		// First line must be the tab bar (labels present), and there must be
		// at least one newline separating the bar from the content area.
		lines := strings.SplitN(view, "\n", 2)
		if len(lines) < 2 {
			t.Fatalf("expected tab bar + content for active tab %d, got %q", active, view)
		}
		if !strings.Contains(lines[0], "Changes") || !strings.Contains(lines[0], "Project") {
			t.Fatalf("first line should be tab bar, got %q", lines[0])
		}
		// View sizes the inner active model to contentHeight = height-1.
		switch active {
		case TabChanges:
			if s.changes.height != 19 {
				t.Fatalf("changes inner height = %d, want 19", s.changes.height)
			}
		case TabProject:
			if s.projectTree.height != 19 {
				t.Fatalf("projectTree inner height = %d, want 19", s.projectTree.height)
			}
		}
	}
}

func TestTabBarViewMatchesRenderTabBar(t *testing.T) {
	s := newTestTabbedSidebar(t)
	s.SetSize(40, 20)

	if got, want := s.TabBarView(), s.renderTabBar(); got != want {
		t.Fatalf("TabBarView() = %q, want %q", got, want)
	}
}

func TestContentViewEmptyWhenContentHeightNonPositive(t *testing.T) {
	tests := []struct {
		name   string
		height int
	}{
		{name: "height one yields zero content", height: 1},
		{name: "zero height", height: 0},
		{name: "negative height", height: -2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestTabbedSidebar(t)
			s.SetSize(40, tc.height)

			if got := s.ContentView(); got != "" {
				t.Fatalf("expected empty ContentView for height %d, got %q", tc.height, got)
			}
		})
	}
}

func TestContentViewSizesAndRendersActiveTab(t *testing.T) {
	for _, active := range []SidebarTab{TabChanges, TabProject} {
		s := newTestTabbedSidebar(t)
		s.activeTab = active
		s.SetSize(40, 20)

		content := s.ContentView()
		if content == "" {
			t.Fatalf("expected non-empty content for active tab %d", active)
		}
		// ContentView omits the tab bar entirely.
		if strings.Contains(content, "Changes") && strings.Contains(content, "Project") {
			// Labels could legitimately appear in file lists, but the literal
			// joined tab bar should not be the leading content.
			bar := s.renderTabBar()
			if strings.HasPrefix(content, bar) {
				t.Fatalf("ContentView should not include the tab bar, got %q", content)
			}
		}
		switch active {
		case TabChanges:
			if s.changes.height != 19 {
				t.Fatalf("changes inner height = %d, want 19", s.changes.height)
			}
		case TabProject:
			if s.projectTree.height != 19 {
				t.Fatalf("projectTree inner height = %d, want 19", s.projectTree.height)
			}
		}
	}
}

func TestUpdateNumberKeysSwitchTabsWhenFocused(t *testing.T) {
	s := newTestTabbedSidebar(t)
	s.Focus()

	// "2" -> Project
	updated, cmd := s.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	if updated.ActiveTab() != TabProject {
		t.Fatalf("after key '2' want TabProject, got %d", updated.ActiveTab())
	}
	if cmd != nil {
		t.Fatalf("tab switch should return nil cmd, got %T", cmd)
	}
	if !updated.projectTree.Focused() || updated.changes.Focused() {
		t.Fatal("focus should follow to project tree after switching with '2'")
	}

	// "1" -> Changes
	updated, _ = updated.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	if updated.ActiveTab() != TabChanges {
		t.Fatalf("after key '1' want TabChanges, got %d", updated.ActiveTab())
	}
	if !updated.changes.Focused() || updated.projectTree.Focused() {
		t.Fatal("focus should follow to changes after switching with '1'")
	}
}

func TestUpdateNumberKeysIgnoredWhenUnfocused(t *testing.T) {
	s := newTestTabbedSidebar(t)
	// Not focused: number keys must not switch tabs.
	s.Update(tea.KeyPressMsg{Code: '2', Text: "2"})

	if s.ActiveTab() != TabChanges {
		t.Fatalf("unfocused number key should not switch tabs, got %d", s.ActiveTab())
	}
}

func TestUpdateNumberKeysIgnoredWhileChangesFilterActive(t *testing.T) {
	s := newTestTabbedSidebar(t)
	s.Focus()
	s.SetActiveTab(TabChanges)
	s.changes.filterMode = true
	s.changes.filterInput.Focus()

	updated, _ := s.Update(tea.KeyPressMsg{Code: '2', Text: "2"})

	if updated.ActiveTab() != TabChanges {
		t.Fatalf("filter-active number key should not switch tabs, got %d", updated.ActiveTab())
	}
}

func TestUpdateMouseClickOnTabBarSwitchesTabs(t *testing.T) {
	s := newTestTabbedSidebar(t)
	s.SetSize(40, 20)
	// Populate tabHits via a render so the click can resolve a region.
	s.renderTabBar()

	// Click within the Project tab region on row 0.
	projectRegion := s.tabHits[1].region
	clickX := projectRegion.X + projectRegion.Width/2

	updated, cmd := s.Update(tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      clickX,
		Y:      0,
	})
	if updated.ActiveTab() != TabProject {
		t.Fatalf("click on Project tab should activate it, got %d", updated.ActiveTab())
	}
	if cmd != nil {
		t.Fatalf("tab bar click should return nil cmd, got %T", cmd)
	}

	// Now render again and click the Changes region.
	updated.renderTabBar()
	changesRegion := updated.tabHits[0].region
	updated, _ = updated.Update(tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      changesRegion.X,
		Y:      0,
	})
	if updated.ActiveTab() != TabChanges {
		t.Fatalf("click on Changes tab should activate it, got %d", updated.ActiveTab())
	}
}

func TestUpdateMouseClickOutsideTabBarDoesNotSwitch(t *testing.T) {
	s := newTestTabbedSidebar(t)
	s.SetSize(40, 20)
	s.renderTabBar()

	// Click below the tab bar (Y > 0) is forwarded to the content, not a switch.
	updated, _ := s.Update(tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      1,
		Y:      5,
	})
	if updated.ActiveTab() != TabChanges {
		t.Fatalf("content click should not switch tabs, got %d", updated.ActiveTab())
	}

	// A non-left button on row 0 should also not switch.
	updated, _ = updated.Update(tea.MouseClickMsg{
		Button: tea.MouseRight,
		X:      s.tabHits[1].region.X,
		Y:      0,
	})
	if updated.ActiveTab() != TabChanges {
		t.Fatalf("right click on tab bar should not switch tabs, got %d", updated.ActiveTab())
	}
}

func TestUpdateMouseWheelDoesNotSwitchTabsOrCrash(t *testing.T) {
	s := newTestTabbedSidebar(t)
	s.SetSize(40, 20)
	s.renderTabBar()

	updated, _ := s.Update(tea.MouseWheelMsg{
		Button: tea.MouseWheelDown,
		X:      1,
		Y:      3,
	})
	if updated.ActiveTab() != TabChanges {
		t.Fatalf("wheel should not switch active tab, got %d", updated.ActiveTab())
	}
}

func TestUpdateForwardsToActiveTabAndReturnsSelf(t *testing.T) {
	s := newTestTabbedSidebar(t)
	s.SetSize(40, 20)

	// An unrelated message is forwarded to the active tab; the pointer
	// returned must be the same sidebar instance.
	updated, _ := s.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	if updated != s {
		t.Fatal("Update should return the same sidebar pointer")
	}
}
