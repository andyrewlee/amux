package tmux

import (
	"errors"
	"testing"
)

// TestPaneSizeParsesViaSeam exercises paneSize end-to-end through the runTmuxCmd
// seam. Before capture.go routed its exec through runTmuxCmd, paneSize's
// build-exec-parse path could not be unit-tested at all (only the parsePaneSize
// helper in isolation); now canned list-panes output drives the whole function
// with no live tmux server.
func TestPaneSizeParsesViaSeam(t *testing.T) {
	fakeRunTmuxCmd(t, []byte("%5\t80\t24\n"), nil)

	cols, rows, ok, err := paneSize("%5", testOpts())
	if err != nil {
		t.Fatalf("paneSize error = %v", err)
	}
	if !ok || cols != 80 || rows != 24 {
		t.Fatalf("paneSize = (%d, %d, %v), want (80, 24, true)", cols, rows, ok)
	}
}

// TestPaneSizePropagatesTmuxError confirms an exec failure surfaced by the seam
// reaches the caller rather than being silently treated as an empty pane.
func TestPaneSizePropagatesTmuxError(t *testing.T) {
	wantErr := errors.New("boom")
	fakeRunTmuxCmd(t, nil, wantErr)

	if _, _, _, err := paneSize("%5", testOpts()); !errors.Is(err, wantErr) {
		t.Fatalf("paneSize err = %v, want %v", err, wantErr)
	}
}

// TestCapturePaneSnapshotDataViaSeam confirms the unexported snapshot capture
// helper now returns exactly what the seam yields, proving the capture path is
// reachable in tests without spawning tmux.
func TestCapturePaneSnapshotDataViaSeam(t *testing.T) {
	want := []byte("line one\x1b[0m\nline two\n")
	fakeRunTmuxCmd(t, want, nil)

	got, err := capturePaneSnapshotData("%9", testOpts())
	if err != nil {
		t.Fatalf("capturePaneSnapshotData error = %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("capturePaneSnapshotData = %q, want %q", got, want)
	}
}
