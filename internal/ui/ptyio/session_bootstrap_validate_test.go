package ptyio

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// These cover the two guards that run after the pre-attach snapshot has already
// been taken: the rollback that undoes the bootstrap resize, and the
// post-attach validation deciding whether the snapshot is still an accurate
// picture of the session. Shared fixtures (eligibleProbe, probeScript) live in
// session_bootstrap_test.go.

func TestRollbackExistingSessionBootstrap_SkipsReplacementSession(t *testing.T) {
	var calls []string
	replacement := eligibleProbe()
	replacement.CreatedAt = 456
	replacement.PaneID = "%9"

	RollbackExistingSessionBootstrap("session-race", SessionBootstrapCapture{
		SessionCreatedAt: 123,
		PaneID:           "%1",
		RollbackCols:     91,
		RollbackRows:     27,
		NeedsRollback:    true,
	}, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, replacement),
		ResizePaneToSize: func(string, int, int, tmux.Options) error {
			t.Fatal("did not expect rollback resize for a replacement session")
			return nil
		},
	})
	if len(calls) != 1 {
		t.Fatalf("expected a single probe before skipping rollback, got %v", calls)
	}
}

func TestRollbackExistingSessionBootstrap_SkipsLiveSharedSession(t *testing.T) {
	var calls []string
	attached := eligibleProbe()
	attached.ClientCount = 1

	RollbackExistingSessionBootstrap("session-race", SessionBootstrapCapture{
		SessionCreatedAt: 123,
		PaneID:           "%1",
		RollbackCols:     91,
		RollbackRows:     27,
		NeedsRollback:    true,
	}, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, attached),
		ResizePaneToSize: func(string, int, int, tmux.Options) error {
			t.Fatal("did not expect rollback resize for a shared session")
			return nil
		},
	})
	if len(calls) != 1 {
		t.Fatalf("expected a single probe before skipping rollback, got %v", calls)
	}
}

func TestBootstrapSnapshotStillMatchesSession_AcceptsUnchangedSession(t *testing.T) {
	var calls []string
	// One client is the attaching client itself, which is expected.
	attached := eligibleProbe()
	attached.ClientCount = 1

	ok := BootstrapSnapshotStillMatchesSession("session-race", SessionBootstrapCapture{
		Snapshot:         tmux.PaneSnapshot{Cols: 91, Rows: 27},
		CaptureFullPane:  true,
		SnapshotCaptured: time.Now(),
		SessionCreatedAt: 123,
		PaneID:           "%1",
	}, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, attached),
	})
	if !ok {
		t.Fatal("expected an unchanged session with only the attaching client to keep the snapshot")
	}
}

func TestBootstrapSnapshotStillMatchesSession_RejectsRecentActivitySinceCapture(t *testing.T) {
	var calls []string
	active := eligibleProbe()
	active.ClientCount = 1
	active.LatestActivity = time.Now().Unix()

	ok := BootstrapSnapshotStillMatchesSession("session-race", SessionBootstrapCapture{
		Snapshot:         tmux.PaneSnapshot{Cols: 91, Rows: 27},
		CaptureFullPane:  true,
		SnapshotCaptured: time.Now().Add(-5 * time.Second),
		SessionCreatedAt: 123,
		PaneID:           "%1",
	}, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, active),
	})
	if ok {
		t.Fatal("expected snapshot to be rejected once the pane became active after capture")
	}
}

func TestBootstrapSnapshotStillMatchesSession_IgnoresSameSecondActivityBeforeSnapshot(t *testing.T) {
	var calls []string
	sameSecond := eligibleProbe()
	sameSecond.ClientCount = 1
	sameSecond.LatestActivity = 12

	ok := BootstrapSnapshotStillMatchesSession("session-race", SessionBootstrapCapture{
		Snapshot:         tmux.PaneSnapshot{Cols: 91, Rows: 27},
		CaptureFullPane:  true,
		SnapshotCaptured: time.Unix(12, 900_000_000),
		SessionCreatedAt: 123,
		PaneID:           "%1",
	}, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, sameSecond),
	})
	if !ok {
		t.Fatal("expected same-second activity before snapshot to keep the snapshot valid")
	}
}

func TestBootstrapSnapshotStillMatchesSession_RejectsLaterSecondActivitySinceCapture(t *testing.T) {
	var calls []string
	laterSecond := eligibleProbe()
	laterSecond.ClientCount = 1
	laterSecond.LatestActivity = 13

	ok := BootstrapSnapshotStillMatchesSession("session-race", SessionBootstrapCapture{
		Snapshot:         tmux.PaneSnapshot{Cols: 91, Rows: 27},
		CaptureFullPane:  true,
		SnapshotCaptured: time.Unix(12, 100_000_000),
		SessionCreatedAt: 123,
		PaneID:           "%1",
	}, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, laterSecond),
	})
	if ok {
		t.Fatal("expected later-second activity after capture to invalidate the snapshot")
	}
}

func TestBootstrapSnapshotStillMatchesSession_RejectsSharedSessionAfterCapture(t *testing.T) {
	var calls []string
	// Two clients: one is the attaching client, the other is something else
	// driving the session.
	shared := eligibleProbe()
	shared.ClientCount = 2

	ok := BootstrapSnapshotStillMatchesSession("session-race", SessionBootstrapCapture{
		Snapshot:         tmux.PaneSnapshot{Cols: 91, Rows: 27},
		CaptureFullPane:  true,
		SnapshotCaptured: time.Now().Add(-5 * time.Second),
		SessionCreatedAt: 123,
		PaneID:           "%1",
	}, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, shared),
	})
	if ok {
		t.Fatal("expected snapshot to be rejected once another tmux client attached")
	}
}

func TestBootstrapSnapshotStillMatchesSession_RejectsPaneSizeChangeAfterCapture(t *testing.T) {
	var calls []string
	resized := eligibleProbe()
	resized.ClientCount = 1
	resized.PaneMeta.Cols, resized.PaneMeta.Rows = 120, 40
	resized.PaneCols, resized.PaneRows = 120, 40

	ok := BootstrapSnapshotStillMatchesSession("session-race", SessionBootstrapCapture{
		Snapshot:         tmux.PaneSnapshot{Cols: 91, Rows: 27},
		CaptureFullPane:  true,
		SnapshotCaptured: time.Now().Add(-5 * time.Second),
		SessionCreatedAt: 123,
		PaneID:           "%1",
	}, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, resized),
	})
	if ok {
		t.Fatal("expected snapshot to be rejected once pane size changed after capture")
	}
}

func TestBootstrapSnapshotStillMatchesSession_RejectsReplacementSession(t *testing.T) {
	var calls []string
	replacement := eligibleProbe()
	replacement.ClientCount = 1
	replacement.CreatedAt = 456
	replacement.PaneID = "%9"

	ok := BootstrapSnapshotStillMatchesSession("session-race", SessionBootstrapCapture{
		Snapshot:         tmux.PaneSnapshot{Cols: 91, Rows: 27},
		CaptureFullPane:  true,
		SnapshotCaptured: time.Now(),
		SessionCreatedAt: 123,
		PaneID:           "%1",
	}, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, replacement),
	})
	if ok {
		t.Fatal("expected a recreated session to invalidate the snapshot")
	}
}
