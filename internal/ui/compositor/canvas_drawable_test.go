package compositor

import (
	"image/color"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// colorEqualRGBA compares two color.Color values by their RGBA components.
// A nil color is only equal to another nil color.
func colorEqualRGBA(t *testing.T, got, want color.Color) bool {
	t.Helper()
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	gr, gg, gb, ga := got.RGBA()
	wr, wg, wb, wa := want.RGBA()
	return gr == wr && gg == wg && gb == wb && ga == wa
}

func TestNewStringDrawable(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		x, y       int
		wantWidth  int
		wantHeight int
		wantLines  []string
	}{
		{
			name:       "empty string yields single empty line",
			content:    "",
			x:          0,
			y:          0,
			wantWidth:  0,
			wantHeight: 1,
			wantLines:  []string{""},
		},
		{
			name:       "single line uses its display width",
			content:    "hello",
			x:          3,
			y:          7,
			wantWidth:  5,
			wantHeight: 1,
			wantLines:  []string{"hello"},
		},
		{
			name:       "width is the max of all lines",
			content:    "ab\nabcd\nabc",
			x:          0,
			y:          0,
			wantWidth:  4,
			wantHeight: 3,
			wantLines:  []string{"ab", "abcd", "abc"},
		},
		{
			name:       "ansi escapes do not count toward display width",
			content:    "\x1b[31mred\x1b[0m",
			x:          0,
			y:          0,
			wantWidth:  3,
			wantHeight: 1,
			wantLines:  []string{"\x1b[31mred\x1b[0m"},
		},
		{
			name:       "wide runes count as two columns",
			content:    "你好",
			x:          0,
			y:          0,
			wantWidth:  4,
			wantHeight: 1,
			wantLines:  []string{"你好"},
		},
		{
			name:       "trailing newline produces an empty final line",
			content:    "line\n",
			x:          2,
			y:          5,
			wantWidth:  4,
			wantHeight: 2,
			wantLines:  []string{"line", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewStringDrawable(tt.content, tt.x, tt.y)
			if d == nil {
				t.Fatal("NewStringDrawable returned nil")
			}
			if d.content != tt.content {
				t.Errorf("content = %q, want %q", d.content, tt.content)
			}
			if d.x != tt.x || d.y != tt.y {
				t.Errorf("position = (%d,%d), want (%d,%d)", d.x, d.y, tt.x, tt.y)
			}
			if d.width != tt.wantWidth {
				t.Errorf("width = %d, want %d", d.width, tt.wantWidth)
			}
			if d.height != tt.wantHeight {
				t.Errorf("height = %d, want %d", d.height, tt.wantHeight)
			}
			if len(d.lines) != len(tt.wantLines) {
				t.Fatalf("lines = %#v, want %#v", d.lines, tt.wantLines)
			}
			for i := range tt.wantLines {
				if d.lines[i] != tt.wantLines[i] {
					t.Errorf("lines[%d] = %q, want %q", i, d.lines[i], tt.wantLines[i])
				}
			}
			// height always equals the number of lines.
			if d.height != len(d.lines) {
				t.Errorf("height %d should equal len(lines) %d", d.height, len(d.lines))
			}
		})
	}
}

func TestStringDrawableImplementsDrawable(t *testing.T) {
	var _ uv.Drawable = NewStringDrawable("x", 0, 0)
}

func TestStringDrawableDrawEmptyIsNoop(t *testing.T) {
	buf := uv.NewScreenBuffer(4, 1)
	// Pre-fill with a sentinel so we can detect any unexpected writes.
	sentinel := &uv.Cell{Content: "Z", Width: 1}
	buf.Fill(sentinel)

	d := NewStringDrawable("", 0, 0)
	d.Draw(buf, buf.Bounds())

	for x := 0; x < 4; x++ {
		if got := buf.CellAt(x, 0).Content; got != "Z" {
			t.Errorf("empty Draw modified cell %d: got %q, want sentinel", x, got)
		}
	}
}

func TestStringDrawableDrawPlainText(t *testing.T) {
	buf := uv.NewScreenBuffer(5, 1)
	d := NewStringDrawable("hi", 1, 0)
	d.Draw(buf, buf.Bounds())

	if got := buf.CellAt(1, 0).Content; got != "h" {
		t.Errorf("cell (1,0) = %q, want %q", got, "h")
	}
	if got := buf.CellAt(2, 0).Content; got != "i" {
		t.Errorf("cell (2,0) = %q, want %q", got, "i")
	}
	// Cell at the offset x=0 should be untouched (empty space).
	if got := buf.CellAt(0, 0).Content; got == "h" || got == "i" {
		t.Errorf("cell (0,0) should be untouched, got %q", got)
	}
}

func TestStringDrawableDrawMultiLine(t *testing.T) {
	buf := uv.NewScreenBuffer(3, 3)
	d := NewStringDrawable("ab\ncd", 0, 0)
	d.Draw(buf, buf.Bounds())

	want := map[[2]int]string{
		{0, 0}: "a", {1, 0}: "b",
		{0, 1}: "c", {1, 1}: "d",
	}
	for pos, ch := range want {
		if got := buf.CellAt(pos[0], pos[1]).Content; got != ch {
			t.Errorf("cell (%d,%d) = %q, want %q", pos[0], pos[1], got, ch)
		}
	}
}

func TestStringDrawableDrawClipsOutOfBounds(t *testing.T) {
	// A 3-line drawable positioned so only the middle row falls inside the
	// clip rectangle [y=1, y=2).
	buf := uv.NewScreenBuffer(2, 3)
	d := NewStringDrawable("aa\nbb\ncc", 0, 0)
	clip := uv.Rect(0, 1, 2, 1) // only row y=1 is drawable

	d.Draw(buf, clip)

	// Row 0 and row 2 must be untouched; row 1 ("bb") must be written.
	if got := buf.CellAt(0, 0).Content; got == "a" {
		t.Errorf("row 0 should be clipped out, got %q", got)
	}
	if got := buf.CellAt(0, 2).Content; got == "c" {
		t.Errorf("row 2 should be clipped out, got %q", got)
	}
	if got := buf.CellAt(0, 1).Content; got != "b" {
		t.Errorf("row 1 should be drawn, got %q", got)
	}
	if got := buf.CellAt(1, 1).Content; got != "b" {
		t.Errorf("row 1 col 1 should be drawn, got %q", got)
	}
}

func TestStringDrawableDrawClipsHorizontally(t *testing.T) {
	// Draw "abcd" at x=0 into a clip window that only allows columns [1,3).
	buf := uv.NewScreenBuffer(4, 1)
	d := NewStringDrawable("abcd", 0, 0)
	clip := uv.Rect(1, 0, 2, 1) // columns 1 and 2 only

	d.Draw(buf, clip)

	if got := buf.CellAt(0, 0).Content; got == "a" {
		t.Errorf("col 0 should be clipped, got %q", got)
	}
	if got := buf.CellAt(1, 0).Content; got != "b" {
		t.Errorf("col 1 = %q, want %q", got, "b")
	}
	if got := buf.CellAt(2, 0).Content; got != "c" {
		t.Errorf("col 2 = %q, want %q", got, "c")
	}
	if got := buf.CellAt(3, 0).Content; got == "d" {
		t.Errorf("col 3 should be clipped, got %q", got)
	}
}

func TestStringDrawableDrawAppliesStyle(t *testing.T) {
	// "\x1b[1;31mX" -> bold + red foreground on X.
	buf := uv.NewScreenBuffer(1, 1)
	d := NewStringDrawable("\x1b[1;31mX", 0, 0)
	d.Draw(buf, buf.Bounds())

	cell := buf.CellAt(0, 0)
	if cell.Content != "X" {
		t.Fatalf("content = %q, want %q", cell.Content, "X")
	}
	if cell.Style.Attrs&uv.AttrBold == 0 {
		t.Errorf("expected bold attribute on cell, attrs = %d", cell.Style.Attrs)
	}
	if !colorEqualRGBA(t, cell.Style.Fg, ansiColor(1)) {
		t.Errorf("expected red (ansi 1) foreground, got %#v", cell.Style.Fg)
	}
}

func TestStringDrawableDrawWideRune(t *testing.T) {
	buf := uv.NewScreenBuffer(4, 1)
	d := NewStringDrawable("你x", 0, 0)
	d.Draw(buf, buf.Bounds())

	wide := buf.CellAt(0, 0)
	if wide.Content != "你" {
		t.Errorf("cell (0,0) = %q, want %q", wide.Content, "你")
	}
	if wide.Width != 2 {
		t.Errorf("wide cell width = %d, want 2", wide.Width)
	}
	// The narrow rune lands at x=2 because the wide rune occupied 0..1.
	if got := buf.CellAt(2, 0).Content; got != "x" {
		t.Errorf("cell (2,0) = %q, want %q", got, "x")
	}
}

func TestRGBColorValRGBA(t *testing.T) {
	tests := []struct {
		name                   string
		c                      rgbColorVal
		wantR, wantG, wantB, a uint32
	}{
		{
			name:  "zero is fully black, opaque",
			c:     rgbColorVal{0, 0, 0},
			wantR: 0, wantG: 0, wantB: 0, a: 65535,
		},
		{
			name:  "max is fully white, opaque",
			c:     rgbColorVal{255, 255, 255},
			wantR: 65535, wantG: 65535, wantB: 65535, a: 65535,
		},
		{
			name:  "mid value scales by 257",
			c:     rgbColorVal{1, 2, 3},
			wantR: 257, wantG: 514, wantB: 771, a: 65535,
		},
		{
			name:  "distinct channels are independent",
			c:     rgbColorVal{10, 20, 30},
			wantR: 2570, wantG: 5140, wantB: 7710, a: 65535,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, g, b, a := tt.c.RGBA()
			if r != tt.wantR || g != tt.wantG || b != tt.wantB || a != tt.a {
				t.Errorf("RGBA() = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
					r, g, b, a, tt.wantR, tt.wantG, tt.wantB, tt.a)
			}
			// Alpha is always fully opaque.
			if a != 65535 {
				t.Errorf("alpha = %d, want 65535 (opaque)", a)
			}
		})
	}
}

// TestRGBColorValImplementsColor ensures rgbColorVal satisfies color.Color so
// it can be assigned to a uv.Style's Fg/Bg fields.
func TestRGBColorValImplementsColor(t *testing.T) {
	var _ color.Color = rgbColorVal{1, 2, 3}
}
