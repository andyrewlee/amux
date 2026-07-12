package compositor

import (
	"image/color"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestVTermLayerSelectionCursorOverlap(t *testing.T) {
	term := vterm.New(3, 1)
	term.CursorX = 0
	term.CursorY = 0
	term.SetSelection(0, 0, 0, 0, true, false)

	snap := NewVTermSnapshot(term, true)
	if snap == nil {
		t.Fatalf("expected snapshot, got nil")
	}

	cell := snap.Screen[0][0]
	var uvCell uv.Cell
	// Precompute the selection containment the way DrawAt does (bounds are
	// normalized once per frame and passed in).
	selStartX, selStartY, selEndX, selEndY := vterm.NormalizeSelectionRange(
		snap.SelStartX, snap.SelStartY, snap.SelEndX, snap.SelEndY)
	inSel := snap.SelActive && vterm.SelectionContains(
		selStartX, selStartY, selEndX, selEndY, 0, 0)
	cellToUVSnapshot(&uvCell, cell, snap, 0, 0, inSel)

	if uvCell.Style.Attrs&uv.AttrReverse == 0 {
		t.Fatalf("expected reverse attribute for selection+cursor overlap")
	}
}

// selectionSnapshot builds a blank w x h snapshot with an active selection.
func selectionSnapshot(w, h, startX, startY, endX, endY int) *VTermSnapshot {
	screen := make([][]vterm.Cell, h)
	for y := range screen {
		screen[y] = vterm.MakeBlankLine(w)
	}
	return &VTermSnapshot{
		Screen:    screen,
		Width:     w,
		Height:    h,
		SelActive: true,
		SelStartX: startX,
		SelStartY: startY,
		SelEndX:   endX,
		SelEndY:   endY,
	}
}

// reversedCells draws the snapshot and returns which cells have the reverse
// attribute (i.e. are rendered as selected).
func reversedCells(t *testing.T, snap *VTermSnapshot, w, h int) map[[2]int]bool {
	t.Helper()
	screen := &bufferScreen{Buffer: uv.NewBuffer(w, h)}
	NewVTermLayer(snap).Draw(screen, screen.Bounds())

	got := make(map[[2]int]bool)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			cell := screen.CellAt(x, y)
			if cell == nil {
				t.Fatalf("expected cell at (%d,%d)", x, y)
			}
			got[[2]int{x, y}] = cell.Style.Attrs&uv.AttrReverse != 0
		}
	}
	return got
}

// TestVTermLayerDrawMultiRowSelectionHighlight asserts a multi-row selection
// highlights exactly the selected range, and that a reversed selection (end
// before start) highlights the same cells as the forward one — normalization
// is the only thing making end<start highlight correctly.
func TestVTermLayerDrawMultiRowSelectionHighlight(t *testing.T) {
	const w, h = 5, 3

	// Forward selection: (1,0) through (3,1).
	forward := reversedCells(t, selectionSnapshot(w, h, 1, 0, 3, 1), w, h)

	// Exactly the selected range: row 0 from x=1, row 1 through x=3, row 2 none.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			want := (y == 0 && x >= 1) || (y == 1 && x <= 3)
			if forward[[2]int{x, y}] != want {
				t.Errorf("forward selection at (%d,%d): reverse=%v, want %v",
					x, y, forward[[2]int{x, y}], want)
			}
		}
	}

	// Reversed selection: same endpoints with end before start.
	reversed := reversedCells(t, selectionSnapshot(w, h, 3, 1, 1, 0), w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if reversed[[2]int{x, y}] != forward[[2]int{x, y}] {
				t.Errorf("reversed selection at (%d,%d): reverse=%v, want %v (same as forward)",
					x, y, reversed[[2]int{x, y}], forward[[2]int{x, y}])
			}
		}
	}
}

type bufferScreen struct {
	*uv.Buffer
}

type testWidth struct{}

func (testWidth) StringWidth(s string) int { return len(s) }

func (s *bufferScreen) WidthMethod() uv.WidthMethod {
	return testWidth{}
}

func TestVTermLayerClearsContinuationCells(t *testing.T) {
	term := vterm.New(2, 1)
	term.Screen[0][0] = vterm.Cell{Rune: '中', Width: 2}
	term.Screen[0][1] = vterm.Cell{Width: 0}

	snap := NewVTermSnapshot(term, true)
	if snap == nil {
		t.Fatalf("expected snapshot, got nil")
	}
	layer := NewVTermLayer(snap)

	screen := &bufferScreen{Buffer: uv.NewBuffer(2, 1)}
	// Seed stale content in the continuation cell.
	screen.SetCell(1, 0, &uv.Cell{Content: "X", Width: 1})
	layer.Draw(screen, screen.Bounds())

	cell := screen.CellAt(1, 0)
	if cell == nil {
		t.Fatalf("expected cell to be written at continuation position")
	}
	if cell.Width != 0 || cell.Content != "" {
		t.Fatalf("expected continuation cell to be cleared, got width=%d content=%q", cell.Width, cell.Content)
	}
}

func TestVTermSnapshotHonorsCursorHideOutsideAltScreen(t *testing.T) {
	term := vterm.New(10, 3)
	term.Write([]byte("\x1b[?25l")) // hide cursor outside alt screen

	snap := NewVTermSnapshot(term, true)
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if !snap.CursorHidden {
		t.Fatal("expected CursorHidden = true after \\x1b[?25l outside alt screen")
	}
}

func TestAnsiColorRGBA(t *testing.T) {
	tests := []struct {
		name                   string
		idx                    ansiColor
		wantR, wantG, wantB, a uint32
	}{
		{
			name:  "palette index 0 is black, opaque",
			idx:   0,
			wantR: 0, wantG: 0, wantB: 0, a: 65535,
		},
		{
			name:  "palette index 1 is red (205,49,49) scaled by 257",
			idx:   1,
			wantR: 205 * 257, wantG: 49 * 257, wantB: 49 * 257, a: 65535,
		},
		{
			name:  "palette index 7 is white (229,229,229)",
			idx:   7,
			wantR: 229 * 257, wantG: 229 * 257, wantB: 229 * 257, a: 65535,
		},
		{
			name:  "palette index 15 is bright white (255,255,255)",
			idx:   15,
			wantR: 65535, wantG: 65535, wantB: 65535, a: 65535,
		},
		{
			name:  "cube lower bound index 16 is black",
			idx:   16,
			wantR: 0, wantG: 0, wantB: 0, a: 65535,
		},
		{
			name:  "cube index 21 is pure blue (b level 255)",
			idx:   21,
			wantR: 0, wantG: 0, wantB: 255 * 257, a: 65535,
		},
		{
			name:  "cube index 196 is pure red (r level 255)",
			idx:   196,
			wantR: 255 * 257, wantG: 0, wantB: 0, a: 65535,
		},
		{
			name:  "cube index 46 is pure green (g level 255)",
			idx:   46,
			wantR: 0, wantG: 255 * 257, wantB: 0, a: 65535,
		},
		{
			name: "cube interior index 59 uses 55+level*40 ramp",
			idx:  59,
			// 59-16=43 -> r=(43/36)%6=1, g=(43/6)%6=1, b=43%6=1
			// each level = 55 + 1*40 = 95
			wantR: 95 * 257, wantG: 95 * 257, wantB: 95 * 257, a: 65535,
		},
		{
			name:  "cube upper bound index 231 is white",
			idx:   231,
			wantR: 255 * 257, wantG: 255 * 257, wantB: 255 * 257, a: 65535,
		},
		{
			name:  "grayscale lower bound index 232 is gray 8",
			idx:   232,
			wantR: 8 * 257, wantG: 8 * 257, wantB: 8 * 257, a: 65535,
		},
		{
			name: "grayscale interior index 243 is gray 118",
			idx:  243,
			// 8 + (243-232)*10 = 8 + 110 = 118
			wantR: 118 * 257, wantG: 118 * 257, wantB: 118 * 257, a: 65535,
		},
		{
			name: "grayscale upper bound index 255 is gray 238",
			idx:  255,
			// 8 + (255-232)*10 = 238
			wantR: 238 * 257, wantG: 238 * 257, wantB: 238 * 257, a: 65535,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, g, b, a := tt.idx.RGBA()
			if r != tt.wantR || g != tt.wantG || b != tt.wantB || a != tt.a {
				t.Errorf("ansiColor(%d).RGBA() = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
					uint32(tt.idx), r, g, b, a, tt.wantR, tt.wantG, tt.wantB, tt.a)
			}
			// Alpha is always fully opaque across the whole range.
			if a != 65535 {
				t.Errorf("ansiColor(%d) alpha = %d, want 65535", uint32(tt.idx), a)
			}
		})
	}
}

// TestAnsiColorRGBAGrayscaleIsAchromatic asserts every grayscale index produces
// equal R/G/B channels (the defining property of gray).
func TestAnsiColorRGBAGrayscaleIsAchromatic(t *testing.T) {
	for idx := ansiColor(232); idx <= 255; idx++ {
		r, g, b, _ := idx.RGBA()
		if r != g || g != b {
			t.Errorf("grayscale index %d not achromatic: (%d,%d,%d)", uint32(idx), r, g, b)
		}
	}
}

// TestAnsiColorRGBAImplementsColor ensures ansiColor satisfies color.Color so it
// can be used as a uv.Style foreground/background.
func TestAnsiColorRGBAImplementsColor(t *testing.T) {
	var _ color.Color = ansiColor(1)
}

func TestVTermLayerDrawNilSnapshotIsNoop(t *testing.T) {
	screen := &bufferScreen{Buffer: uv.NewBuffer(2, 1)}
	sentinel := &uv.Cell{Content: "Z", Width: 1}
	screen.Fill(sentinel)

	// Nil snapshot must not panic and must not write anything.
	layer := NewVTermLayer(nil)
	layer.Draw(screen, screen.Bounds())

	for x := 0; x < 2; x++ {
		if got := screen.CellAt(x, 0).Content; got != "Z" {
			t.Errorf("nil-snapshot Draw modified cell %d: got %q, want sentinel", x, got)
		}
	}
}

func TestVTermLayerDrawEmptyScreenIsNoop(t *testing.T) {
	screen := &bufferScreen{Buffer: uv.NewBuffer(2, 1)}
	sentinel := &uv.Cell{Content: "Z", Width: 1}
	screen.Fill(sentinel)

	// A snapshot whose Screen slice is empty must short-circuit before writing.
	layer := NewVTermLayer(&VTermSnapshot{Width: 2, Height: 1})
	layer.Draw(screen, screen.Bounds())

	for x := 0; x < 2; x++ {
		if got := screen.CellAt(x, 0).Content; got != "Z" {
			t.Errorf("empty-screen Draw modified cell %d: got %q, want sentinel", x, got)
		}
	}
}

func TestVTermLayerDrawRendersGlyphs(t *testing.T) {
	term := vterm.New(3, 2)
	term.Screen[0] = vterm.MakeBlankLine(3)
	term.Screen[1] = vterm.MakeBlankLine(3)
	term.Screen[0][0] = vterm.Cell{Rune: 'a', Width: 1}
	term.Screen[0][1] = vterm.Cell{Rune: 'b', Width: 1}
	term.Screen[1][0] = vterm.Cell{Rune: 'c', Width: 1}

	snap := NewVTermSnapshot(term, false)
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	layer := NewVTermLayer(snap)

	screen := &bufferScreen{Buffer: uv.NewBuffer(3, 2)}
	layer.Draw(screen, screen.Bounds())

	want := map[[2]int]string{
		{0, 0}: "a", {1, 0}: "b", {0, 1}: "c",
	}
	for pos, ch := range want {
		if got := screen.CellAt(pos[0], pos[1]).Content; got != ch {
			t.Errorf("cell (%d,%d) = %q, want %q", pos[0], pos[1], got, ch)
		}
	}
}

func TestVTermLayerDrawZeroRuneBecomesSpace(t *testing.T) {
	term := vterm.New(1, 1)
	// A zero-rune cell (width 1) should render as a space, not NUL.
	term.Screen[0] = []vterm.Cell{{Rune: 0, Width: 1}}

	snap := NewVTermSnapshot(term, false)
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	layer := NewVTermLayer(snap)

	screen := &bufferScreen{Buffer: uv.NewBuffer(1, 1)}
	layer.Draw(screen, screen.Bounds())

	if got := screen.CellAt(0, 0).Content; got != " " {
		t.Errorf("zero rune cell = %q, want space", got)
	}
}

func TestVTermLayerDrawClampsToSnapshotDimensions(t *testing.T) {
	// Snapshot is 2x1 but the draw region is larger; only 2 cells get written.
	term := vterm.New(2, 1)
	term.Screen[0] = []vterm.Cell{{Rune: 'x', Width: 1}, {Rune: 'y', Width: 1}}

	snap := NewVTermSnapshot(term, false)
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	layer := NewVTermLayer(snap)

	screen := &bufferScreen{Buffer: uv.NewBuffer(4, 1)}
	sentinel := &uv.Cell{Content: "Z", Width: 1}
	screen.Fill(sentinel)
	layer.Draw(screen, screen.Bounds())

	if got := screen.CellAt(0, 0).Content; got != "x" {
		t.Errorf("cell (0,0) = %q, want %q", got, "x")
	}
	if got := screen.CellAt(1, 0).Content; got != "y" {
		t.Errorf("cell (1,0) = %q, want %q", got, "y")
	}
	// Cells beyond the snapshot width must remain untouched.
	for x := 2; x < 4; x++ {
		if got := screen.CellAt(x, 0).Content; got != "Z" {
			t.Errorf("cell (%d,0) past snapshot width = %q, want sentinel", x, got)
		}
	}
}

func TestPositionedVTermLayerDrawNilLayerIsNoop(t *testing.T) {
	screen := &bufferScreen{Buffer: uv.NewBuffer(2, 1)}
	sentinel := &uv.Cell{Content: "Z", Width: 1}
	screen.Fill(sentinel)

	// A nil embedded VTermLayer must not panic and must write nothing.
	layer := &PositionedVTermLayer{VTermLayer: nil, Width: 2, Height: 1}
	layer.Draw(screen, screen.Bounds())

	for x := 0; x < 2; x++ {
		if got := screen.CellAt(x, 0).Content; got != "Z" {
			t.Errorf("nil-layer Draw modified cell %d: got %q, want sentinel", x, got)
		}
	}
}

func TestPositionedVTermLayerDrawRendersAtOffset(t *testing.T) {
	term := vterm.New(1, 1)
	term.Screen[0] = []vterm.Cell{{Rune: 'q', Width: 1}}

	snap := NewVTermSnapshot(term, false)
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}

	screen := &bufferScreen{Buffer: uv.NewBuffer(3, 2)}
	sentinel := &uv.Cell{Content: "Z", Width: 1}
	screen.Fill(sentinel)

	layer := &PositionedVTermLayer{
		VTermLayer: NewVTermLayer(snap),
		PosX:       2,
		PosY:       1,
		Width:      1,
		Height:     1,
	}
	// The Rectangle argument is ignored by PositionedVTermLayer.Draw in favor of
	// the layer's own PosX/PosY, so pass an arbitrary one.
	layer.Draw(screen, uv.Rect(0, 0, 3, 2))

	if got := screen.CellAt(2, 1).Content; got != "q" {
		t.Errorf("offset cell (2,1) = %q, want %q", got, "q")
	}
	// Origin cell must remain untouched: PositionedVTermLayer ignores r.Min.
	if got := screen.CellAt(0, 0).Content; got != "Z" {
		t.Errorf("origin cell (0,0) = %q, want sentinel (untouched)", got)
	}
}

func TestVTermSnapshotRespectsViewOffsetChange(t *testing.T) {
	term := vterm.New(2, 1)
	live := vterm.MakeBlankLine(2)
	live[0] = vterm.Cell{Rune: 'A', Width: 1}
	term.Screen[0] = live

	scroll := vterm.MakeBlankLine(2)
	scroll[0] = vterm.Cell{Rune: 'B', Width: 1}
	term.Scrollback = [][]vterm.Cell{scroll}

	term.ViewOffset = 1
	snap := NewVTermSnapshotWithCache(term, true, nil)
	if snap == nil {
		t.Fatalf("expected snapshot, got nil")
	}
	if snap.Screen[0][0].Rune != 'B' {
		t.Fatalf("expected scrollback cell, got %q", snap.Screen[0][0].Rune)
	}

	term.ViewOffset = 0
	snap2 := NewVTermSnapshotWithCache(term, true, snap)
	if snap2 == nil {
		t.Fatalf("expected snapshot, got nil")
	}
	if snap2.Screen[0][0].Rune != 'A' {
		t.Fatalf("expected live cell after ViewOffset reset, got %q", snap2.Screen[0][0].Rune)
	}
}
