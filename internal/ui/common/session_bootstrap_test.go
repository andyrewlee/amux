package common

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCaptureExistingSessionBootstrap_SkipsRollbackWhenClientsAppearAfterResize(t *testing.T) {
	calls := make([]string, 0, 12)
	hasClientsCalls := 0
	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		SessionHasClients: func(sessionName string, opts tmux.Options) (bool, error) {
			calls = append(calls, "clients")
			hasClientsCalls++
			return hasClientsCalls >= 3, nil
		},
		SessionActiveWithin: func(sessionName string, quietWindow time.Duration, opts tmux.Options) (bool, error) {
			calls = append(calls, "activity")
			return false, nil
		},
		SessionCreatedAt: func(sessionName string, opts tmux.Options) (int64, error) {
			calls = append(calls, "created")
			return 123, nil
		},
		SessionPaneID: func(sessionName string, opts tmux.Options) (string, error) {
			calls = append(calls, "pane")
			return "%1", nil
		},
		SessionPaneSnapshotInfo: func(sessionName string, opts tmux.Options) (int, int, bool, error) {
			calls = append(calls, "info")
			return 91, 27, true, nil
		},
		SessionPaneSize: func(sessionName string, opts tmux.Options) (int, int, bool, error) {
			return 0, 0, false, nil
		},
		ResizePaneToSize: func(sessionName string, cols, rows int, opts tmux.Options) error {
			calls = append(calls, "resize")
			return nil
		},
		CapturePaneSnapshot: func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
			t.Fatal("did not expect snapshot capture after exclusivity was lost")
			return tmux.PaneSnapshot{}, nil
		},
	})

	if bootstrap.CaptureFullPane {
		t.Fatal("expected full-pane bootstrap to abort once another client attached")
	}
	resizeCalls := 0
	for _, call := range calls {
		if call == "resize" {
			resizeCalls++
		}
	}
	if resizeCalls != 1 {
		t.Fatalf("expected exactly one resize without rollback after client attach, got %d calls: %v", resizeCalls, calls)
	}
}

func TestCaptureExistingSessionBootstrap_IgnoresResizeInducedActivity(t *testing.T) {
	calls := make([]string, 0, 12)
	activityCalls := 0
	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		SessionHasClients: func(sessionName string, opts tmux.Options) (bool, error) {
			calls = append(calls, "clients")
			return false, nil
		},
		SessionActiveWithin: func(sessionName string, quietWindow time.Duration, opts tmux.Options) (bool, error) {
			calls = append(calls, "activity")
			activityCalls++
			return activityCalls >= 3, nil
		},
		SessionCreatedAt: func(sessionName string, opts tmux.Options) (int64, error) {
			calls = append(calls, "created")
			return 123, nil
		},
		SessionPaneID: func(sessionName string, opts tmux.Options) (string, error) {
			calls = append(calls, "pane")
			return "%1", nil
		},
		SessionPaneSnapshotInfo: func(sessionName string, opts tmux.Options) (int, int, bool, error) {
			calls = append(calls, "info")
			return 91, 27, true, nil
		},
		SessionPaneSize: func(sessionName string, opts tmux.Options) (int, int, bool, error) {
			return 0, 0, false, nil
		},
		ResizePaneToSize: func(sessionName string, cols, rows int, opts tmux.Options) error {
			calls = append(calls, "resize")
			return nil
		},
		CapturePaneSnapshot: func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
			calls = append(calls, "snapshot")
			return tmux.PaneSnapshot{Data: []byte("frame"), Cols: 80, Rows: 24}, nil
		},
	})

	if !bootstrap.CaptureFullPane {
		t.Fatalf("expected bootstrap snapshot to survive resize-induced activity, got %+v (calls=%v)", bootstrap, calls)
	}
	if len(bootstrap.Snapshot.Data) == 0 {
		t.Fatalf("expected snapshot data to be preserved, got %+v", bootstrap)
	}
}

func TestCaptureExistingSessionBootstrap_StartsFreshnessWindowBeforeSnapshotReturns(t *testing.T) {
	captureDelay := 25 * time.Millisecond
	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		SessionHasClients: func(sessionName string, opts tmux.Options) (bool, error) {
			return false, nil
		},
		SessionActiveWithin: func(sessionName string, quietWindow time.Duration, opts tmux.Options) (bool, error) {
			return false, nil
		},
		SessionCreatedAt: func(sessionName string, opts tmux.Options) (int64, error) {
			return 123, nil
		},
		SessionPaneID: func(sessionName string, opts tmux.Options) (string, error) {
			return "%1", nil
		},
		SessionPaneSnapshotInfo: func(sessionName string, opts tmux.Options) (int, int, bool, error) {
			return 91, 27, true, nil
		},
		ResizePaneToSize: func(sessionName string, cols, rows int, opts tmux.Options) error {
			return nil
		},
		CapturePaneSnapshot: func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
			time.Sleep(captureDelay)
			return tmux.PaneSnapshot{Data: []byte("frame"), Cols: 80, Rows: 24}, nil
		},
	})

	if !bootstrap.CaptureFullPane {
		t.Fatalf("expected bootstrap snapshot to succeed, got %+v", bootstrap)
	}
	age := time.Since(bootstrap.SnapshotCaptured)
	if age < captureDelay {
		t.Fatalf("expected freshness timing to start before snapshot capture finished, got age %s for delay %s", age, captureDelay)
	}
}

func TestCaptureExistingSessionBootstrap_SkipsUnknownViewportSize(t *testing.T) {
	calls := make([]string, 0, 4)
	bootstrap := CaptureExistingSessionBootstrap("session-race", 0, 0, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		SessionHasClients: func(sessionName string, opts tmux.Options) (bool, error) {
			calls = append(calls, "clients")
			return false, nil
		},
		SessionActiveWithin: func(sessionName string, quietWindow time.Duration, opts tmux.Options) (bool, error) {
			calls = append(calls, "activity")
			return false, nil
		},
		SessionCreatedAt: func(sessionName string, opts tmux.Options) (int64, error) {
			calls = append(calls, "created")
			return 123, nil
		},
		SessionPaneID: func(sessionName string, opts tmux.Options) (string, error) {
			calls = append(calls, "pane")
			return "%1", nil
		},
		SessionPaneSnapshotInfo: func(sessionName string, opts tmux.Options) (int, int, bool, error) {
			calls = append(calls, "info")
			return 91, 27, true, nil
		},
		ResizePaneToSize: func(sessionName string, cols, rows int, opts tmux.Options) error {
			calls = append(calls, "resize")
			return nil
		},
		CapturePaneSnapshot: func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
			calls = append(calls, "snapshot")
			return tmux.PaneSnapshot{Data: []byte("frame")}, nil
		},
	})

	if bootstrap.CaptureFullPane {
		t.Fatal("expected unknown viewport size to skip full-pane bootstrap")
	}
	if len(calls) != 0 {
		t.Fatalf("expected unknown viewport size to avoid tmux bootstrap work, got %v", calls)
	}
}

func TestRollbackExistingSessionBootstrap_SkipsReplacementSession(t *testing.T) {
	calls := make([]string, 0, 4)
	RollbackExistingSessionBootstrap("session-race", SessionBootstrapCapture{
		SessionCreatedAt: 123,
		PaneID:           "%1",
		RollbackCols:     91,
		RollbackRows:     27,
		NeedsRollback:    true,
	}, tmux.Options{}, SessionBootstrapFns{
		SessionCreatedAt: func(sessionName string, opts tmux.Options) (int64, error) {
			calls = append(calls, "created")
			return 456, nil
		},
		SessionPaneID: func(sessionName string, opts tmux.Options) (string, error) {
			calls = append(calls, "pane")
			return "%9", nil
		},
		SessionHasClients: func(sessionName string, opts tmux.Options) (bool, error) {
			t.Fatal("did not expect client check after generation mismatch")
			return false, nil
		},
		ResizePaneToSize: func(sessionName string, cols, rows int, opts tmux.Options) error {
			t.Fatal("did not expect rollback resize for a replacement session")
			return nil
		},
	})
	if len(calls) != 2 {
		t.Fatalf("expected only generation check before skipping rollback, got %v", calls)
	}
}

func TestRollbackExistingSessionBootstrap_SkipsLiveSharedSession(t *testing.T) {
	calls := make([]string, 0, 4)
	RollbackExistingSessionBootstrap("session-race", SessionBootstrapCapture{
		SessionCreatedAt: 123,
		PaneID:           "%1",
		RollbackCols:     91,
		RollbackRows:     27,
		NeedsRollback:    true,
	}, tmux.Options{}, SessionBootstrapFns{
		SessionCreatedAt: func(sessionName string, opts tmux.Options) (int64, error) {
			calls = append(calls, "created")
			return 123, nil
		},
		SessionPaneID: func(sessionName string, opts tmux.Options) (string, error) {
			calls = append(calls, "pane")
			return "%1", nil
		},
		SessionHasClients: func(sessionName string, opts tmux.Options) (bool, error) {
			calls = append(calls, "clients")
			return true, nil
		},
		ResizePaneToSize: func(sessionName string, cols, rows int, opts tmux.Options) error {
			t.Fatal("did not expect rollback resize for a shared session")
			return nil
		},
	})
	if len(calls) != 3 {
		t.Fatalf("expected generation and client checks before skipping rollback, got %v", calls)
	}
}

func TestBootstrapSnapshotStillMatchesSession_RejectsRecentActivitySinceCapture(t *testing.T) {
	calls := make([]string, 0, 4)
	ok := BootstrapSnapshotStillMatchesSession("session-race", SessionBootstrapCapture{
		Snapshot:         tmux.PaneSnapshot{Cols: 91, Rows: 27},
		CaptureFullPane:  true,
		SnapshotCaptured: time.Now().Add(-5 * time.Second),
		SessionCreatedAt: 123,
		PaneID:           "%1",
	}, tmux.Options{}, SessionBootstrapFns{
		SessionCreatedAt: func(sessionName string, opts tmux.Options) (int64, error) {
			calls = append(calls, "created")
			return 123, nil
		},
		SessionPaneID: func(sessionName string, opts tmux.Options) (string, error) {
			calls = append(calls, "pane")
			return "%1", nil
		},
		SessionPaneSize: func(sessionName string, opts tmux.Options) (int, int, bool, error) {
			calls = append(calls, "size")
			return 91, 27, true, nil
		},
		SessionActiveWithin: func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
			calls = append(calls, "activity")
			if window < 4*time.Second {
				t.Fatalf("expected activity recheck window to cover time since snapshot, got %s", window)
			}
			return true, nil
		},
	})
	if ok {
		t.Fatal("expected snapshot to be rejected once the pane became active after capture")
	}
	if len(calls) != 4 {
		t.Fatalf("expected generation, size, and activity recheck, got %v", calls)
	}
}

func TestBootstrapSnapshotStillMatchesSession_IgnoresSameSecondActivityBeforeSnapshot(t *testing.T) {
	ok := BootstrapSnapshotStillMatchesSession("session-race", SessionBootstrapCapture{
		Snapshot:         tmux.PaneSnapshot{Cols: 91, Rows: 27},
		CaptureFullPane:  true,
		SnapshotCaptured: time.Unix(12, 900_000_000),
		SessionCreatedAt: 123,
		PaneID:           "%1",
	}, tmux.Options{}, SessionBootstrapFns{
		SessionCreatedAt: func(sessionName string, opts tmux.Options) (int64, error) {
			return 123, nil
		},
		SessionPaneID: func(sessionName string, opts tmux.Options) (string, error) {
			return "%1", nil
		},
		SessionPaneSize: func(sessionName string, opts tmux.Options) (int, int, bool, error) {
			return 91, 27, true, nil
		},
		SessionClientCount: func(sessionName string, opts tmux.Options) (int, error) {
			return 1, nil
		},
		SessionLatestActivity: func(sessionName string, opts tmux.Options) (time.Time, bool, error) {
			return time.Unix(12, 0), true, nil
		},
		SessionActiveWithin: func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
			t.Fatal("did not expect padded activity-window check when raw activity timestamp is available")
			return false, nil
		},
	})
	if !ok {
		t.Fatal("expected same-second activity before snapshot to keep the snapshot valid")
	}
}

func TestBootstrapSnapshotStillMatchesSession_RejectsLaterSecondActivitySinceCapture(t *testing.T) {
	ok := BootstrapSnapshotStillMatchesSession("session-race", SessionBootstrapCapture{
		Snapshot:         tmux.PaneSnapshot{Cols: 91, Rows: 27},
		CaptureFullPane:  true,
		SnapshotCaptured: time.Unix(12, 100_000_000),
		SessionCreatedAt: 123,
		PaneID:           "%1",
	}, tmux.Options{}, SessionBootstrapFns{
		SessionCreatedAt: func(sessionName string, opts tmux.Options) (int64, error) {
			return 123, nil
		},
		SessionPaneID: func(sessionName string, opts tmux.Options) (string, error) {
			return "%1", nil
		},
		SessionPaneSize: func(sessionName string, opts tmux.Options) (int, int, bool, error) {
			return 91, 27, true, nil
		},
		SessionClientCount: func(sessionName string, opts tmux.Options) (int, error) {
			return 1, nil
		},
		SessionLatestActivity: func(sessionName string, opts tmux.Options) (time.Time, bool, error) {
			return time.Unix(13, 0), true, nil
		},
		SessionActiveWithin: func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
			t.Fatal("did not expect padded activity-window check when raw activity timestamp is available")
			return false, nil
		},
	})
	if ok {
		t.Fatal("expected later-second activity after capture to invalidate the snapshot")
	}
}

func TestBootstrapSnapshotStillMatchesSession_RejectsSharedSessionAfterCapture(t *testing.T) {
	calls := make([]string, 0, 4)
	ok := BootstrapSnapshotStillMatchesSession("session-race", SessionBootstrapCapture{
		Snapshot:         tmux.PaneSnapshot{Cols: 91, Rows: 27},
		CaptureFullPane:  true,
		SnapshotCaptured: time.Now().Add(-5 * time.Second),
		SessionCreatedAt: 123,
		PaneID:           "%1",
	}, tmux.Options{}, SessionBootstrapFns{
		SessionCreatedAt: func(sessionName string, opts tmux.Options) (int64, error) {
			calls = append(calls, "created")
			return 123, nil
		},
		SessionPaneID: func(sessionName string, opts tmux.Options) (string, error) {
			calls = append(calls, "pane")
			return "%1", nil
		},
		SessionPaneSize: func(sessionName string, opts tmux.Options) (int, int, bool, error) {
			calls = append(calls, "size")
			return 91, 27, true, nil
		},
		SessionClientCount: func(sessionName string, opts tmux.Options) (int, error) {
			calls = append(calls, "clients")
			return 2, nil
		},
		SessionActiveWithin: func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
			t.Fatal("did not expect activity check after session became shared")
			return false, nil
		},
	})
	if ok {
		t.Fatal("expected snapshot to be rejected once another tmux client attached")
	}
	if len(calls) != 4 {
		t.Fatalf("expected generation, size, and shared-session recheck, got %v", calls)
	}
}

func TestBootstrapSnapshotStillMatchesSession_RejectsPaneSizeChangeAfterCapture(t *testing.T) {
	calls := make([]string, 0, 3)
	ok := BootstrapSnapshotStillMatchesSession("session-race", SessionBootstrapCapture{
		Snapshot:         tmux.PaneSnapshot{Cols: 91, Rows: 27},
		CaptureFullPane:  true,
		SnapshotCaptured: time.Now().Add(-5 * time.Second),
		SessionCreatedAt: 123,
		PaneID:           "%1",
	}, tmux.Options{}, SessionBootstrapFns{
		SessionCreatedAt: func(sessionName string, opts tmux.Options) (int64, error) {
			calls = append(calls, "created")
			return 123, nil
		},
		SessionPaneID: func(sessionName string, opts tmux.Options) (string, error) {
			calls = append(calls, "pane")
			return "%1", nil
		},
		SessionPaneSize: func(sessionName string, opts tmux.Options) (int, int, bool, error) {
			calls = append(calls, "size")
			return 120, 40, true, nil
		},
		SessionClientCount: func(sessionName string, opts tmux.Options) (int, error) {
			t.Fatal("did not expect shared-session check after size drift")
			return 0, nil
		},
		SessionActiveWithin: func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
			t.Fatal("did not expect activity check after size drift")
			return false, nil
		},
	})
	if ok {
		t.Fatal("expected snapshot to be rejected once pane size changed after capture")
	}
	if len(calls) != 3 {
		t.Fatalf("expected generation and size recheck, got %v", calls)
	}
}
