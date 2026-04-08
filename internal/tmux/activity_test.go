package tmux

import (
	"testing"
	"time"
)

func TestActivityWithinWindow_AccountsForSecondResolution(t *testing.T) {
	window := 2 * time.Second
	activitySeconds := int64(12)

	if !activityWithinWindow(activitySeconds, window, time.Unix(14, 100_000_000)) {
		t.Fatal("expected recent activity near a second boundary to remain active")
	}
	if activityWithinWindow(activitySeconds, window, time.Unix(15, 100_000_000)) {
		t.Fatal("expected activity past the extra second of tmux precision slack to be inactive")
	}
}

func TestActivityWithinWindow_IgnoresInvalidInput(t *testing.T) {
	now := time.Unix(10, 0)
	if activityWithinWindow(0, 2*time.Second, now) {
		t.Fatal("expected zero activity timestamp to be inactive")
	}
	if activityWithinWindow(10, 0, now) {
		t.Fatal("expected non-positive activity windows to be inactive")
	}
}
