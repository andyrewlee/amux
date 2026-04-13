package app

import (
	"fmt"
	"testing"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
)

func newScrollableCenterWheelHarness(t *testing.T) (*App, *center.Tab) {
	t.Helper()

	h, err := NewHarness(HarnessOptions{
		Mode:    HarnessCenter,
		Tabs:    1,
		Width:   140,
		Height:  40,
		HotTabs: 0,
	})
	if err != nil {
		t.Fatalf("harness init: %v", err)
	}
	if h.app == nil || len(h.tabs) != 1 || h.tabs[0] == nil || h.tabs[0].Terminal == nil {
		t.Fatal("expected harness center tab terminal")
	}

	tab := h.tabs[0]
	for i := 0; i < 80; i++ {
		tab.WriteToTerminal([]byte(fmt.Sprintf("line %d\n", i)))
	}
	tab.Terminal.ScrollView(12)
	offset, _ := tab.Terminal.GetScrollInfo()
	if offset == 0 {
		t.Fatal("expected center terminal to start scrolled into history")
	}

	if cmd := h.app.focusPane(messages.PaneCenter); cmd != nil {
		t.Fatal("expected scroll harness focus to be synchronous")
	}
	return h.app, tab
}
