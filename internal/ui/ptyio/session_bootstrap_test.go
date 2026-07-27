package ptyio

import (
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// eligibleProbe is a session probe that passes every bootstrap gate: detached,
// quiet, single live pane, complete metadata. Tests start from it and change the
// one fact under test.
func eligibleProbe() tmux.SessionProbe {
	return tmux.SessionProbe{
		Exists:           true,
		CreatedAt:        123,
		ClientCount:      0,
		PaneID:           "%1",
		PaneCols:         91,
		PaneRows:         27,
		HasPaneSize:      true,
		HasLivePane:      true,
		SinglePaneWindow: true,
		PaneMeta: tmux.PaneSnapshotMeta{
			Cols:      91,
			Rows:      27,
			HasSize:   true,
			ModeState: tmux.PaneModeState{HasState: true},
		},
	}
}

// probeScript returns a ProbeSession fn that hands out the given probes in
// order, repeating the last one once exhausted, and records each call in calls.
// The bootstrap's guards are a sequence of point-in-time reads, so scripting the
// probes is how a test says "the session changed at step N".
func probeScript(calls *[]string, probes ...tmux.SessionProbe) func(string, tmux.Options) (tmux.SessionProbe, error) {
	i := 0
	return func(string, tmux.Options) (tmux.SessionProbe, error) {
		*calls = append(*calls, "probe")
		p := probes[i]
		if i < len(probes)-1 {
			i++
		}
		return p, nil
	}
}

func TestCaptureExistingSessionBootstrap_CapturesQuietDetachedSession(t *testing.T) {
	var calls []string
	var resizedCols, resizedRows int
	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, eligibleProbe()),
		ResizePaneToSize: func(_ string, cols, rows int, _ tmux.Options) error {
			calls = append(calls, "resize")
			resizedCols, resizedRows = cols, rows
			return nil
		},
		CapturePaneFullData: func(paneID string, _ tmux.Options) ([]byte, error) {
			calls = append(calls, "capture")
			if paneID != "%1" {
				t.Fatalf("capture targeted pane %q, want the probed pane %q", paneID, "%1")
			}
			return []byte("frame"), nil
		},
	})

	if !bootstrap.CaptureFullPane {
		t.Fatalf("expected a quiet detached session to yield a full-pane bootstrap, got %+v", bootstrap)
	}
	if string(bootstrap.Snapshot.Data) != "frame" {
		t.Fatalf("snapshot data = %q, want %q", bootstrap.Snapshot.Data, "frame")
	}
	if resizedCols != 80 || resizedRows != 24 {
		t.Fatalf("resized to (%d, %d), want the attaching client's size (80, 24)", resizedCols, resizedRows)
	}
	// The pane size read before the resize is what a rollback must restore.
	if bootstrap.RollbackCols != 91 || bootstrap.RollbackRows != 27 {
		t.Fatalf("rollback size = (%d, %d), want the pre-resize size (91, 27)", bootstrap.RollbackCols, bootstrap.RollbackRows)
	}
	if !bootstrap.NeedsRollback {
		t.Fatal("expected a resized pane to be marked as needing rollback")
	}
}

func TestCaptureExistingSessionBootstrap_SkipsRollbackWhenClientsAppearAfterResize(t *testing.T) {
	var calls []string
	attached := eligibleProbe()
	attached.ClientCount = 1

	resizes := 0
	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		// Quiet at the first checkpoint, a client attached by the post-resize one.
		ProbeSession: probeScript(&calls, eligibleProbe(), attached),
		ResizePaneToSize: func(string, int, int, tmux.Options) error {
			calls = append(calls, "resize")
			resizes++
			return nil
		},
		CapturePaneFullData: func(string, tmux.Options) ([]byte, error) {
			t.Fatal("did not expect a capture after exclusivity was lost")
			return nil, nil
		},
	})

	if bootstrap.CaptureFullPane {
		t.Fatal("expected full-pane bootstrap to abort once another client attached")
	}
	// Exactly one resize: the rollback must not fire, because resizing a pane a
	// live client is now rendering would disrupt that client.
	if resizes != 1 {
		t.Fatalf("expected exactly one resize and no rollback, got %d resizes (calls %v)", resizes, calls)
	}
}

func TestCaptureExistingSessionBootstrap_IgnoresResizeInducedActivity(t *testing.T) {
	var calls []string
	// The resize itself bumps window_activity, so every post-resize probe reports
	// activity inside the quiet window. That must not abort the capture.
	noisy := eligibleProbe()
	noisy.LatestActivity = time.Now().Unix()

	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		ProbeSession:     probeScript(&calls, eligibleProbe(), noisy),
		ResizePaneToSize: func(string, int, int, tmux.Options) error { return nil },
		CapturePaneFullData: func(string, tmux.Options) ([]byte, error) {
			return []byte("frame"), nil
		},
	})

	if !bootstrap.CaptureFullPane {
		t.Fatalf("expected bootstrap snapshot to survive resize-induced activity, got %+v (calls %v)", bootstrap, calls)
	}
	if len(bootstrap.Snapshot.Data) == 0 {
		t.Fatalf("expected snapshot data to be preserved, got %+v", bootstrap)
	}
}

func TestCaptureExistingSessionBootstrap_SkipsActiveSession(t *testing.T) {
	var calls []string
	active := eligibleProbe()
	active.LatestActivity = time.Now().Unix()

	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, active),
		ResizePaneToSize: func(string, int, int, tmux.Options) error {
			t.Fatal("did not expect a resize on a session with recent activity")
			return nil
		},
		CapturePaneFullData: func(string, tmux.Options) ([]byte, error) {
			t.Fatal("did not expect a capture on a session with recent activity")
			return nil, nil
		},
	})
	if bootstrap.CaptureFullPane {
		t.Fatal("expected recent activity to skip the pre-attach full-pane bootstrap")
	}
}

func TestCaptureExistingSessionBootstrap_SkipsAttachedSession(t *testing.T) {
	var calls []string
	attached := eligibleProbe()
	attached.ClientCount = 1

	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, FullPaneCaptureQuietWindow, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, attached),
		ResizePaneToSize: func(string, int, int, tmux.Options) error {
			t.Fatal("did not expect a resize while another client is attached")
			return nil
		},
		CapturePaneFullData: func(string, tmux.Options) ([]byte, error) {
			t.Fatal("did not expect a capture while another client is attached")
			return nil, nil
		},
	})
	if bootstrap.CaptureFullPane {
		t.Fatal("expected an attached session to skip the pre-attach full-pane bootstrap")
	}
}

func TestCaptureExistingSessionBootstrap_SkipsSplitWindow(t *testing.T) {
	var calls []string
	// A split (or zoomed-split) window shares the resize with hidden siblings,
	// so it is never eligible for the pre-attach resize-and-snapshot.
	split := eligibleProbe()
	split.SinglePaneWindow = false

	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, split),
		ResizePaneToSize: func(string, int, int, tmux.Options) error {
			t.Fatal("did not expect a resize for a split window")
			return nil
		},
		CapturePaneFullData: func(string, tmux.Options) ([]byte, error) {
			t.Fatal("did not expect a capture for a split window")
			return nil, nil
		},
	})
	if bootstrap.CaptureFullPane {
		t.Fatal("expected a split window to skip the pre-attach full-pane bootstrap")
	}
}

func TestCaptureExistingSessionBootstrap_SkipsMissingModeState(t *testing.T) {
	var calls []string
	// Without VT mode state there is no way to replay alt-screen/scroll-region
	// state, so the snapshot would seed a subtly wrong screen.
	noMode := eligibleProbe()
	noMode.PaneMeta.ModeState = tmux.PaneModeState{}

	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, noMode),
		ResizePaneToSize: func(string, int, int, tmux.Options) error {
			t.Fatal("did not expect a resize without pane mode metadata")
			return nil
		},
		CapturePaneFullData: func(string, tmux.Options) ([]byte, error) {
			t.Fatal("did not expect a capture without pane mode metadata")
			return nil, nil
		},
	})
	if bootstrap.CaptureFullPane {
		t.Fatal("expected missing mode metadata to skip the full-pane bootstrap")
	}
}

func TestCaptureExistingSessionBootstrap_DiscardsSnapshotWhenSessionRecreated(t *testing.T) {
	var calls []string
	// Same name, new incarnation: different creation stamp and pane ID.
	replacement := eligibleProbe()
	replacement.CreatedAt = 456
	replacement.PaneID = "%9"

	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		ProbeSession:     probeScript(&calls, eligibleProbe(), replacement),
		ResizePaneToSize: func(string, int, int, tmux.Options) error { return nil },
		CapturePaneFullData: func(string, tmux.Options) ([]byte, error) {
			t.Fatal("did not expect a capture from a replacement session")
			return nil, nil
		},
	})
	if bootstrap.CaptureFullPane {
		t.Fatal("expected a recreated session to invalidate the bootstrap")
	}
}

func TestCaptureExistingSessionBootstrap_DiscardsSnapshotOnMetadataDrift(t *testing.T) {
	var calls []string
	// The pane moved between the pre-capture and post-capture checkpoints, so the
	// captured bytes describe no single coherent screen.
	drifted := eligibleProbe()
	drifted.PaneMeta.CursorY = 7

	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		ProbeSession:     probeScript(&calls, eligibleProbe(), eligibleProbe(), drifted),
		ResizePaneToSize: func(string, int, int, tmux.Options) error { return nil },
		CapturePaneFullData: func(string, tmux.Options) ([]byte, error) {
			return []byte("torn frame"), nil
		},
	})
	if bootstrap.CaptureFullPane {
		t.Fatal("expected pane metadata drift across the capture to discard the snapshot")
	}
}

func TestCaptureExistingSessionBootstrap_SkipsWhenResizeFails(t *testing.T) {
	var calls []string
	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: probeScript(&calls, eligibleProbe()),
		ResizePaneToSize: func(string, int, int, tmux.Options) error {
			return errors.New("resize failed")
		},
		CapturePaneFullData: func(string, tmux.Options) ([]byte, error) {
			t.Fatal("did not expect a capture after the resize failed")
			return nil, nil
		},
	})
	if bootstrap.CaptureFullPane {
		t.Fatal("expected a failed resize to skip the full-pane bootstrap")
	}
}

func TestCaptureExistingSessionBootstrap_SkipsWhenProbeFails(t *testing.T) {
	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: func(string, tmux.Options) (tmux.SessionProbe, error) {
			return tmux.SessionProbe{}, errors.New("no server")
		},
		ResizePaneToSize: func(string, int, int, tmux.Options) error {
			t.Fatal("did not expect a resize when the session could not be probed")
			return nil
		},
		CapturePaneFullData: func(string, tmux.Options) ([]byte, error) {
			t.Fatal("did not expect a capture when the session could not be probed")
			return nil, nil
		},
	})
	if bootstrap.CaptureFullPane {
		t.Fatal("expected an unreadable session to fall back to history replay")
	}
}

func TestCaptureExistingSessionBootstrap_StartsFreshnessWindowBeforeSnapshotReturns(t *testing.T) {
	var calls []string
	captureDelay := 25 * time.Millisecond
	bootstrap := CaptureExistingSessionBootstrap("session-race", 80, 24, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		ProbeSession:     probeScript(&calls, eligibleProbe()),
		ResizePaneToSize: func(string, int, int, tmux.Options) error { return nil },
		CapturePaneFullData: func(string, tmux.Options) ([]byte, error) {
			time.Sleep(captureDelay)
			return []byte("frame"), nil
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
	var calls []string
	bootstrap := CaptureExistingSessionBootstrap("session-race", 0, 0, 2*time.Second, tmux.Options{}, SessionBootstrapFns{
		ProbeSession:     probeScript(&calls, eligibleProbe()),
		ResizePaneToSize: func(string, int, int, tmux.Options) error { return nil },
		CapturePaneFullData: func(string, tmux.Options) ([]byte, error) {
			return []byte("frame"), nil
		},
	})

	if bootstrap.CaptureFullPane {
		t.Fatal("expected unknown viewport size to skip full-pane bootstrap")
	}
	if len(calls) != 0 {
		t.Fatalf("expected unknown viewport size to avoid tmux bootstrap work, got %v", calls)
	}
}
