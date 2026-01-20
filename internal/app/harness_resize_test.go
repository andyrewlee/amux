package app

import (
	"fmt"
	"testing"
)

func TestHarnessResizeRestoresScrollback(t *testing.T) {
	opts := HarnessOptions{
		Mode:    HarnessCenter,
		Tabs:    1,
		Width:   120,
		Height:  40,
		HotTabs: 0,
	}
	h, err := NewHarness(opts)
	if err != nil {
		t.Fatalf("harness init: %v", err)
	}
	if len(h.tabs) == 0 || h.tabs[0] == nil || h.tabs[0].Terminal == nil {
		t.Fatalf("expected harness tab terminal")
	}
	tab := h.tabs[0]

	for i := 0; i < 5; i++ {
		tab.WriteToTerminal([]byte(fmt.Sprintf("line %d\n", i)))
	}

	baseScrollback := len(tab.Terminal.Scrollback)
	if baseScrollback != 0 {
		t.Fatalf("expected empty scrollback before resize, got %d", baseScrollback)
	}
	baseHeight := tab.Terminal.Height
	if baseHeight <= 0 {
		t.Fatalf("expected terminal height > 0, got %d", baseHeight)
	}

	resizeHarness(t, h, 120, 20)
	shrinkScrollback := len(tab.Terminal.Scrollback)
	shrinkHeight := tab.Terminal.Height
	if shrinkHeight >= baseHeight {
		t.Fatalf("expected shrink height < %d, got %d", baseHeight, shrinkHeight)
	}
	if shrinkScrollback == 0 {
		t.Fatalf("expected scrollback after shrink, got 0")
	}

	resizeHarness(t, h, 120, 40)
	growScrollback := len(tab.Terminal.Scrollback)
	growHeight := tab.Terminal.Height
	if growHeight != baseHeight {
		t.Fatalf("expected height %d after grow, got %d", baseHeight, growHeight)
	}
	if growScrollback >= shrinkScrollback {
		t.Fatalf("expected scrollback to decrease after grow (shrink=%d grow=%d)", shrinkScrollback, growScrollback)
	}
	if growScrollback != baseScrollback {
		t.Fatalf("expected scrollback to restore to %d after grow, got %d", baseScrollback, growScrollback)
	}
}

func resizeHarness(t *testing.T, h *Harness, width, height int) {
	t.Helper()
	if h == nil || h.app == nil {
		t.Fatalf("harness app not initialized")
	}
	h.app.width = width
	h.app.height = height
	if h.app.layout != nil {
		h.app.layout.Resize(width, height)
	}
	h.app.updateLayout()
}
