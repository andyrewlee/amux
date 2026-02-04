package main

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func resetMouseFilterState() {
	lastMouseMotionEvent = time.Time{}
	lastMouseWheelEvent = time.Time{}
	lastMouseX = 0
	lastMouseY = 0
}

func TestMouseWheelNotThrottledByMotion(t *testing.T) {
	resetMouseFilterState()

	motion := tea.MouseMotionMsg{X: 10, Y: 10, Button: tea.MouseLeft}
	if mouseEventFilter(nil, motion) == nil {
		t.Fatalf("expected motion event to pass through")
	}

	wheel := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown}
	if mouseEventFilter(nil, wheel) == nil {
		t.Fatalf("expected wheel event to pass through after motion")
	}
}

func TestMouseWheelThrottleIndependent(t *testing.T) {
	resetMouseFilterState()

	wheel := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown}
	if mouseEventFilter(nil, wheel) == nil {
		t.Fatalf("expected first wheel event to pass through")
	}
	if mouseEventFilter(nil, wheel) != nil {
		t.Fatalf("expected second wheel event to be throttled")
	}
}
