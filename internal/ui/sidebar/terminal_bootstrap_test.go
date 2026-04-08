package sidebar

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCaptureExistingSessionBootstrap_RechecksExclusivityBeforeResize(t *testing.T) {
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionActiveWithinFn := sessionActiveWithinFn
	oldSessionCreatedAtFn := sessionCreatedAtFn
	oldSessionPaneIDFn := sessionPaneIDFn
	oldSessionPaneSnapshotInfoFn := sessionPaneSnapshotInfoFn
	oldResizePaneToSizeFn := resizePaneToSizeFn
	oldCapturePaneSnapshotFn := capturePaneSnapshotFn
	defer func() {
		sessionHasClientsFn = oldSessionHasClientsFn
		sessionActiveWithinFn = oldSessionActiveWithinFn
		sessionCreatedAtFn = oldSessionCreatedAtFn
		sessionPaneIDFn = oldSessionPaneIDFn
		sessionPaneSnapshotInfoFn = oldSessionPaneSnapshotInfoFn
		resizePaneToSizeFn = oldResizePaneToSizeFn
		capturePaneSnapshotFn = oldCapturePaneSnapshotFn
	}()

	calls := make([]string, 0, 8)
	hasClientsCalls := 0
	sessionHasClientsFn = func(sessionName string, opts tmux.Options) (bool, error) {
		calls = append(calls, "clients")
		hasClientsCalls++
		return hasClientsCalls >= 2, nil
	}
	sessionActiveWithinFn = func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
		calls = append(calls, "activity")
		return false, nil
	}
	sessionCreatedAtFn = func(sessionName string, opts tmux.Options) (int64, error) {
		calls = append(calls, "created")
		return 123, nil
	}
	sessionPaneIDFn = func(sessionName string, opts tmux.Options) (string, error) {
		calls = append(calls, "pane")
		return "%1", nil
	}
	sessionPaneSnapshotInfoFn = func(sessionName string, opts tmux.Options) (int, int, bool, error) {
		calls = append(calls, "info")
		return 91, 27, true, nil
	}
	resizePaneToSizeFn = func(sessionName string, cols, rows int, opts tmux.Options) error {
		calls = append(calls, "resize")
		return nil
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("frame")}, nil
	}

	bootstrap := captureExistingSessionBootstrap("session-race", 80, 24, tmux.Options{})
	if bootstrap.CaptureFullPane {
		t.Fatal("expected shared-on-recheck session to skip pre-attach full-pane bootstrap")
	}
	for _, call := range calls {
		if call == "resize" || call == "snapshot" {
			t.Fatalf("expected exclusivity recheck to prevent resize/snapshot, got %v", calls)
		}
	}
	assertSidebarCallOrder(t, calls, "clients", "activity", "created", "pane", "info", "clients", "activity")
}
