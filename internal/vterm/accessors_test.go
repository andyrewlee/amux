package vterm

import "testing"

// TestLastCursorState verifies that LastCursorState surfaces the cached cursor
// state recorded by the previous render frame. The cache is populated through
// the public ClearDirtyWithCursor seam so the test exercises the same path used
// by the renderer rather than poking unexported fields directly.
func TestLastCursorState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		cursorX      int
		cursorY      int
		showCursor   bool
		cursorHidden bool
		want         CursorRenderState
	}{
		{
			name: "zero value",
			want: CursorRenderState{X: 0, Y: 0, ShowCursor: false, Hidden: false},
		},
		{
			name:         "visible cursor mid-screen",
			cursorX:      3,
			cursorY:      2,
			showCursor:   true,
			cursorHidden: false,
			want:         CursorRenderState{X: 3, Y: 2, ShowCursor: true, Hidden: false},
		},
		{
			name:         "hidden cursor at origin",
			cursorX:      0,
			cursorY:      0,
			showCursor:   false,
			cursorHidden: true,
			want:         CursorRenderState{X: 0, Y: 0, ShowCursor: false, Hidden: true},
		},
		{
			name:         "boundary bottom-right corner",
			cursorX:      9,
			cursorY:      4,
			showCursor:   true,
			cursorHidden: false,
			want:         CursorRenderState{X: 9, Y: 4, ShowCursor: true, Hidden: false},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(10, 5)
			vt.CursorX = tc.cursorX
			vt.CursorY = tc.cursorY
			vt.CursorHidden = tc.cursorHidden
			// ClearDirtyWithCursor copies CursorX/Y, CursorHidden, and the
			// supplied showCursor into the last-frame cache that
			// LastCursorState reads back.
			vt.ClearDirtyWithCursor(tc.showCursor)

			got := vt.LastCursorState()
			if got != tc.want {
				t.Fatalf("LastCursorState() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestLastCursorStateReflectsLatestFrame ensures the cache is overwritten on
// each ClearDirtyWithCursor call so LastCursorState always reports the most
// recent frame rather than a stale snapshot.
func TestLastCursorStateReflectsLatestFrame(t *testing.T) {
	t.Parallel()
	vt := New(10, 5)

	vt.CursorX, vt.CursorY = 1, 1
	vt.CursorHidden = false
	vt.ClearDirtyWithCursor(true)
	if got := vt.LastCursorState(); got != (CursorRenderState{X: 1, Y: 1, ShowCursor: true}) {
		t.Fatalf("after first frame LastCursorState() = %+v", got)
	}

	vt.CursorX, vt.CursorY = 4, 3
	vt.CursorHidden = true
	vt.ClearDirtyWithCursor(false)
	want := CursorRenderState{X: 4, Y: 3, ShowCursor: false, Hidden: true}
	if got := vt.LastCursorState(); got != want {
		t.Fatalf("after second frame LastCursorState() = %+v, want %+v", got, want)
	}
}

// TestSelStartXEndX verifies the plain selection-column accessors return the
// exact X coordinates stored by SetSelection, including negative and
// out-of-range values which SetSelection stores verbatim (it does not clamp).
func TestSelStartXEndX(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		startX     int
		startLine  int
		endX       int
		endLine    int
		active     bool
		rect       bool
		wantStartX int
		wantEndX   int
	}{
		{
			name:       "basic forward selection",
			startX:     2,
			startLine:  0,
			endX:       7,
			endLine:    0,
			active:     true,
			wantStartX: 2,
			wantEndX:   7,
		},
		{
			name:       "zero coordinates",
			startX:     0,
			startLine:  0,
			endX:       0,
			endLine:    1,
			active:     true,
			wantStartX: 0,
			wantEndX:   0,
		},
		{
			name:       "negative start stored verbatim",
			startX:     -1,
			startLine:  0,
			endX:       3,
			endLine:    0,
			active:     true,
			wantStartX: -1,
			wantEndX:   3,
		},
		{
			name:       "rectangular selection columns",
			startX:     5,
			startLine:  1,
			endX:       2,
			endLine:    3,
			active:     true,
			rect:       true,
			wantStartX: 5,
			wantEndX:   2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(10, 5)
			vt.SetSelection(tc.startX, tc.startLine, tc.endX, tc.endLine, tc.active, tc.rect)

			if got := vt.SelStartX(); got != tc.wantStartX {
				t.Errorf("SelStartX() = %d, want %d", got, tc.wantStartX)
			}
			if got := vt.SelEndX(); got != tc.wantEndX {
				t.Errorf("SelEndX() = %d, want %d", got, tc.wantEndX)
			}
		})
	}
}

// TestSelStartXEndXDefault confirms that a freshly constructed VTerm with no
// selection reports zero-valued selection columns.
func TestSelStartXEndXDefault(t *testing.T) {
	t.Parallel()
	vt := New(10, 5)
	if got := vt.SelStartX(); got != 0 {
		t.Errorf("SelStartX() default = %d, want 0", got)
	}
	if got := vt.SelEndX(); got != 0 {
		t.Errorf("SelEndX() default = %d, want 0", got)
	}
}

// TestSelStartYEndY exercises the screen-Y projection of the selection lines.
// SelStartY/SelEndY delegate to AbsoluteLineToScreenY, which returns -1 when
// the absolute line is above or below the visible viewport. With no scrollback,
// absolute line L maps directly to screen row L for a viewport of Height rows.
func TestSelStartYEndY(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		height     int
		startLine  int
		endLine    int
		wantStartY int
		wantEndY   int
	}{
		{
			name:       "lines visible at top and bottom",
			height:     5,
			startLine:  0,
			endLine:    4,
			wantStartY: 0,
			wantEndY:   4,
		},
		{
			name:       "single visible line",
			height:     5,
			startLine:  2,
			endLine:    2,
			wantStartY: 2,
			wantEndY:   2,
		},
		{
			name:       "end line below viewport is not visible",
			height:     5,
			startLine:  3,
			endLine:    9,
			wantStartY: 3,
			wantEndY:   -1,
		},
		{
			name:       "start line below viewport is not visible",
			height:     5,
			startLine:  7,
			endLine:    4,
			wantStartY: -1,
			wantEndY:   4,
		},
		{
			name:       "negative start line above viewport",
			height:     5,
			startLine:  -1,
			endLine:    0,
			wantStartY: -1,
			wantEndY:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(10, tc.height)
			// No scrollback: totalLines == Height, ViewOffset == 0, so the
			// visible window starts at absolute line 0 and screenY == absLine.
			vt.SetSelection(0, tc.startLine, 0, tc.endLine, true, false)

			if got := vt.SelStartY(); got != tc.wantStartY {
				t.Errorf("SelStartY() = %d, want %d", got, tc.wantStartY)
			}
			if got := vt.SelEndY(); got != tc.wantEndY {
				t.Errorf("SelEndY() = %d, want %d", got, tc.wantEndY)
			}
		})
	}
}

// TestSelStartYEndYWithScrollback verifies the Y projection shifts correctly
// once scrollback exists. With scrollbackLen lines of history and a viewport
// of Height rows scrolled to the bottom (ViewOffset 0), the visible window
// starts at absolute line scrollbackLen, so only lines in
// [scrollbackLen, scrollbackLen+Height) are visible.
func TestSelStartYEndYWithScrollback(t *testing.T) {
	t.Parallel()
	const height = 4
	const scrollbackLen = 6

	vt := New(8, height)
	vt.Scrollback = make([][]Cell, scrollbackLen)
	for i := range vt.Scrollback {
		vt.Scrollback[i] = MakeBlankLine(8)
	}

	// Visible window starts at absolute line scrollbackLen (6) and spans rows
	// 0..height-1, i.e. absolute lines 6..9.
	vt.SetSelection(0, scrollbackLen, 0, scrollbackLen+height-1, true, false)
	if got := vt.SelStartY(); got != 0 {
		t.Errorf("SelStartY() at top of viewport = %d, want 0", got)
	}
	if got := vt.SelEndY(); got != height-1 {
		t.Errorf("SelEndY() at bottom of viewport = %d, want %d", got, height-1)
	}

	// A selection wholly inside scrollback (above the viewport) is not visible.
	vt.SetSelection(0, 0, 0, 1, true, false)
	if got := vt.SelStartY(); got != -1 {
		t.Errorf("SelStartY() for scrolled-off line = %d, want -1", got)
	}
	if got := vt.SelEndY(); got != -1 {
		t.Errorf("SelEndY() for scrolled-off line = %d, want -1", got)
	}

	// Scrolling the viewport up by 2 reveals the previously hidden lines: the
	// window now starts at absolute line scrollbackLen-ViewOffset (4).
	vt.ViewOffset = 2
	vt.SetSelection(0, scrollbackLen-2, 0, scrollbackLen-1, true, false)
	if got := vt.SelStartY(); got != 0 {
		t.Errorf("SelStartY() after scroll-up = %d, want 0", got)
	}
	if got := vt.SelEndY(); got != 1 {
		t.Errorf("SelEndY() after scroll-up = %d, want 1", got)
	}
}
