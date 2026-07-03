package vterm

import (
	"testing"
	"time"
)

// addScrollbackLines appends n blank scrollback rows so that
// currentMaxViewOffset (== scrollback length) becomes n.
func addScrollbackLines(v *VTerm, n int) {
	for i := 0; i < n; i++ {
		v.Scrollback = append(v.Scrollback, MakeBlankLine(v.Width))
	}
}

func TestScrollViewTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		scrollbackLen  int
		startOffset    int
		requestOffset  int
		wantOffset     int
		wantVersionBmp bool
	}{
		{
			name:           "into history within bounds",
			scrollbackLen:  100,
			startOffset:    0,
			requestOffset:  40,
			wantOffset:     40,
			wantVersionBmp: true,
		},
		{
			name:           "clamps above max to scrollback length",
			scrollbackLen:  50,
			startOffset:    0,
			requestOffset:  9999,
			wantOffset:     50,
			wantVersionBmp: true,
		},
		{
			name:           "clamps negative to zero",
			scrollbackLen:  50,
			startOffset:    20,
			requestOffset:  -10,
			wantOffset:     0,
			wantVersionBmp: true,
		},
		{
			name:           "exact max boundary",
			scrollbackLen:  30,
			startOffset:    0,
			requestOffset:  30,
			wantOffset:     30,
			wantVersionBmp: true,
		},
		{
			name:           "no scrollback clamps any positive request to zero",
			scrollbackLen:  0,
			startOffset:    0,
			requestOffset:  25,
			wantOffset:     0,
			wantVersionBmp: false,
		},
		{
			name:           "same offset does not bump version",
			scrollbackLen:  100,
			startOffset:    33,
			requestOffset:  33,
			wantOffset:     33,
			wantVersionBmp: false,
		},
		{
			name:           "zero request from live stays live",
			scrollbackLen:  100,
			startOffset:    0,
			requestOffset:  0,
			wantOffset:     0,
			wantVersionBmp: false,
		},
		{
			name:           "return to live from history bumps version",
			scrollbackLen:  100,
			startOffset:    60,
			requestOffset:  0,
			wantOffset:     0,
			wantVersionBmp: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vt := New(80, 24)
			addScrollbackLines(vt, tt.scrollbackLen)
			vt.ViewOffset = tt.startOffset

			before := vt.Version()
			vt.ScrollViewTo(tt.requestOffset)

			if vt.ViewOffset != tt.wantOffset {
				t.Errorf("ViewOffset = %d, want %d", vt.ViewOffset, tt.wantOffset)
			}

			bumped := vt.Version() != before
			if bumped != tt.wantVersionBmp {
				t.Errorf("version bumped = %v (%d -> %d), want %v",
					bumped, before, vt.Version(), tt.wantVersionBmp)
			}
		})
	}
}

// TestScrollViewToIdempotent verifies that repeatedly setting the same final
// (post-clamp) offset only bumps the version on the first effective change.
func TestScrollViewToIdempotent(t *testing.T) {
	t.Parallel()
	vt := New(80, 24)
	addScrollbackLines(vt, 40)

	vt.ScrollViewTo(40)
	afterFirst := vt.Version()
	if vt.ViewOffset != 40 {
		t.Fatalf("ViewOffset = %d after first scroll, want 40", vt.ViewOffset)
	}

	// Requesting a beyond-max offset clamps back to the same 40 — no change,
	// so the version must not bump again.
	vt.ScrollViewTo(1000)
	if vt.ViewOffset != 40 {
		t.Fatalf("ViewOffset = %d after clamped re-scroll, want 40", vt.ViewOffset)
	}
	if vt.Version() != afterFirst {
		t.Errorf("version bumped on no-op clamped scroll: %d -> %d", afterFirst, vt.Version())
	}
}

// TestScrollViewToDuringSync verifies clamping uses the frozen scrollback
// length while synchronized output is active, not the underlying buffer.
func TestScrollViewToDuringSync(t *testing.T) {
	t.Parallel()
	vt := New(80, 24)
	addScrollbackLines(vt, 100)

	// Freeze the viewport at a smaller scrollback length than the live buffer.
	// syncStartedAt must be fresh or the stall failsafe releases the sync.
	vt.syncActive = true
	vt.syncStartedAt = time.Now()
	vt.syncScreen = vt.Screen
	vt.syncScrollbackLen = 20

	vt.ScrollViewTo(500)
	if vt.ViewOffset != 20 {
		t.Errorf("ViewOffset = %d during sync, want clamp to frozen max 20", vt.ViewOffset)
	}
}

func TestIsScrolled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		viewOffset int
		want       bool
	}{
		{name: "live view is not scrolled", viewOffset: 0, want: false},
		{name: "positive offset is scrolled", viewOffset: 1, want: true},
		{name: "large offset is scrolled", viewOffset: 9999, want: true},
		{name: "negative offset is not scrolled", viewOffset: -5, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vt := New(80, 24)
			vt.ViewOffset = tt.viewOffset
			if got := vt.IsScrolled(); got != tt.want {
				t.Errorf("IsScrolled() = %v, want %v (ViewOffset=%d)", got, tt.want, tt.viewOffset)
			}
		})
	}
}

// TestIsScrolledTracksScrollViewTo verifies IsScrolled reflects the effective
// (clamped) offset set by ScrollViewTo, including the round trip back to live.
func TestIsScrolledTracksScrollViewTo(t *testing.T) {
	t.Parallel()
	vt := New(80, 24)
	addScrollbackLines(vt, 50)

	if vt.IsScrolled() {
		t.Fatal("fresh terminal should not report scrolled")
	}

	vt.ScrollViewTo(10)
	if !vt.IsScrolled() {
		t.Error("expected IsScrolled true after scrolling into history")
	}

	// A request that clamps to 0 (no scrollback honored) leaves us live.
	vt.ScrollViewTo(0)
	if vt.IsScrolled() {
		t.Error("expected IsScrolled false after returning to live view")
	}

	// With no scrollback, any positive request clamps to 0 and stays live.
	empty := New(80, 24)
	empty.ScrollViewTo(5)
	if empty.IsScrolled() {
		t.Error("expected IsScrolled false when scrollback is empty")
	}
}

func TestMaxViewOffset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		vt         *VTerm
		scrollback int
		want       int
	}{
		{
			name: "nil receiver",
			vt:   nil,
			want: 0,
		},
		{
			name: "no scrollback",
			vt:   New(5, 3),
			want: 0,
		},
		{
			name:       "with scrollback",
			vt:         New(5, 3),
			scrollback: 6,
			want:       6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.vt != nil && tt.scrollback > 0 {
				addScrollbackLines(tt.vt, tt.scrollback)
			}
			if got := tt.vt.MaxViewOffset(); got != tt.want {
				t.Fatalf("MaxViewOffset() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMaxViewOffsetUsesFrozenSyncSnapshot(t *testing.T) {
	t.Parallel()
	vt := New(5, 2)
	addScrollbackLines(vt, 3)

	vt.setSynchronizedOutput(true)
	// Live scrollback grows during sync; MaxViewOffset reports the frozen length.
	vt.Scrollback = append(vt.Scrollback, MakeBlankLine(5), MakeBlankLine(5))

	if got := vt.MaxViewOffset(); got != 3 {
		t.Fatalf("MaxViewOffset() during sync = %d, want 3 (frozen scrollback)", got)
	}
}

func TestVTermHasScrollback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		vt         *VTerm
		scrollback int
		want       bool
	}{
		{name: "nil receiver", vt: nil, want: false},
		{name: "no scrollback", vt: New(5, 3), want: false},
		{name: "one scrollback row", vt: New(5, 3), scrollback: 1, want: true},
		{name: "many scrollback rows", vt: New(5, 3), scrollback: 42, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.vt != nil && tt.scrollback > 0 {
				addScrollbackLines(tt.vt, tt.scrollback)
			}
			if got := VTermHasScrollback(tt.vt); got != tt.want {
				t.Fatalf("VTermHasScrollback() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestScrollViewAndNote verifies the helper moves the viewport exactly like
// ScrollView and records the interaction like NoteSyncViewportInteraction, so
// the two paired calls stay equivalent to the inlined pair.
func TestScrollViewAndNote(t *testing.T) {
	t.Parallel()

	t.Run("moves viewport like ScrollView", func(t *testing.T) {
		t.Parallel()
		vt := New(80, 24)
		addScrollbackLines(vt, 100)

		vt.ScrollViewAndNote(30)
		if vt.ViewOffset != 30 {
			t.Fatalf("ViewOffset = %d, want 30", vt.ViewOffset)
		}
		vt.ScrollViewAndNote(-10)
		if vt.ViewOffset != 20 {
			t.Fatalf("ViewOffset = %d after scroll back, want 20", vt.ViewOffset)
		}
	})

	t.Run("clamps like ScrollView", func(t *testing.T) {
		t.Parallel()
		vt := New(80, 24)
		addScrollbackLines(vt, 5)

		vt.ScrollViewAndNote(9999)
		if vt.ViewOffset != 5 {
			t.Fatalf("ViewOffset = %d, want clamp to max 5", vt.ViewOffset)
		}
	})

	t.Run("notes interaction during sync while scrolled", func(t *testing.T) {
		t.Parallel()
		vt := New(80, 24)
		addScrollbackLines(vt, 100)
		vt.setSynchronizedOutput(true)

		vt.ScrollViewAndNote(10)
		if !vt.syncPreserveViewport {
			t.Error("expected syncPreserveViewport set after scrolling into history during sync")
		}
	})
}
