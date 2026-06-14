package vterm

import "testing"

// sampleStyle returns a non-default Style so tests can prove save/restore copy
// the whole struct rather than leaving it zero-valued.
func sampleStyle() Style {
	return Style{
		Fg:        Color{Type: ColorIndexed, Value: 3},
		Bg:        Color{Type: ColorRGB, Value: 0x112233},
		Bold:      true,
		Underline: true,
		Reverse:   true,
	}
}

// TestSetScrollRegion exercises the 1-indexed scroll-region setter across the
// normal accept path, the clamping of out-of-range top/bottom, the inverted
// region guard, and origin-mode cursor homing.
func TestSetScrollRegion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		height     int
		originMode bool
		top        int
		bottom     int
		// expected region; if changed is false the region must stay at its
		// initial New() value (top=0, bottom=height).
		changed    bool
		wantTop    int
		wantBottom int
		wantCurX   int
		wantCurY   int
	}{
		{
			name:       "full region 1..height",
			height:     5,
			top:        1,
			bottom:     5,
			changed:    true,
			wantTop:    0,
			wantBottom: 5,
			wantCurX:   0,
			wantCurY:   0,
		},
		{
			name:       "interior region",
			height:     6,
			top:        2,
			bottom:     4,
			changed:    true,
			wantTop:    1,
			wantBottom: 4,
			wantCurX:   0,
			wantCurY:   0,
		},
		{
			name:       "top below 1 clamps to 0",
			height:     5,
			top:        0,
			bottom:     3,
			changed:    true,
			wantTop:    0,
			wantBottom: 3,
			wantCurX:   0,
			wantCurY:   0,
		},
		{
			name:       "negative top clamps to 0",
			height:     5,
			top:        -4,
			bottom:     2,
			changed:    true,
			wantTop:    0,
			wantBottom: 2,
			wantCurX:   0,
			wantCurY:   0,
		},
		{
			name:       "bottom past height clamps to height",
			height:     4,
			top:        2,
			bottom:     99,
			changed:    true,
			wantTop:    1,
			wantBottom: 4,
			wantCurX:   0,
			wantCurY:   0,
		},
		{
			name:    "inverted region top==bottom is rejected",
			height:  5,
			top:     3,
			bottom:  2, // b becomes 2, t becomes 2 -> t >= b
			changed: false,
		},
		{
			name:    "inverted region top>bottom is rejected",
			height:  5,
			top:     4,
			bottom:  1, // b=1, t=3 -> t >= b
			changed: false,
		},
		{
			name:    "zero bottom collapses and is rejected",
			height:  5,
			top:     1,
			bottom:  0, // t=0, b=0 -> t >= b
			changed: false,
		},
		{
			name:       "origin mode homes cursor to scroll top",
			height:     6,
			originMode: true,
			top:        3,
			bottom:     5,
			changed:    true,
			wantTop:    2,
			wantBottom: 5,
			wantCurX:   0,
			wantCurY:   2, // ScrollTop
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(8, tc.height)
			vt.OriginMode = tc.originMode
			// Park the cursor away from home so a successful call is observable.
			vt.CursorX = 4
			vt.CursorY = tc.height - 1
			startVersion := vt.Version()
			startTop, startBottom := vt.ScrollTop, vt.ScrollBottom

			vt.setScrollRegion(tc.top, tc.bottom)

			if !tc.changed {
				if vt.ScrollTop != startTop || vt.ScrollBottom != startBottom {
					t.Fatalf("rejected region mutated bounds: got top=%d bottom=%d, want top=%d bottom=%d",
						vt.ScrollTop, vt.ScrollBottom, startTop, startBottom)
				}
				if vt.CursorX != 4 || vt.CursorY != tc.height-1 {
					t.Fatalf("rejected region moved cursor to (%d,%d), want (4,%d)",
						vt.CursorX, vt.CursorY, tc.height-1)
				}
				if vt.Version() != startVersion {
					t.Fatalf("rejected region bumped version from %d to %d", startVersion, vt.Version())
				}
				return
			}

			if vt.ScrollTop != tc.wantTop || vt.ScrollBottom != tc.wantBottom {
				t.Fatalf("scroll region = top %d bottom %d, want top %d bottom %d",
					vt.ScrollTop, vt.ScrollBottom, tc.wantTop, tc.wantBottom)
			}
			if vt.CursorX != tc.wantCurX || vt.CursorY != tc.wantCurY {
				t.Fatalf("cursor = (%d,%d), want (%d,%d)",
					vt.CursorX, vt.CursorY, tc.wantCurX, tc.wantCurY)
			}
			// The cursor started at (4, height-1) and homed, so it moved and the
			// version must have advanced.
			if vt.Version() == startVersion {
				t.Fatalf("successful region change did not bump version (still %d)", startVersion)
			}
		})
	}
}

// TestSetScrollRegionNoVersionBumpWhenCursorStationary checks the
// bumpVersionIfCursorMoved contract: if the homed cursor lands exactly where it
// already was, no version bump occurs even though the region changed.
func TestSetScrollRegionNoVersionBumpWhenCursorStationary(t *testing.T) {
	t.Parallel()
	vt := New(8, 5)
	// Cursor already at home (0,0); a non-origin region homes to (0,0) again.
	vt.CursorX, vt.CursorY = 0, 0
	startVersion := vt.Version()

	vt.setScrollRegion(2, 4)

	if vt.ScrollTop != 1 || vt.ScrollBottom != 4 {
		t.Fatalf("scroll region = top %d bottom %d, want top 1 bottom 4", vt.ScrollTop, vt.ScrollBottom)
	}
	if vt.Version() != startVersion {
		t.Fatalf("version bumped from %d to %d despite stationary cursor", startVersion, vt.Version())
	}
}

// TestSaveCursor verifies saveCursor snapshots position and style without
// touching the live cursor, and that a later restore reads the saved snapshot
// rather than whatever the cursor moved to afterward.
func TestSaveCursor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		curX  int
		curY  int
		style Style
	}{
		{name: "home with default style", curX: 0, curY: 0, style: Style{}},
		{name: "interior with styled cell", curX: 3, curY: 2, style: sampleStyle()},
		{name: "far corner", curX: 7, curY: 4, style: Style{Italic: true}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(8, 5)
			vt.CursorX, vt.CursorY = tc.curX, tc.curY
			vt.CurrentStyle = tc.style
			startVersion := vt.Version()

			vt.saveCursor()

			if vt.SavedCursorX != tc.curX || vt.SavedCursorY != tc.curY {
				t.Fatalf("saved cursor = (%d,%d), want (%d,%d)",
					vt.SavedCursorX, vt.SavedCursorY, tc.curX, tc.curY)
			}
			if vt.SavedStyle != tc.style {
				t.Fatalf("saved style = %+v, want %+v", vt.SavedStyle, tc.style)
			}
			// saveCursor must not move the live cursor or alter visible content,
			// so the version counter stays put.
			if vt.CursorX != tc.curX || vt.CursorY != tc.curY {
				t.Fatalf("saveCursor moved live cursor to (%d,%d)", vt.CursorX, vt.CursorY)
			}
			if vt.Version() != startVersion {
				t.Fatalf("saveCursor bumped version from %d to %d", startVersion, vt.Version())
			}
		})
	}
}

// TestRestoreCursor verifies restoreCursor copies the saved snapshot back onto
// the live cursor and style, and bumps the version exactly when the cursor
// actually moves.
func TestRestoreCursor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// live cursor before restore
		curX, curY int
		// saved snapshot to restore
		savedX, savedY int
		savedStyle     Style
		wantBump       bool
	}{
		{
			name: "restore moves cursor and style",
			curX: 5, curY: 3,
			savedX: 1, savedY: 0,
			savedStyle: sampleStyle(),
			wantBump:   true,
		},
		{
			name: "restore to same position bumps nothing",
			curX: 2, curY: 2,
			savedX: 2, savedY: 2,
			savedStyle: Style{Bold: true},
			wantBump:   false,
		},
		{
			name: "restore changes only column",
			curX: 0, curY: 4,
			savedX: 6, savedY: 4,
			savedStyle: Style{},
			wantBump:   true,
		},
		{
			name: "restore changes only row",
			curX: 3, curY: 1,
			savedX: 3, savedY: 4,
			savedStyle: Style{Underline: true},
			wantBump:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(8, 5)
			vt.CursorX, vt.CursorY = tc.curX, tc.curY
			vt.CurrentStyle = Style{Strike: true} // distinct from savedStyle
			vt.SavedCursorX, vt.SavedCursorY = tc.savedX, tc.savedY
			vt.SavedStyle = tc.savedStyle
			startVersion := vt.Version()

			vt.restoreCursor()

			if vt.CursorX != tc.savedX || vt.CursorY != tc.savedY {
				t.Fatalf("restored cursor = (%d,%d), want (%d,%d)",
					vt.CursorX, vt.CursorY, tc.savedX, tc.savedY)
			}
			if vt.CurrentStyle != tc.savedStyle {
				t.Fatalf("restored style = %+v, want %+v", vt.CurrentStyle, tc.savedStyle)
			}
			if got := vt.Version() != startVersion; got != tc.wantBump {
				t.Fatalf("version bumped = %v (from %d to %d), want bump = %v",
					got, startVersion, vt.Version(), tc.wantBump)
			}
		})
	}
}

// TestSaveRestoreCursorRoundTrip exercises the DECSC/DECRC pair end to end: a
// save followed by cursor movement and a restore returns to the saved state,
// proving restore reads the snapshot and not the intervening cursor.
func TestSaveRestoreCursorRoundTrip(t *testing.T) {
	t.Parallel()
	vt := New(10, 6)
	vt.CursorX, vt.CursorY = 4, 2
	vt.CurrentStyle = sampleStyle()

	vt.saveCursor()

	// Move the cursor and change style after saving.
	vt.CursorX, vt.CursorY = 9, 5
	vt.CurrentStyle = Style{Blink: true}

	vt.restoreCursor()

	if vt.CursorX != 4 || vt.CursorY != 2 {
		t.Fatalf("round-trip cursor = (%d,%d), want (4,2)", vt.CursorX, vt.CursorY)
	}
	if vt.CurrentStyle != sampleStyle() {
		t.Fatalf("round-trip style = %+v, want %+v", vt.CurrentStyle, sampleStyle())
	}
}
