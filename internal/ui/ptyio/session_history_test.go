package ptyio

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestSessionHistoryCaptureSize(t *testing.T) {
	tests := []struct {
		name         string
		fallbackCols int
		fallbackRows int
		paneSize     func(string, tmux.Options) (int, int, bool, error)
		wantCols     int
		wantRows     int
	}{
		{
			name:         "live pane size overrides fallback",
			fallbackCols: 80,
			fallbackRows: 24,
			paneSize: func(string, tmux.Options) (int, int, bool, error) {
				return 120, 40, true, nil
			},
			wantCols: 120,
			wantRows: 40,
		},
		{
			name:         "missing pane size falls back",
			fallbackCols: 80,
			fallbackRows: 24,
			paneSize: func(string, tmux.Options) (int, int, bool, error) {
				return 0, 0, false, nil
			},
			wantCols: 80,
			wantRows: 24,
		},
		{
			name:         "pane size error falls back",
			fallbackCols: 100,
			fallbackRows: 30,
			paneSize: func(string, tmux.Options) (int, int, bool, error) {
				return 120, 40, true, errors.New("boom")
			},
			wantCols: 100,
			wantRows: 30,
		},
		{
			name:         "non-positive cols falls back",
			fallbackCols: 100,
			fallbackRows: 30,
			paneSize: func(string, tmux.Options) (int, int, bool, error) {
				return 0, 40, true, nil
			},
			wantCols: 100,
			wantRows: 30,
		},
		{
			name:         "non-positive rows falls back",
			fallbackCols: 100,
			fallbackRows: 30,
			paneSize: func(string, tmux.Options) (int, int, bool, error) {
				return 120, 0, true, nil
			},
			wantCols: 100,
			wantRows: 30,
		},
		{
			name:         "negative live size falls back",
			fallbackCols: 64,
			fallbackRows: 16,
			paneSize: func(string, tmux.Options) (int, int, bool, error) {
				return -1, -1, true, nil
			},
			wantCols: 64,
			wantRows: 16,
		},
		{
			name:         "zero fallback preserved when pane size unavailable",
			fallbackCols: 0,
			fallbackRows: 0,
			paneSize: func(string, tmux.Options) (int, int, bool, error) {
				return 0, 0, false, nil
			},
			wantCols: 0,
			wantRows: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCols, gotRows := SessionHistoryCaptureSize("session-history", tt.fallbackCols, tt.fallbackRows, tmux.Options{}, SessionBootstrapFns{
				SessionPaneSize: tt.paneSize,
			})
			if gotCols != tt.wantCols || gotRows != tt.wantRows {
				t.Fatalf("SessionHistoryCaptureSize = (%d, %d), want (%d, %d)", gotCols, gotRows, tt.wantCols, tt.wantRows)
			}
		})
	}
}

func TestCaptureSessionHistory_UsesLivePaneSize(t *testing.T) {
	var capturedSession string
	wantData := []byte("scrollback frame")
	data, cols, rows := CaptureSessionHistory("session-history", 80, 24, tmux.Options{}, SessionBootstrapFns{
		SessionPaneSize: func(string, tmux.Options) (int, int, bool, error) {
			return 120, 40, true, nil
		},
	}, func(sessionName string, opts tmux.Options) ([]byte, error) {
		capturedSession = sessionName
		return wantData, nil
	})

	if capturedSession != "session-history" {
		t.Fatalf("capturePane received session %q, want %q", capturedSession, "session-history")
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
		SessionPaneSize: func(string, tmux.Options) (int, int, bool, error) {
			return 0, 0, false, nil
		},
	}, func(string, tmux.Options) ([]byte, error) {
		return []byte("frame"), nil
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
		SessionPaneSize: func(string, tmux.Options) (int, int, bool, error) {
			return 100, 30, true, nil
		},
	}, func(string, tmux.Options) ([]byte, error) {
		return []byte("partial"), errors.New("capture failed")
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
		SessionPaneSize: func(string, tmux.Options) (int, int, bool, error) {
			return 0, 0, false, nil
		},
	}, func(string, tmux.Options) ([]byte, error) {
		return nil, nil
	})

	if data != nil {
		t.Fatalf("CaptureSessionHistory data = %q, want nil for empty capture", data)
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
	rollbackSessionBootstrap("session-history", SessionBootstrapCapture{
		SessionCreatedAt: 123,
		PaneID:           "%1",
		RollbackCols:     91,
		RollbackRows:     27,
		NeedsRollback:    true,
	}, tmux.Options{}, SessionBootstrapFns{
		SessionCreatedAt: func(string, tmux.Options) (int64, error) {
			return 123, nil
		},
		SessionPaneID: func(string, tmux.Options) (string, error) {
			return "%1", nil
		},
		SessionHasClients: func(string, tmux.Options) (bool, error) {
			return false, nil
		},
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
				SessionCreatedAt: func(string, tmux.Options) (int64, error) {
					t.Fatal("did not expect a generation check when rollback is unnecessary")
					return 0, nil
				},
				ResizePaneToSize: func(string, int, int, tmux.Options) error {
					t.Fatal("did not expect a resize when rollback is unnecessary")
					return nil
				},
			})
		})
	}
}

func TestRollbackSessionBootstrap_SkipsWhenGenerationLookupFails(t *testing.T) {
	rollbackSessionBootstrap("session-history", SessionBootstrapCapture{
		SessionCreatedAt: 123,
		PaneID:           "%1",
		RollbackCols:     91,
		RollbackRows:     27,
		NeedsRollback:    true,
	}, tmux.Options{}, SessionBootstrapFns{
		SessionCreatedAt: func(string, tmux.Options) (int64, error) {
			return 0, errors.New("session gone")
		},
		SessionPaneID: func(string, tmux.Options) (string, error) {
			return "%1", nil
		},
		SessionHasClients: func(string, tmux.Options) (bool, error) {
			t.Fatal("did not expect a client check after generation lookup failed")
			return false, nil
		},
		ResizePaneToSize: func(string, int, int, tmux.Options) error {
			t.Fatal("did not expect a resize after generation lookup failed")
			return nil
		},
	})
}

func TestRollbackSessionBootstrap_SkipsWhenClientCheckErrors(t *testing.T) {
	rollbackSessionBootstrap("session-history", SessionBootstrapCapture{
		SessionCreatedAt: 123,
		PaneID:           "%1",
		RollbackCols:     91,
		RollbackRows:     27,
		NeedsRollback:    true,
	}, tmux.Options{}, SessionBootstrapFns{
		SessionCreatedAt: func(string, tmux.Options) (int64, error) {
			return 123, nil
		},
		SessionPaneID: func(string, tmux.Options) (string, error) {
			return "%1", nil
		},
		SessionHasClients: func(string, tmux.Options) (bool, error) {
			return false, errors.New("query failed")
		},
		ResizePaneToSize: func(string, int, int, tmux.Options) error {
			t.Fatal("did not expect a resize when the client check errored")
			return nil
		},
	})
}
