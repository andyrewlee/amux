package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
	"github.com/andyrewlee/amux/internal/vterm"
)

// newGateCenterHarness builds a center-mode harness with keymap hints enabled
// so the center help pane renders and its compose gate is exercised.
func newGateCenterHarness(t *testing.T) *Harness {
	t.Helper()
	h, err := NewHarness(HarnessOptions{
		Mode:            HarnessCenter,
		Tabs:            2,
		Width:           120,
		Height:          40,
		ShowKeymapHints: true,
	})
	if err != nil {
		t.Fatalf("center harness init: %v", err)
	}
	return h
}

// TestCenterHelpGate_SkipsRebuildWhenClean renders the same frame repeatedly
// with no state change and asserts the center help string-build path
// (Model.HelpLines) is not re-entered: the paneGate must reuse the cached
// drawable instead of rebuilding an identical string every spinner tick.
func TestCenterHelpGate_SkipsRebuildWhenClean(t *testing.T) {
	h := newGateCenterHarness(t)

	h.Render()
	builds := h.app.center.HelpBuildCount()
	if builds == 0 {
		t.Fatal("first render never built the center help pane; the gate path was not exercised")
	}
	if !h.app.renderCache.centerHelpGate.rendered {
		t.Fatal("first render did not record a composed help drawable")
	}

	h.Render()
	h.Render()

	if got := h.app.center.HelpBuildCount(); got != builds {
		t.Fatalf("help string rebuilt on clean re-render: builds %d -> %d", builds, got)
	}
}

// TestCenterHelpGate_RebuildsWhenDirty mutates the center pane (adds a tab,
// which bumps the help version via noteTabsChanged) and asserts the next
// render re-enters the help string build.
func TestCenterHelpGate_RebuildsWhenDirty(t *testing.T) {
	h := newGateCenterHarness(t)

	h.Render()
	h.Render()
	builds := h.app.center.HelpBuildCount()
	if builds == 0 {
		t.Fatal("renders never built the center help pane; the gate path was not exercised")
	}

	ws := harnessWorkspace()
	h.app.center.AddTab(&center.Tab{
		ID:        center.TabID("gate-tab"),
		Name:      "amp-gate",
		Assistant: "amp",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
	})
	h.Render()

	if got := h.app.center.HelpBuildCount(); got <= builds {
		t.Fatalf("help string not rebuilt after tab add: builds stayed at %d", builds)
	}
}

// TestCenterHelpGate_RebuildsWhenHintsToggled flips keymap hints off and
// asserts the gate invalidates: the help build path re-runs and the gate stops
// composing a help drawable (the pane's rendered content is now empty).
func TestCenterHelpGate_RebuildsWhenHintsToggled(t *testing.T) {
	h := newGateCenterHarness(t)

	h.Render()
	builds := h.app.center.HelpBuildCount()
	if builds == 0 {
		t.Fatal("first render never built the center help pane; the gate path was not exercised")
	}

	h.app.setKeymapHintsEnabled(false)
	h.Render()

	if got := h.app.center.HelpBuildCount(); got <= builds {
		t.Fatalf("help build path not re-entered after hints toggle: builds stayed at %d", builds)
	}
	if h.app.renderCache.centerHelpGate.rendered {
		t.Fatal("gate still composes a help drawable after hints were disabled")
	}
}

// TestSidebarTabBarGate_SkipsCleanAndRebuildsDirty covers both gate behaviors
// for the sidebar Changes/Project tab bar: clean re-renders must not re-enter
// TabBarView, and an active-tab switch must force a rebuild.
func TestSidebarTabBarGate_SkipsCleanAndRebuildsDirty(t *testing.T) {
	// Wide enough for the three-pane layout; the sidebar top pane only
	// composes in LayoutThreePane.
	h, err := NewHarness(HarnessOptions{
		Mode:   HarnessSidebar,
		Tabs:   1,
		Width:  200,
		Height: 50,
	})
	if err != nil {
		t.Fatalf("sidebar harness init: %v", err)
	}

	h.Render()
	builds := h.app.sidebar.TabBarBuildCount()
	if builds == 0 {
		t.Fatal("first render never built the sidebar tab bar; the gate path was not exercised")
	}
	if !h.app.renderCache.sidebarTopTabBarGate.rendered {
		t.Fatal("first render did not record a composed sidebar tab bar drawable")
	}

	h.Render()
	h.Render()
	if got := h.app.sidebar.TabBarBuildCount(); got != builds {
		t.Fatalf("sidebar tab bar rebuilt on clean re-render: builds %d -> %d", builds, got)
	}

	h.app.sidebar.SetActiveTab(sidebar.TabProject)
	h.Render()
	if got := h.app.sidebar.TabBarBuildCount(); got <= builds {
		t.Fatalf("sidebar tab bar not rebuilt after active-tab switch: builds stayed at %d", builds)
	}
}

// TestPaneGate_GeometryChangeForcesRebuild pins the reviewer-flagged resize
// invariant: a pane that is content-clean must still rebuild when the compose
// geometry changes, because the cached drawable bakes in position and width.
func TestPaneGate_GeometryChangeForcesRebuild(t *testing.T) {
	gate := paneGate{}
	geom := [4]int{10, 2, 80, 40}
	gate.record(7, geom, true)

	if !gate.clean(7, geom) {
		t.Fatal("gate should be clean for the recorded version and geometry")
	}
	for i := 0; i < len(geom); i++ {
		changed := geom
		changed[i]++
		if gate.clean(7, changed) {
			t.Fatalf("gate reported clean despite geometry component %d changing", i)
		}
	}
	if gate.clean(8, geom) {
		t.Fatal("gate reported clean despite a version bump")
	}
}
