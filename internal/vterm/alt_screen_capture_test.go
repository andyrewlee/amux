package vterm

import "testing"

// newTrackedAltCaptureVTerm builds a VTerm with a tracked alt-screen capture
// by driving the public flow: enter alt screen, draw content, erase display.
func newTrackedAltCaptureVTerm(t *testing.T) *VTerm {
	t.Helper()
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h")) // enter alt screen
	vt.Write([]byte("hello\r\nworld"))
	vt.Write([]byte("\x1b[2J")) // erase display -> captureScreenToScrollback
	if !vt.altCapture.tracked {
		t.Fatalf("setup: expected tracked alt-screen capture, got %+v", vt.altCapture)
	}
	if vt.altCapture.frameLen <= 0 || vt.altCapture.dropLen <= 0 {
		t.Fatalf("setup: expected positive frameLen/dropLen, got %+v", vt.altCapture)
	}
	return vt
}

// assertAltCaptureCleared asserts resetInvalid took the safe path: all
// tracking state is zeroed so a corrupted capture cannot touch scrollback.
func assertAltCaptureCleared(t *testing.T, vt *VTerm) {
	t.Helper()
	if vt.altCapture != (altScreenCaptureState{}) {
		t.Fatalf("expected altCapture cleared after invariant violation, got %+v", vt.altCapture)
	}
}

func snapshotScrollback(vt *VTerm) [][]Cell {
	snap := make([][]Cell, len(vt.Scrollback))
	for i, line := range vt.Scrollback {
		snap[i] = CopyLine(line)
	}
	return snap
}

func assertScrollbackUnchanged(t *testing.T, vt *VTerm, snap [][]Cell) {
	t.Helper()
	if len(vt.Scrollback) != len(snap) {
		t.Fatalf("scrollback length changed: got %d, want %d", len(vt.Scrollback), len(snap))
	}
	for i := range snap {
		if !linesEqual(vt.Scrollback[i], snap[i]) {
			t.Fatalf("scrollback line %d corrupted after invariant violation", i)
		}
	}
}

// Drives the "scrollback shrank below tracked frame" invariant violation in
// matchesTrackedAltScreenCapture (alt_screen_capture.go:142) and asserts
// resetInvalid safely cleared the tracking state.
func TestMatchesTrackedAltScreenCaptureResetInvalidOnShrunkScrollback(t *testing.T) {
	t.Parallel()
	vt := newTrackedAltCaptureVTerm(t)

	// Violate the invariant: scrollback shrinks below the tracked frame.
	total := vt.altCapture.frameLen + vt.altCapture.endOffset
	vt.Scrollback = vt.Scrollback[:total-1]
	snap := snapshotScrollback(vt)

	// A same-length frame reaches the tracked-position check.
	lines := make([][]Cell, vt.altCapture.frameLen)
	for i := range lines {
		lines[i] = MakeBlankLine(vt.Width)
	}
	matched, removed := vt.matchesTrackedAltScreenCapture(lines)

	if matched {
		t.Fatalf("expected no match after scrollback shrank below tracked frame")
	}
	if removed != 0 {
		t.Fatalf("expected no rows removed on invariant violation, got %d", removed)
	}
	assertAltCaptureCleared(t, vt)
	assertScrollbackUnchanged(t, vt, snap)
}

// Drives the "drop length out of range" invariant violation in
// dropTrackedAltScreenCapture (alt_screen_capture.go:204) and asserts
// resetInvalid safely cleared the tracking state without touching scrollback.
func TestDropTrackedAltScreenCaptureResetInvalidOnDropLenOutOfRange(t *testing.T) {
	t.Parallel()
	vt := newTrackedAltCaptureVTerm(t)

	// Violate the invariant: dropLen exceeds the tracked frame length.
	vt.altCapture.dropLen = vt.altCapture.frameLen + 1
	snap := snapshotScrollback(vt)

	removed, rows := vt.dropTrackedAltScreenCapture()

	if removed != 0 {
		t.Fatalf("expected no rows removed on invariant violation, got %d", removed)
	}
	if rows != nil {
		t.Fatalf("expected no removed rows returned, got %d", len(rows))
	}
	assertAltCaptureCleared(t, vt)
	assertScrollbackUnchanged(t, vt, snap)
}

// Drives the "scrollback shrank below tracked frame" invariant violation in
// dropTrackedAltScreenCapture (alt_screen_capture.go:209).
func TestDropTrackedAltScreenCaptureResetInvalidOnShrunkScrollback(t *testing.T) {
	t.Parallel()
	vt := newTrackedAltCaptureVTerm(t)

	total := vt.altCapture.frameLen + vt.altCapture.endOffset
	vt.Scrollback = vt.Scrollback[:total-1]
	snap := snapshotScrollback(vt)

	removed, rows := vt.dropTrackedAltScreenCapture()

	if removed != 0 {
		t.Fatalf("expected no rows removed on invariant violation, got %d", removed)
	}
	if rows != nil {
		t.Fatalf("expected no removed rows returned, got %d", len(rows))
	}
	assertAltCaptureCleared(t, vt)
	assertScrollbackUnchanged(t, vt, snap)
}

// Drives the "drop length out of range" invariant violation in
// trackedAltScreenCapture (alt_screen_capture.go:310), reached from the
// transition path.
func TestTrackedAltScreenCaptureResetInvalidOnDropLenOutOfRange(t *testing.T) {
	t.Parallel()
	vt := newTrackedAltCaptureVTerm(t)

	vt.altCapture.dropLen = vt.altCapture.frameLen + 1
	snap := snapshotScrollback(vt)

	_, _, _, oldLines, ok := vt.trackedAltScreenCapture()

	if ok {
		t.Fatalf("expected tracked capture lookup to fail on invalid dropLen")
	}
	if oldLines != nil {
		t.Fatalf("expected no capture lines returned, got %d", len(oldLines))
	}
	assertAltCaptureCleared(t, vt)
	assertScrollbackUnchanged(t, vt, snap)
}

// Drives the "scrollback shrank below tracked frame" invariant violation in
// trackedAltScreenCapture (alt_screen_capture.go:315).
func TestTrackedAltScreenCaptureResetInvalidOnShrunkScrollback(t *testing.T) {
	t.Parallel()
	vt := newTrackedAltCaptureVTerm(t)

	total := vt.altCapture.frameLen + vt.altCapture.endOffset
	vt.Scrollback = vt.Scrollback[:total-1]
	snap := snapshotScrollback(vt)

	_, _, _, oldLines, ok := vt.trackedAltScreenCapture()

	if ok {
		t.Fatalf("expected tracked capture lookup to fail on shrunk scrollback")
	}
	if oldLines != nil {
		t.Fatalf("expected no capture lines returned, got %d", len(oldLines))
	}
	assertAltCaptureCleared(t, vt)
	assertScrollbackUnchanged(t, vt, snap)
}
