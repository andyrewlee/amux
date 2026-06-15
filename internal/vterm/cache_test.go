package vterm

import "testing"

// allLinesDirty reports whether every visible line is currently dirty relative
// to the last clear. It is a small helper so the cache tests can assert the
// post-clear/post-mark dirty state through the same lineDirty predicate the
// renderer uses.
func allLinesDirty(vt *VTerm) bool {
	for y := 0; y < vt.Height; y++ {
		if !vt.lineDirty(y) {
			return false
		}
	}
	return true
}

// anyLineDirty reports whether at least one visible line is dirty.
func anyLineDirty(vt *VTerm) bool {
	for y := 0; y < vt.Height; y++ {
		if vt.lineDirty(y) {
			return true
		}
	}
	return false
}

// primeRenderCache renders once to populate the per-line renderCache strings
// and establish a known last-cursor baseline / clean epoch through a full
// Render pass. The per-line renderLineEpoch slice itself is already sized to
// Height by New() (vterm.go:143 via ensureRenderCache), so it is available
// before any render; this prime step is about seeding the cached frame and
// clean-epoch baseline, not allocating the epoch slice.
func primeRenderCache(vt *VTerm) {
	vt.Render()
}

// TestRenderAndClearMatchesRender confirms RenderAndClear returns byte-for-byte
// the same string Render produces for the same screen state. The clear side
// effect must not alter the emitted frame.
func TestRenderAndClearMatchesRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		width  int
		height int
		rows   []string
	}{
		{name: "empty screen", width: 4, height: 2, rows: nil},
		{name: "single row", width: 6, height: 1, rows: []string{"hello"}},
		{name: "multi row", width: 6, height: 3, rows: []string{"aa", "bb", "cc"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Render() and RenderAndClear() must agree, so build two identical
			// terminals: one to read the reference frame, one to clear.
			ref := writeRows(tc.width, tc.height, tc.rows...)
			got := writeRows(tc.width, tc.height, tc.rows...)

			want := ref.Render()
			if out := got.RenderAndClear(); out != want {
				t.Fatalf("RenderAndClear() = %q, want %q", out, want)
			}
		})
	}
}

// TestRenderAndClearClearsDirtyState verifies the combined render+clear leaves
// the live cache clean: after writing content (which marks lines dirty) a
// RenderAndClear must drop every line back to not-dirty, exactly as a Render
// followed by ClearDirty would.
func TestRenderAndClearClearsDirtyState(t *testing.T) {
	t.Parallel()
	vt := writeRows(6, 3, "aa", "bb", "cc")
	// Writing content marked lines dirty; confirm the precondition.
	primeRenderCache(vt)
	vt.markDirtyRange(0, vt.Height-1)
	if !anyLineDirty(vt) {
		t.Fatalf("precondition: expected dirty lines after marking, got none")
	}

	vt.RenderAndClear()

	if anyLineDirty(vt) {
		t.Fatalf("RenderAndClear() left lines dirty: lineDirty still reports a dirty row")
	}
	if vt.allDirty() {
		t.Fatalf("RenderAndClear() left global-dirty flag set")
	}
}

// TestRenderAndClearDoesNotClearWhenScrolled checks that the clear half of
// RenderAndClear honors the same live-cache guard as ClearDirty: when scrolled
// (ViewOffset > 0) the dirty state must be preserved, because the live cache is
// not the buffer being shown.
func TestRenderAndClearDoesNotClearWhenScrolled(t *testing.T) {
	t.Parallel()
	vt := New(6, 3)
	vt.Scrollback = [][]Cell{
		lineFromString(6, "h0"),
		lineFromString(6, "h1"),
		lineFromString(6, "h2"),
	}
	vt.Screen = [][]Cell{
		lineFromString(6, "s0"),
		lineFromString(6, "s1"),
		lineFromString(6, "s2"),
	}
	primeRenderCache(vt)
	vt.markDirtyRange(0, vt.Height-1)
	if !anyLineDirty(vt) {
		t.Fatalf("precondition: expected dirty lines, got none")
	}

	// Scroll up so liveRenderCacheActive() is false.
	vt.ViewOffset = 1
	vt.RenderAndClear()

	if !anyLineDirty(vt) {
		t.Fatalf("RenderAndClear() while scrolled wrongly cleared dirty state")
	}
}

// TestClearDirtyWithCursorClearsAndRecordsCursor verifies the live path: it both
// clears the dirty epoch AND copies the cursor fields into the last-frame cache
// that LastCursorState reads back.
func TestClearDirtyWithCursorClearsAndRecordsCursor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		cursorX      int
		cursorY      int
		cursorHidden bool
		showCursor   bool
		want         CursorRenderState
	}{
		{
			name:       "visible cursor recorded",
			cursorX:    2,
			cursorY:    1,
			showCursor: true,
			want:       CursorRenderState{X: 2, Y: 1, ShowCursor: true, Hidden: false},
		},
		{
			name:         "hidden cursor recorded",
			cursorX:      0,
			cursorY:      0,
			cursorHidden: true,
			showCursor:   false,
			want:         CursorRenderState{X: 0, Y: 0, ShowCursor: false, Hidden: true},
		},
		{
			name:       "boundary corner recorded",
			cursorX:    5,
			cursorY:    2,
			showCursor: true,
			want:       CursorRenderState{X: 5, Y: 2, ShowCursor: true, Hidden: false},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(6, 3)
			primeRenderCache(vt)
			vt.markDirtyRange(0, vt.Height-1)

			vt.CursorX = tc.cursorX
			vt.CursorY = tc.cursorY
			vt.CursorHidden = tc.cursorHidden
			vt.ClearDirtyWithCursor(tc.showCursor)

			if got := vt.LastCursorState(); got != tc.want {
				t.Fatalf("LastCursorState() = %+v, want %+v", got, tc.want)
			}
			if anyLineDirty(vt) {
				t.Fatalf("ClearDirtyWithCursor() did not clear dirty lines")
			}
		})
	}
}

// TestClearDirtyWithCursorSkippedWhenSynced confirms the live-cache guard: while
// synchronized output is active liveRenderCacheActive() is false, so the call
// must be a no-op for BOTH the dirty clear and the cursor cache. A stale cursor
// snapshot from an earlier frame must survive untouched.
func TestClearDirtyWithCursorSkippedWhenSynced(t *testing.T) {
	t.Parallel()
	vt := New(6, 3)
	primeRenderCache(vt)

	// Record a known cursor snapshot through the live path first.
	vt.CursorX, vt.CursorY = 1, 1
	vt.ClearDirtyWithCursor(true)
	baseline := vt.LastCursorState()

	// Now mark dirty, enter sync, and try to clear with a different cursor.
	vt.markDirtyRange(0, vt.Height-1)
	vt.setSynchronizedOutput(true)
	vt.CursorX, vt.CursorY = 4, 2
	vt.CursorHidden = true

	// Snapshot renderCleanEpoch before the in-sync clear. The guard must make
	// ClearDirtyWithCursor a no-op, so renderCleanEpoch must not advance. We
	// assert this while still in sync, before exiting sync runs
	// invalidateRenderCache() (which would mark everything dirty unconditionally
	// and mask whether the in-sync clear was actually suppressed).
	before := vt.renderCleanEpoch
	vt.ClearDirtyWithCursor(false)
	if vt.renderCleanEpoch != before {
		t.Fatalf("ClearDirtyWithCursor() during sync advanced renderCleanEpoch: got %d, want %d", vt.renderCleanEpoch, before)
	}

	if got := vt.LastCursorState(); got != baseline {
		t.Fatalf("ClearDirtyWithCursor() during sync mutated cursor cache: got %+v, want %+v", got, baseline)
	}
}

// TestClearDirtyWithCursorSkippedWhenScrolled mirrors the sync case for the
// ViewOffset > 0 branch of the live-cache guard: a scrolled terminal must not
// have its cursor cache or dirty epoch updated.
func TestClearDirtyWithCursorSkippedWhenScrolled(t *testing.T) {
	t.Parallel()
	vt := New(6, 3)
	vt.Scrollback = [][]Cell{lineFromString(6, "h0")}
	primeRenderCache(vt)

	vt.CursorX, vt.CursorY = 0, 0
	vt.ClearDirtyWithCursor(true)
	baseline := vt.LastCursorState()

	vt.markDirtyRange(0, vt.Height-1)
	vt.ViewOffset = 1
	vt.CursorX, vt.CursorY = 3, 2
	vt.ClearDirtyWithCursor(false)

	if got := vt.LastCursorState(); got != baseline {
		t.Fatalf("ClearDirtyWithCursor() while scrolled mutated cursor cache: got %+v, want %+v", got, baseline)
	}
	if !anyLineDirty(vt) {
		t.Fatalf("ClearDirtyWithCursor() while scrolled wrongly cleared dirty state")
	}
}

// TestClearDirtyWithCursorOverwritesPreviousFrame ensures consecutive live
// calls overwrite the cached cursor state rather than accumulating, so
// LastCursorState always reflects the most recent cleared frame.
func TestClearDirtyWithCursorOverwritesPreviousFrame(t *testing.T) {
	t.Parallel()
	vt := New(8, 4)
	primeRenderCache(vt)

	vt.CursorX, vt.CursorY = 1, 0
	vt.CursorHidden = false
	vt.ClearDirtyWithCursor(true)
	if got := vt.LastCursorState(); got != (CursorRenderState{X: 1, Y: 0, ShowCursor: true}) {
		t.Fatalf("first frame LastCursorState() = %+v", got)
	}

	vt.CursorX, vt.CursorY = 7, 3
	vt.CursorHidden = true
	vt.ClearDirtyWithCursor(false)
	want := CursorRenderState{X: 7, Y: 3, ShowCursor: false, Hidden: true}
	if got := vt.LastCursorState(); got != want {
		t.Fatalf("second frame LastCursorState() = %+v, want %+v", got, want)
	}
}

// TestRenderAndClearThenMarkDirtyAgain checks the full cache loop: after a
// RenderAndClear leaves the cache clean, a fresh write must mark lines dirty
// again, proving the clear advanced the epoch rather than freezing it.
func TestRenderAndClearThenMarkDirtyAgain(t *testing.T) {
	t.Parallel()
	vt := writeRows(6, 2, "ab", "cd")
	primeRenderCache(vt)
	vt.RenderAndClear()
	if anyLineDirty(vt) {
		t.Fatalf("precondition: expected clean cache after RenderAndClear")
	}

	// A new write must dirty its line again.
	vt.markDirtyLine(0)
	if !vt.lineDirty(0) {
		t.Fatalf("markDirtyLine after RenderAndClear failed to re-dirty line 0")
	}
	if vt.lineDirty(1) {
		t.Fatalf("marking line 0 wrongly dirtied line 1")
	}
	if !allLinesDirty(writeRows(6, 2, "ab", "cd")) {
		// Sanity on the helper: a freshly written, un-cleared terminal reports
		// dirty lines (renderCleanEpoch starts at 0).
		t.Fatalf("allLinesDirty helper: fresh terminal should report dirty")
	}
}
