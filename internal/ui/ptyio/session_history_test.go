package ptyio

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/tmux"
)

// sizedProbe returns a probe reporting the given live pane size. A zero size
// stands for "tmux could not tell us", which is what makes the caller's fallback
// dimensions apply.
func sizedProbe(cols, rows int) tmux.SessionProbe {
	p := tmux.SessionProbe{Exists: true, CreatedAt: 1, PaneID: "%1", HasLivePane: true}
	if cols > 0 && rows > 0 {
		p.PaneCols, p.PaneRows, p.HasPaneSize = cols, rows, true
		p.PaneMeta = tmux.PaneSnapshotMeta{Cols: cols, Rows: rows, HasSize: true}
	}
	return p
}

func staticProbe(p tmux.SessionProbe) func(string, tmux.Options) (tmux.SessionProbe, error) {
	return func(string, tmux.Options) (tmux.SessionProbe, error) { return p, nil }
}

func TestSessionHistoryCaptureSize(t *testing.T) {
	tests := []struct {
		name         string
		fallbackCols int
		fallbackRows int
		probe        func(string, tmux.Options) (tmux.SessionProbe, error)
		wantCols     int
		wantRows     int
	}{
		{
			name:         "live pane size overrides fallback",
			fallbackCols: 80,
			fallbackRows: 24,
			probe:        staticProbe(sizedProbe(120, 40)),
			wantCols:     120,
			wantRows:     40,
		},
		{
			name:         "missing pane size falls back",
			fallbackCols: 80,
			fallbackRows: 24,
			probe:        staticProbe(sizedProbe(0, 0)),
			wantCols:     80,
			wantRows:     24,
		},
		{
			name:         "probe error falls back",
			fallbackCols: 100,
			fallbackRows: 30,
			probe: func(string, tmux.Options) (tmux.SessionProbe, error) {
				return sizedProbe(120, 40), errors.New("boom")
			},
			wantCols: 100,
			wantRows: 30,
		},
		{
			name:         "non-positive cols falls back",
			fallbackCols: 100,
			fallbackRows: 30,
			probe: staticProbe(tmux.SessionProbe{
				Exists: true, PaneID: "%1", HasPaneSize: true, PaneCols: 0, PaneRows: 40,
			}),
			wantCols: 100,
			wantRows: 30,
		},
		{
			name:         "non-positive rows falls back",
			fallbackCols: 100,
			fallbackRows: 30,
			probe: staticProbe(tmux.SessionProbe{
				Exists: true, PaneID: "%1", HasPaneSize: true, PaneCols: 120, PaneRows: 0,
			}),
			wantCols: 100,
			wantRows: 30,
		},
		{
			name:         "negative live size falls back",
			fallbackCols: 64,
			fallbackRows: 16,
			probe: staticProbe(tmux.SessionProbe{
				Exists: true, PaneID: "%1", HasPaneSize: true, PaneCols: -1, PaneRows: -1,
			}),
			wantCols: 64,
			wantRows: 16,
		},
		{
			name:         "zero fallback preserved when pane size unavailable",
			fallbackCols: 0,
			fallbackRows: 0,
			probe:        staticProbe(sizedProbe(0, 0)),
			wantCols:     0,
			wantRows:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCols, gotRows := SessionHistoryCaptureSize("session-history", tt.fallbackCols, tt.fallbackRows, tmux.Options{}, SessionBootstrapFns{
				ProbeSession: tt.probe,
			})
			if gotCols != tt.wantCols || gotRows != tt.wantRows {
				t.Fatalf("SessionHistoryCaptureSize = (%d, %d), want (%d, %d)", gotCols, gotRows, tt.wantCols, tt.wantRows)
			}
		})
	}
}

func TestCaptureSessionHistory_UsesLivePaneSize(t *testing.T) {
	var capturedPane string
	wantData := []byte("scrollback frame")
	data, cols, rows := CaptureSessionHistory("session-history", 80, 24, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: staticProbe(sizedProbe(120, 40)),
		CapturePaneHistoryData: func(paneID string, opts tmux.Options) ([]byte, error) {
			capturedPane = paneID
			return wantData, nil
		},
	})

	// The capture targets the pane the same probe resolved, so it cannot land on
	// a different pane than the size it is interpreted at.
	if capturedPane != "%1" {
		t.Fatalf("capture targeted pane %q, want the probed pane %q", capturedPane, "%1")
	}
	if string(data) != string(wantData) {
		t.Fatalf("CaptureSessionHistory data = %q, want %q", data, wantData)
	}
	if cols != 120 || rows != 40 {
		t.Fatalf("CaptureSessionHistory size = (%d, %d), want live (120, 40)", cols, rows)
	}
}

func TestCaptureSessionHistory_FallsBackToProvidedSize(t *testing.T) {
	data, cols, rows := CaptureSessionHistory("session-history", 90, 28, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: staticProbe(sizedProbe(0, 0)),
		CapturePaneHistoryData: func(string, tmux.Options) ([]byte, error) {
			return []byte("frame"), nil
		},
	})

	if cols != 90 || rows != 28 {
		t.Fatalf("CaptureSessionHistory size = (%d, %d), want fallback (90, 28)", cols, rows)
	}
	if string(data) != "frame" {
		t.Fatalf("CaptureSessionHistory data = %q, want %q", data, "frame")
	}
}

func TestCaptureSessionHistory_SwallowsCaptureErrorAndReturnsPartialData(t *testing.T) {
	data, cols, rows := CaptureSessionHistory("session-history", 80, 24, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: staticProbe(sizedProbe(100, 30)),
		CapturePaneHistoryData: func(string, tmux.Options) ([]byte, error) {
			return []byte("partial"), errors.New("capture failed")
		},
	})

	// The capture error is intentionally swallowed; whatever bytes the callback
	// returned alongside the error are still surfaced, with sizing intact.
	if string(data) != "partial" {
		t.Fatalf("CaptureSessionHistory data = %q, want partial data even on error", data)
	}
	if cols != 100 || rows != 30 {
		t.Fatalf("CaptureSessionHistory size = (%d, %d), want live (100, 30)", cols, rows)
	}
}

func TestCaptureSessionHistory_NilScrollbackOnEmptyCapture(t *testing.T) {
	data, cols, rows := CaptureSessionHistory("session-history", 70, 20, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: staticProbe(sizedProbe(0, 0)),
		CapturePaneHistoryData: func(string, tmux.Options) ([]byte, error) {
			return nil, nil
		},
	})

	if data != nil {
		t.Fatalf("CaptureSessionHistory data = %q, want nil for empty capture", data)
	}
	if cols != 70 || rows != 20 {
		t.Fatalf("CaptureSessionHistory size = (%d, %d), want fallback (70, 20)", cols, rows)
	}
}

func TestCaptureSessionHistory_SkipsCaptureWhenNoPaneResolved(t *testing.T) {
	data, cols, rows := CaptureSessionHistory("session-history", 70, 20, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: staticProbe(tmux.SessionProbe{}),
		CapturePaneHistoryData: func(string, tmux.Options) ([]byte, error) {
			t.Fatal("did not expect a capture when the probe resolved no pane")
			return nil, nil
		},
	})

	if data != nil {
		t.Fatalf("CaptureSessionHistory data = %q, want nil when no pane was resolved", data)
	}
	if cols != 70 || rows != 20 {
		t.Fatalf("CaptureSessionHistory size = (%d, %d), want fallback (70, 20)", cols, rows)
	}
}

func TestRollbackSessionBootstrap_RestoresOriginalSizeWhenSessionUnchanged(t *testing.T) {
	var (
		resizeCols, resizeRows int
		resizeCalled           bool
	)
	unchanged := sizedProbe(80, 24)
	unchanged.CreatedAt = 123

	rollbackSessionBootstrap("session-history", SessionBootstrapCapture{
		SessionCreatedAt: 123,
		PaneID:           "%1",
		RollbackCols:     91,
		RollbackRows:     27,
		NeedsRollback:    true,
	}, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: staticProbe(unchanged),
		ResizePaneToSize: func(sessionName string, cols, rows int, opts tmux.Options) error {
			resizeCalled = true
			resizeCols, resizeRows = cols, rows
			return nil
		},
	})

	if !resizeCalled {
		t.Fatal("expected rollback to resize the pane back to its original size")
	}
	if resizeCols != 91 || resizeRows != 27 {
		t.Fatalf("rollback resized to (%d, %d), want recorded rollback size (91, 27)", resizeCols, resizeRows)
	}
}

func TestRollbackSessionBootstrap_SkipsWhenRollbackNotNeeded(t *testing.T) {
	tests := []struct {
		name      string
		bootstrap SessionBootstrapCapture
	}{
		{
			name: "needs rollback false",
			bootstrap: SessionBootstrapCapture{
				SessionCreatedAt: 123,
				PaneID:           "%1",
				RollbackCols:     91,
				RollbackRows:     27,
				NeedsRollback:    false,
			},
		},
		{
			name: "non-positive rollback cols",
			bootstrap: SessionBootstrapCapture{
				SessionCreatedAt: 123,
				PaneID:           "%1",
				RollbackCols:     0,
				RollbackRows:     27,
				NeedsRollback:    true,
			},
		},
		{
			name: "non-positive rollback rows",
			bootstrap: SessionBootstrapCapture{
				SessionCreatedAt: 123,
				PaneID:           "%1",
				RollbackCols:     91,
				RollbackRows:     0,
				NeedsRollback:    true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rollbackSessionBootstrap("session-history", tt.bootstrap, tmux.Options{}, SessionBootstrapFns{
				ProbeSession: func(string, tmux.Options) (tmux.SessionProbe, error) {
					t.Fatal("did not expect a probe when rollback is unnecessary")
					return tmux.SessionProbe{}, nil
				},
				ResizePaneToSize: func(string, int, int, tmux.Options) error {
					t.Fatal("did not expect a resize when rollback is unnecessary")
					return nil
				},
			})
		})
	}
}

func TestRollbackSessionBootstrap_SkipsWhenProbeFails(t *testing.T) {
	rollbackSessionBootstrap("session-history", SessionBootstrapCapture{
		SessionCreatedAt: 123,
		PaneID:           "%1",
		RollbackCols:     91,
		RollbackRows:     27,
		NeedsRollback:    true,
	}, tmux.Options{}, SessionBootstrapFns{
		ProbeSession: func(string, tmux.Options) (tmux.SessionProbe, error) {
			return tmux.SessionProbe{}, errors.New("session gone")
		},
		ResizePaneToSize: func(string, int, int, tmux.Options) error {
			t.Fatal("did not expect a resize after the probe failed")
			return nil
		},
	})
}
