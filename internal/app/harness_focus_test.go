package app

import "testing"

func TestHarnessRender_SyncsFocusedPaneFlags(t *testing.T) {
	centerHarness, err := NewHarness(HarnessOptions{
		Mode:   HarnessCenter,
		Width:  120,
		Height: 40,
	})
	if err != nil {
		t.Fatalf("center harness init: %v", err)
	}
	if centerHarness.app.center.Focused() {
		t.Fatalf("expected center to start unfocused before render")
	}
	_ = centerHarness.Render()
	if !centerHarness.app.center.Focused() {
		t.Fatalf("expected center focused after render sync")
	}

	sidebarHarness, err := NewHarness(HarnessOptions{
		Mode:   HarnessSidebar,
		Width:  120,
		Height: 40,
	})
	if err != nil {
		t.Fatalf("sidebar harness init: %v", err)
	}
	if sidebarHarness.app.sidebarTerminal.Focused() {
		t.Fatalf("expected sidebar terminal to start unfocused before render")
	}
	_ = sidebarHarness.Render()
	if !sidebarHarness.app.sidebarTerminal.Focused() {
		t.Fatalf("expected sidebar terminal focused after render sync")
	}
}
