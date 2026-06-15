package sidebar

import (
	"fmt"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/vterm"
)

// newTerminalModelWithWorkspace returns a model bound to a workspace so that
// workspaceID() is non-empty and seedTabs installs into the right bucket.
func newTerminalModelWithWorkspace(t *testing.T) *TerminalModel {
	t.Helper()
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	return m
}

// scrolledVTerm returns a VTerm with scrollback that has been scrolled up so
// that IsScrolled() is true and GetScrollInfo() reports a non-zero offset.
func scrolledVTerm(t *testing.T) *vterm.VTerm {
	t.Helper()
	vt := vterm.New(80, 24)
	vt.AllowAltScreenScrollback = true
	for i := 0; i < 100; i++ {
		vt.Write([]byte(fmt.Sprintf("line %d\r\n", i)))
	}
	vt.ScrollView(10)
	if !vt.IsScrolled() {
		t.Fatal("setup: expected VTerm to be scrolled after ScrollView(10)")
	}
	return vt
}

func TestFormatScrollPos(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		total  int
		want   string
	}{
		{name: "zero total short-circuits", offset: 0, total: 0, want: "0/0"},
		{name: "zero total ignores offset", offset: 5, total: 0, want: "0/0"},
		{name: "typical position", offset: 3, total: 42, want: "3/42 lines up"},
		{name: "offset equals total", offset: 42, total: 42, want: "42/42 lines up"},
		{name: "single line", offset: 1, total: 1, want: "1/1 lines up"},
		{name: "negative offset is rendered verbatim", offset: -2, total: 10, want: "-2/10 lines up"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatScrollPos(tt.offset, tt.total); got != tt.want {
				t.Fatalf("formatScrollPos(%d, %d) = %q, want %q", tt.offset, tt.total, got, tt.want)
			}
		})
	}
}

func TestRenderTabBarNoWorkspace(t *testing.T) {
	m := NewTerminalModel() // no workspace set -> m.workspace == nil
	got := m.renderTabBar()
	if !strings.Contains(got, "No terminal") {
		t.Fatalf("expected 'No terminal' message with no workspace, got %q", got)
	}
	if len(m.tabHits) != 0 {
		t.Fatalf("expected no hit regions with no workspace, got %d", len(m.tabHits))
	}
}

func TestRenderTabBarWorkspaceNoTabs(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	got := m.renderTabBar()
	if !strings.Contains(got, "+ New") {
		t.Fatalf("expected '+ New' button when workspace has no tabs, got %q", got)
	}
	// A single clickable plus region must be registered.
	if len(m.tabHits) != 1 {
		t.Fatalf("expected exactly one hit region (plus), got %d", len(m.tabHits))
	}
	hit := m.tabHits[0]
	if hit.kind != terminalTabHitPlus {
		t.Fatalf("expected plus hit kind, got %v", hit.kind)
	}
	if hit.index != -1 {
		t.Fatalf("expected plus index -1, got %d", hit.index)
	}
	if hit.region.Width <= 0 || hit.region.Height != 1 {
		t.Fatalf("expected positive-width single-row region, got %+v", hit.region)
	}
}

func TestRenderTabBarSingleTab(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))

	got := m.renderTabBar()
	if !strings.Contains(got, "Terminal 1") {
		t.Fatalf("expected tab name in output, got %q", got)
	}
	if !strings.Contains(got, "+ New") {
		t.Fatalf("expected trailing '+ New' button, got %q", got)
	}

	// Expect one tab hit, one close hit, and one plus hit.
	var tabHits, closeHits, plusHits int
	for _, h := range m.tabHits {
		switch h.kind {
		case terminalTabHitTab:
			tabHits++
		case terminalTabHitClose:
			closeHits++
		case terminalTabHitPlus:
			plusHits++
		}
	}
	if tabHits != 1 {
		t.Fatalf("expected 1 tab hit, got %d", tabHits)
	}
	if closeHits != 1 {
		t.Fatalf("expected 1 close hit, got %d", closeHits)
	}
	if plusHits != 1 {
		t.Fatalf("expected 1 plus hit, got %d", plusHits)
	}
}

func TestRenderTabBarMultipleTabsHitLayout(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m,
		newWorkspaceTab(t, "Terminal 1"),
		newWorkspaceTab(t, "Terminal 2"),
		newWorkspaceTab(t, "Terminal 3"),
	)
	m.setActiveTabIdx(1)

	got := m.renderTabBar()
	for _, name := range []string{"Terminal 1", "Terminal 2", "Terminal 3"} {
		if !strings.Contains(got, name) {
			t.Fatalf("expected %q in tab bar, got %q", name, got)
		}
	}

	// Collect tab hits in order; their X origins must be strictly increasing
	// because each rendered tab is laid out left-to-right.
	var tabXs []int
	var plus int
	for _, h := range m.tabHits {
		switch h.kind {
		case terminalTabHitTab:
			tabXs = append(tabXs, h.region.X)
		case terminalTabHitPlus:
			plus++
		}
	}
	if len(tabXs) != 3 {
		t.Fatalf("expected 3 tab hits, got %d", len(tabXs))
	}
	for i := 1; i < len(tabXs); i++ {
		if tabXs[i] <= tabXs[i-1] {
			t.Fatalf("tab hit X origins not increasing: %v", tabXs)
		}
	}
	if plus != 1 {
		t.Fatalf("expected exactly 1 plus hit, got %d", plus)
	}
}

func TestRenderTabBarFallbackNameForEmptyName(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	// Empty Name triggers the "Terminal %d" fallback (1-indexed).
	seedTabs(t, m, newWorkspaceTab(t, ""))

	got := m.renderTabBar()
	if !strings.Contains(got, "Terminal 1") {
		t.Fatalf("expected fallback name 'Terminal 1' for empty tab name, got %q", got)
	}
}

func TestRenderTabBarResetsHitsBetweenCalls(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))

	m.renderTabBar()
	firstCount := len(m.tabHits)
	if firstCount == 0 {
		t.Fatal("expected hit regions after first render")
	}
	// A second render must not accumulate stale regions.
	m.renderTabBar()
	if len(m.tabHits) != firstCount {
		t.Fatalf("expected hit count to reset to %d on re-render, got %d", firstCount, len(m.tabHits))
	}
}

func TestTerminalTabBarViewMatchesRenderTabBar(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))

	// TabBarView is a thin wrapper around renderTabBar; both should agree.
	if got, want := m.TabBarView(), m.renderTabBar(); got != want {
		t.Fatalf("TabBarView() = %q, want %q", got, want)
	}
}

func TestTerminalLayerNilWhenNoTerminal(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	// No tabs -> getTerminal() is nil.
	if layer := m.TerminalLayer(); layer != nil {
		t.Fatalf("expected nil layer with no terminal, got %+v", layer)
	}
}

func TestTerminalLayerNilWhenNilVTerm(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, &TerminalTab{ID: generateTerminalTabID(), Name: "T", State: &TerminalState{}})
	if layer := m.TerminalLayer(); layer != nil {
		t.Fatalf("expected nil layer with nil VTerm, got %+v", layer)
	}
}

func TestTerminalLayerReturnsSnapshot(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))

	layer := m.TerminalLayer()
	if layer == nil {
		t.Fatal("expected non-nil layer for live VTerm")
	}
	if layer.Snap == nil {
		t.Fatal("expected snapshot in returned layer")
	}
	// newWorkspaceTab uses vterm.New(10, 3).
	if layer.Snap.Width != 10 || layer.Snap.Height != 3 {
		t.Fatalf("snapshot size = %dx%d, want 10x3", layer.Snap.Width, layer.Snap.Height)
	}
}

func TestTerminalLayerCursorVisibilityFollowsFocus(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))

	// Unfocused: cursor hidden in the snapshot.
	unfocused := m.TerminalLayer()
	if unfocused == nil || unfocused.Snap == nil {
		t.Fatal("expected snapshot while unfocused")
	}
	if unfocused.Snap.ShowCursor {
		t.Fatal("expected cursor hidden when model is unfocused")
	}

	// Focused: cursor shown.
	m.Focus()
	focused := m.TerminalLayer()
	if focused == nil || focused.Snap == nil {
		t.Fatal("expected snapshot while focused")
	}
	if !focused.Snap.ShowCursor {
		t.Fatal("expected cursor shown when model is focused")
	}
}

func TestTerminalLayerWithCursorOwnerFalseHidesCursor(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))
	m.Focus() // focused, but cursor ownership is denied below

	layer := m.TerminalLayerWithCursorOwner(false)
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected snapshot even when not cursor owner")
	}
	if layer.Snap.ShowCursor {
		t.Fatal("expected cursor hidden when this pane does not own the cursor")
	}
}

func TestTerminalLayerUsesCache(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	seedTabs(t, m, newWorkspaceTab(t, "Terminal 1"))

	first := m.TerminalLayer()
	if first == nil {
		t.Fatal("expected first layer")
	}
	ts := m.getTerminal()
	ts.mu.Lock()
	cached := ts.CachedSnap
	ts.mu.Unlock()
	if cached == nil {
		t.Fatal("expected snapshot to be cached after first call")
	}

	// Second call at the same version/focus must reuse the cached snapshot.
	second := m.TerminalLayer()
	if second == nil {
		t.Fatal("expected second layer")
	}
	if second.Snap != cached {
		t.Fatal("expected cached snapshot to be reused on second call")
	}
}
