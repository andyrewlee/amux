package tmux

import (
	"os/exec"
	"testing"
	"time"
)

// This file covers the pane-metadata helpers in capture.go that were missing
// dedicated coverage: the exec-free input guards for paneCursorPosition,
// paneSize, SessionPaneSnapshotInfo, SessionPaneID, SessionPaneSize and
// ResizePaneToSize, plus the subprocess-backed read-back paths for paneSize,
// SessionPaneSize and SessionPaneSnapshotInfo against an isolated tmux server.
//
// The exec-free guards (empty pane/session name, non-positive resize
// dimensions) short-circuit before any tmux command runs, so they are asserted
// as portable pure unit tests with no skipIfNoTmux gate. The subprocess paths
// reuse the same testServer/createSession harness as the sibling integration
// tests and use real read-back assertions, never bare "did not crash" bodies.
//
// The exec-only happy paths for paneCursorPosition, SessionPaneID,
// CapturePaneSnapshot and ResizePaneToSize already have dedicated coverage in
// capture_integration_test.go; this file deliberately fills the remaining gaps
// rather than duplicating them.

// ---------------------------------------------------------------------------
// Exec-free guard tests (no subprocess; portable)
// ---------------------------------------------------------------------------

func TestPaneCursorPosition_EmptyPaneIDShortCircuits(t *testing.T) {
	// An empty pane ID returns the zero cursor with ok=false before any tmux
	// command runs, so this is deterministic without a live server.
	x, y, ok, err := paneCursorPosition("", Options{})
	if err != nil {
		t.Fatalf("expected nil error for empty pane ID, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for empty pane ID, got ok=true")
	}
	if x != 0 || y != 0 {
		t.Fatalf("expected zero cursor (0,0) for empty pane ID, got (%d,%d)", x, y)
	}
}

func TestPaneSize_EmptyPaneIDShortCircuits(t *testing.T) {
	// An empty pane ID returns zero size with ok=false before any tmux command.
	cols, rows, ok, err := paneSize("", Options{})
	if err != nil {
		t.Fatalf("expected nil error for empty pane ID, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for empty pane ID, got ok=true")
	}
	if cols != 0 || rows != 0 {
		t.Fatalf("expected zero size (0x0) for empty pane ID, got %dx%d", cols, rows)
	}
}

func TestSessionPaneSnapshotInfo_EmptySessionShortCircuits(t *testing.T) {
	// An empty session name yields no snapshot info and supported=false before
	// resolving any pane.
	cols, rows, supported, err := SessionPaneSnapshotInfo("", Options{})
	if err != nil {
		t.Fatalf("expected nil error for empty session name, got %v", err)
	}
	if supported {
		t.Fatalf("expected supported=false for empty session name, got supported=true")
	}
	if cols != 0 || rows != 0 {
		t.Fatalf("expected zero size for empty session name, got %dx%d", cols, rows)
	}
}

func TestSessionPaneID_EmptySessionShortCircuits(t *testing.T) {
	// An empty session name returns an empty pane ID and no error without any
	// tmux command.
	paneID, err := SessionPaneID("", Options{})
	if err != nil {
		t.Fatalf("expected nil error for empty session name, got %v", err)
	}
	if paneID != "" {
		t.Fatalf("expected empty pane ID for empty session name, got %q", paneID)
	}
}

func TestSessionPaneSize_EmptySessionShortCircuits(t *testing.T) {
	// An empty session name returns zero size with ok=false before resolving a
	// pane.
	cols, rows, ok, err := SessionPaneSize("", Options{})
	if err != nil {
		t.Fatalf("expected nil error for empty session name, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for empty session name, got ok=true")
	}
	if cols != 0 || rows != 0 {
		t.Fatalf("expected zero size for empty session name, got %dx%d", cols, rows)
	}
}

func TestResizePaneToSize_GuardsReturnNilWithoutResizing(t *testing.T) {
	// Each of these inputs trips an exec-free guard in ResizePaneToSize and must
	// return nil without invoking tmux. A non-empty session name is used for the
	// dimension guards so it is the dimension, not the name, that short-circuits.
	tests := []struct {
		name    string
		session string
		cols    int
		rows    int
	}{
		{name: "empty session", session: "", cols: 80, rows: 24},
		{name: "zero cols", session: "sess", cols: 0, rows: 24},
		{name: "zero rows", session: "sess", cols: 80, rows: 0},
		{name: "negative cols", session: "sess", cols: -1, rows: 24},
		{name: "negative rows", session: "sess", cols: 80, rows: -1},
		{name: "empty session and zero dims", session: "", cols: 0, rows: 0},
		{name: "all zero", session: "sess", cols: 0, rows: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ResizePaneToSize(tt.session, tt.cols, tt.rows, Options{}); err != nil {
				t.Fatalf("expected nil error for guarded resize input, got %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Subprocess-backed tests (isolated tmux server)
// ---------------------------------------------------------------------------

func TestPaneSize_ReportsPositiveDimensionsForLivePane(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "pane-size-live", "sleep 300")

	// Resolve the pane ID first, then read its size back. tmux assigns a default
	// 80x24-style geometry to detached sessions; assert only the invariants the
	// function guarantees (positive, ok=true) so the test is robust across tmux
	// defaults.
	var (
		paneID string
		err    error
	)
	if !eventually(5*time.Second, func() bool {
		paneID, err = sessionPaneID("pane-size-live", opts)
		return err == nil && paneID != ""
	}) {
		t.Fatalf("sessionPaneID did not resolve: %v", err)
	}

	cols, rows, ok, err := paneSize(paneID, opts)
	if err != nil {
		t.Fatalf("paneSize: %v", err)
	}
	if !ok {
		t.Fatalf("expected paneSize to resolve for live pane %s", paneID)
	}
	if cols <= 0 || rows <= 0 {
		t.Fatalf("expected positive pane size for live pane, got %dx%d", cols, rows)
	}
}

func TestPaneSize_MissingPaneReturnsNotFound(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	// A syntactically valid but nonexistent pane ID makes tmux exit non-zero,
	// which surfaces as an error (not a silent zero result).
	cols, rows, ok, err := paneSize("%9999", opts)
	if err == nil {
		t.Fatalf("expected an error for a nonexistent pane ID, got ok=%v size=%dx%d", ok, cols, rows)
	}
	if ok {
		t.Fatalf("expected ok=false for a nonexistent pane ID, got ok=true")
	}
}

func TestSessionPaneSize_ReportsLivePaneSize(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "session-pane-size", "sleep 300")
	// Pin a known geometry so the read-back asserts a concrete value rather than
	// an environment-dependent default.
	if err := ResizePaneToSize("session-pane-size", 100, 30, opts); err != nil {
		t.Fatalf("ResizePaneToSize seed: %v", err)
	}

	var (
		cols, rows int
		ok         bool
		err        error
	)
	if !eventually(5*time.Second, func() bool {
		cols, rows, ok, err = SessionPaneSize("session-pane-size", opts)
		return err == nil && ok && cols == 100 && rows == 30
	}) {
		t.Fatalf("expected SessionPaneSize to report 100x30, got %dx%d ok=%v err=%v", cols, rows, ok, err)
	}
}

func TestSessionPaneSize_MissingSessionReportsNotFound(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	// A nonexistent session resolves to an empty pane ID, so SessionPaneSize
	// returns ok=false with no error (the empty-pane-ID guard short-circuits).
	cols, rows, ok, err := SessionPaneSize("no-such-session", opts)
	if err != nil {
		t.Fatalf("expected nil error for missing session, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for missing session, got ok=true")
	}
	if cols != 0 || rows != 0 {
		t.Fatalf("expected zero size for missing session, got %dx%d", cols, rows)
	}
}

func TestSessionPaneSnapshotInfo_ReportsEligibleSinglePane(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "snapshot-info-single", "printf hi; sleep 300")
	if err := ResizePaneToSize("snapshot-info-single", 100, 30, opts); err != nil {
		t.Fatalf("ResizePaneToSize seed: %v", err)
	}

	var (
		cols, rows int
		supported  bool
		err        error
	)
	if !eventually(5*time.Second, func() bool {
		cols, rows, supported, err = SessionPaneSnapshotInfo("snapshot-info-single", opts)
		return err == nil && supported && cols == 100 && rows == 30
	}) {
		t.Fatalf("expected single-pane snapshot info 100x30 supported=true, got %dx%d supported=%v err=%v",
			cols, rows, supported, err)
	}
}

func TestSessionPaneSnapshotInfo_RejectsMultiPaneWindow(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "snapshot-info-multi", "printf left; sleep 300")
	// A second pane in the same window means the active pane no longer covers
	// the whole window, so snapshot info must report supported=false.
	addPane(t, opts, "snapshot-info-multi", "printf right; sleep 300")
	time.Sleep(200 * time.Millisecond)

	cols, rows, supported, err := SessionPaneSnapshotInfo("snapshot-info-multi", opts)
	if err != nil {
		t.Fatalf("SessionPaneSnapshotInfo: %v", err)
	}
	if supported {
		t.Fatalf("expected supported=false for a multi-pane window, got supported=true")
	}
	if cols != 0 || rows != 0 {
		t.Fatalf("expected zero size when snapshot is unsupported, got %dx%d", cols, rows)
	}
}

func TestSessionPaneSnapshotInfo_MissingSessionReportsUnsupported(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	// A nonexistent session resolves to an empty pane ID; paneSnapshotInfoForPane
	// then reports supported=false with no error.
	cols, rows, supported, err := SessionPaneSnapshotInfo("no-such-session", opts)
	if err != nil {
		t.Fatalf("expected nil error for missing session, got %v", err)
	}
	if supported {
		t.Fatalf("expected supported=false for missing session, got supported=true")
	}
	if cols != 0 || rows != 0 {
		t.Fatalf("expected zero size for missing session, got %dx%d", cols, rows)
	}
}

func TestResizePaneToSize_MissingSessionIsNoOp(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	// hasSession returns false for a nonexistent session, so ResizePaneToSize
	// returns nil without issuing a resize-window command.
	if err := ResizePaneToSize("no-such-session", 91, 27, opts); err != nil {
		t.Fatalf("expected nil error resizing a missing session, got %v", err)
	}
}

// addPane splits the active window of an existing session, adding a second pane
// so the active pane no longer covers the whole window.
func addPane(t *testing.T, opts Options, session, command string) {
	t.Helper()
	args := tmuxArgs(opts, "split-window", "-d", "-t", session, "sh", "-c", command)
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("split-window on %s: %v\n%s", session, err, out)
	}
}
