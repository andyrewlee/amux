package vterm

import (
	"testing"
)

func TestBCE_EraseLine(t *testing.T) {
	t.Parallel()

	vt := New(10, 2)
	// Write with black truecolor background: \x1b[48;2;10;10;10m
	vt.Write([]byte("\x1b[48;2;10;10;10mHello\x1b[K"))

	// Row 0 columns 0..4 have "Hello" with bg 10,10,10
	// Row 0 columns 5..9 are erased via \x1b[K and should have bg 10,10,10
	wantBg := Color{Type: ColorRGB, Value: 0x0a0a0a}
	for x := 0; x < 10; x++ {
		cell := vt.Screen[0][x]
		if cell.Style.Bg != wantBg {
			t.Errorf("cell (x=%d, y=0) bg = %+v, want %+v", x, cell.Style.Bg, wantBg)
		}
	}
}

func TestBCE_EraseDisplay(t *testing.T) {
	t.Parallel()

	vt := New(10, 3)
	// Set background to indexed color 4 (blue) and clear screen
	vt.Write([]byte("\x1b[44m\x1b[2J"))

	wantBg := Color{Type: ColorIndexed, Value: 4}
	for y := 0; y < 3; y++ {
		for x := 0; x < 10; x++ {
			cell := vt.Screen[y][x]
			if cell.Style.Bg != wantBg {
				t.Errorf("cell (x=%d, y=%d) bg = %+v, want %+v", x, y, cell.Style.Bg, wantBg)
			}
		}
	}
}

func TestBCE_ScrollUp(t *testing.T) {
	t.Parallel()

	vt := New(5, 2)
	vt.Write([]byte("\x1b[48;2;20;30;40mLine1\nLine2\n"))

	wantBg := Color{Type: ColorRGB, Value: 0x141e28}
	for x := 0; x < 5; x++ {
		cell := vt.Screen[1][x]
		if cell.Style.Bg != wantBg {
			t.Errorf("scrolled cell (x=%d, y=1) bg = %+v, want %+v", x, cell.Style.Bg, wantBg)
		}
	}
}

func TestBCE_EraseChars(t *testing.T) {
	t.Parallel()

	vt := New(5, 1)
	vt.Write([]byte("ABCDE\x1b[1;2H\x1b[42m\x1b[2X"))

	// Cursor at (1, 0), erase 2 chars ('B' and 'C') with green background (indexed 2)
	wantBg := Color{Type: ColorIndexed, Value: 2}
	if vt.Screen[0][1].Style.Bg != wantBg || vt.Screen[0][1].Rune != ' ' {
		t.Errorf("cell 1 = %+v, want space with green bg", vt.Screen[0][1])
	}
	if vt.Screen[0][2].Style.Bg != wantBg || vt.Screen[0][2].Rune != ' ' {
		t.Errorf("cell 2 = %+v, want space with green bg", vt.Screen[0][2])
	}
	// 'A' and 'D' should retain their original style
	if vt.Screen[0][0].Rune != 'A' || vt.Screen[0][0].Style.Bg.Type != ColorDefault {
		t.Errorf("cell 0 = %+v, want 'A' with default bg", vt.Screen[0][0])
	}
}

func TestBCE_ResetRestoresDefault(t *testing.T) {
	t.Parallel()

	vt := New(5, 1)
	vt.Write([]byte("\x1b[41m\x1b[K\x1b[0m\x1b[1;1H\x1b[K"))

	// After \x1b[0m, eraseLine should use default bg
	for x := 0; x < 5; x++ {
		if vt.Screen[0][x].Style.Bg.Type != ColorDefault {
			t.Errorf("cell %d bg = %+v, want default", x, vt.Screen[0][x].Style.Bg)
		}
	}
}

func TestBCE_InsertDeleteLines(t *testing.T) {
	t.Parallel()

	vt := New(4, 3)
	vt.Write([]byte("\x1b[45m\x1b[1;1H\x1b[1L")) // IL with magenta bg

	wantBg := Color{Type: ColorIndexed, Value: 5}
	for x := 0; x < 4; x++ {
		if vt.Screen[0][x].Style.Bg != wantBg {
			t.Errorf("inserted line cell %d bg = %+v, want %+v", x, vt.Screen[0][x].Style.Bg, wantBg)
		}
	}

	vt.Write([]byte("\x1b[46m\x1b[1;1H\x1b[1M")) // DL with cyan bg
	wantBg2 := Color{Type: ColorIndexed, Value: 6}
	for x := 0; x < 4; x++ {
		if vt.Screen[2][x].Style.Bg != wantBg2 {
			t.Errorf("bottom line after delete cell %d bg = %+v, want %+v", x, vt.Screen[2][x].Style.Bg, wantBg2)
		}
	}
}

func TestBCE_ScrollDown(t *testing.T) {
	t.Parallel()

	vt := New(4, 2)
	vt.Write([]byte("\x1b[43m\x1b[1;1H\x1b[1T")) // SD with yellow bg

	wantBg := Color{Type: ColorIndexed, Value: 3}
	for x := 0; x < 4; x++ {
		if vt.Screen[0][x].Style.Bg != wantBg {
			t.Errorf("top line after scrollDown cell %d bg = %+v, want %+v", x, vt.Screen[0][x].Style.Bg, wantBg)
		}
	}
}
