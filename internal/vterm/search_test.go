package vterm

import "testing"

// blankLines returns height blank lines used to seed deterministic scrollback.
func blankLines(width, height int) [][]Cell {
	rows := make([][]Cell, height)
	for i := range rows {
		rows[i] = MakeBlankLine(width)
	}
	return rows
}

func TestTotalLines(t *testing.T) {
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
			name: "screen only, no scrollback",
			vt:   New(5, 3),
			want: 3,
		},
		{
			name:       "screen plus scrollback",
			vt:         New(5, 3),
			scrollback: 4,
			want:       7,
		},
		{
			name:       "single scrollback line",
			vt:         New(5, 1),
			scrollback: 1,
			want:       2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.vt != nil && tt.scrollback > 0 {
				tt.vt.Scrollback = make([][]Cell, tt.scrollback)
				for i := range tt.vt.Scrollback {
					tt.vt.Scrollback[i] = MakeBlankLine(tt.vt.Width)
				}
			}
			if got := tt.vt.TotalLines(); got != tt.want {
				t.Fatalf("TotalLines() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTotalLinesUsesFrozenSyncSnapshot(t *testing.T) {
	t.Parallel()
	vt := New(5, 2)
	vt.Scrollback = blankLines(5, 2)

	vt.setSynchronizedOutput(true)
	// Grow live scrollback after sync begins; TotalLines must reflect the
	// frozen snapshot length (2), not the live length (4).
	vt.Scrollback = append(vt.Scrollback, MakeBlankLine(5), MakeBlankLine(5))

	if got := vt.TotalLines(); got != 4 {
		t.Fatalf("TotalLines() during sync = %d, want 4 (2 frozen scrollback + 2 screen)", got)
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
				tt.vt.Scrollback = make([][]Cell, tt.scrollback)
				for i := range tt.vt.Scrollback {
					tt.vt.Scrollback[i] = MakeBlankLine(tt.vt.Width)
				}
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
	vt.Scrollback = blankLines(5, 3)

	vt.setSynchronizedOutput(true)
	// Live scrollback grows during sync; MaxViewOffset reports the frozen length.
	vt.Scrollback = append(vt.Scrollback, MakeBlankLine(5), MakeBlankLine(5))

	if got := vt.MaxViewOffset(); got != 3 {
		t.Fatalf("MaxViewOffset() during sync = %d, want 3 (frozen scrollback)", got)
	}
}

func TestVisibleLineRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		vt         *VTerm
		scrollback int
		viewOffset int
		wantStart  int
		wantEnd    int
		wantTotal  int
	}{
		{
			name:      "nil receiver returns zeros",
			vt:        nil,
			wantStart: 0,
			wantEnd:   0,
			wantTotal: 0,
		},
		{
			name:       "screen exactly fills viewport, no scrollback, live view",
			vt:         New(5, 3),
			viewOffset: 0,
			wantStart:  0,
			wantEnd:    3,
			wantTotal:  3,
		},
		{
			name:       "scrollback present, live view shows newest height lines",
			vt:         New(5, 3),
			scrollback: 4,
			viewOffset: 0,
			// total = 7, start = 7 - 3 - 0 = 4, end = 7
			wantStart: 4,
			wantEnd:   7,
			wantTotal: 7,
		},
		{
			name:       "scrolled up by 2 reveals earlier lines",
			vt:         New(5, 3),
			scrollback: 4,
			viewOffset: 2,
			// total = 7, start = 7 - 3 - 2 = 2, end = 5
			wantStart: 2,
			wantEnd:   5,
			wantTotal: 7,
		},
		{
			name:       "scrolled to very top clamps start at 0",
			vt:         New(5, 3),
			scrollback: 4,
			viewOffset: 4,
			// total = 7, start = 7 - 3 - 4 = 0, end = 3
			wantStart: 0,
			wantEnd:   3,
			wantTotal: 7,
		},
		{
			name:       "over-large view offset clamps start at 0",
			vt:         New(5, 3),
			scrollback: 4,
			viewOffset: 100,
			// start would be negative, clamps to 0; end = 0 + 3 = 3
			wantStart: 0,
			wantEnd:   3,
			wantTotal: 7,
		},
		{
			name:       "height larger than total clamps end at total",
			vt:         New(5, 10),
			scrollback: 0,
			viewOffset: 0,
			// total = 10 (screen height), start = 10 - 10 - 0 = 0, end = 10
			wantStart: 0,
			wantEnd:   10,
			wantTotal: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.vt != nil {
				if tt.scrollback > 0 {
					tt.vt.Scrollback = make([][]Cell, tt.scrollback)
					for i := range tt.vt.Scrollback {
						tt.vt.Scrollback[i] = MakeBlankLine(tt.vt.Width)
					}
				}
				tt.vt.ViewOffset = tt.viewOffset
			}

			start, end, total := tt.vt.VisibleLineRange()
			if start != tt.wantStart || end != tt.wantEnd || total != tt.wantTotal {
				t.Fatalf("VisibleLineRange() = (%d, %d, %d), want (%d, %d, %d)",
					start, end, total, tt.wantStart, tt.wantEnd, tt.wantTotal)
			}
		})
	}
}

func TestVisibleLineRangeZeroHeight(t *testing.T) {
	t.Parallel()
	// Construct a normal terminal then force Height=0 directly to exercise the
	// v.Height <= 0 guard in VisibleLineRange (passing 0 to New would instead
	// trip the total <= 0 guard first).
	vt := New(5, 1)
	vt.Height = 0

	start, end, total := vt.VisibleLineRange()
	if start != 0 || end != 0 {
		t.Fatalf("VisibleLineRange() start/end = (%d, %d), want (0, 0) for zero height", start, end)
	}
	// total still reflects the buffers even when the viewport has no rows.
	if total != 1 {
		t.Fatalf("VisibleLineRange() total = %d, want 1", total)
	}
}

func TestVisibleLineRangeEmptyBuffers(t *testing.T) {
	t.Parallel()
	// Force both screen and scrollback empty to hit the total <= 0 guard.
	vt := New(5, 1)
	vt.Screen = nil
	vt.Scrollback = nil

	start, end, total := vt.VisibleLineRange()
	if start != 0 || end != 0 || total != 0 {
		t.Fatalf("VisibleLineRange() = (%d, %d, %d), want (0, 0, 0) for empty buffers", start, end, total)
	}
}

func TestVisibleLineRangeUsesFrozenSyncSnapshot(t *testing.T) {
	t.Parallel()
	vt := New(5, 2)
	vt.Scrollback = blankLines(5, 3)

	vt.setSynchronizedOutput(true)
	// Live scrollback grows during sync; the visible range stays anchored to the
	// frozen snapshot (total = 3 scrollback + 2 screen = 5).
	vt.Scrollback = append(vt.Scrollback, MakeBlankLine(5), MakeBlankLine(5))

	start, end, total := vt.VisibleLineRange()
	if total != 5 {
		t.Fatalf("VisibleLineRange() total during sync = %d, want 5", total)
	}
	// Live view: start = 5 - 2 - 0 = 3, end = 5.
	if start != 3 || end != 5 {
		t.Fatalf("VisibleLineRange() = (%d, %d, _) during sync, want (3, 5, _)", start, end)
	}
}
