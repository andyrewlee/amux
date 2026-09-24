package vterm

import (
	"strings"
	"testing"
)

// fillRow writes exactly Width glyphs, leaving the terminal in the
// pending-wrap state (CursorX == Width, deferred wrap on next write).
func fillRowToEdge(vt *VTerm, ch string) {
	vt.Write([]byte(strings.Repeat(ch, vt.Width)))
}

// TestPendingWrapSetOnLastColumn pins the flag's set semantics.
func TestPendingWrapSetOnLastColumn(t *testing.T) {
	vt := New(10, 3)
	fillRowToEdge(vt, "a")
	if !vt.PendingWrap {
		t.Fatal("PendingWrap not set after filling the last column")
	}
	if vt.CursorX != vt.Width {
		t.Fatalf("CursorX = %d, want %d (raw counter stays at Width)", vt.CursorX, vt.Width)
	}
}

// TestPendingWrapEraseLineLastCell covers the EL-during-pending-wrap bug:
// ESC[K after filling the last column must erase that cell, not stop short.
func TestPendingWrapEraseLineLastCell(t *testing.T) {
	vt := New(10, 3)
	fillRowToEdge(vt, "a")
	vt.Write([]byte("\x1b[K"))
	row := vt.Screen[0]
	// The pending cursor sits on the last cell: erase covers only it.
	for x := 0; x < vt.Width-1; x++ {
		if row[x].Rune != 'a' {
			t.Fatalf("cell %d = %q after ESC[K, want 'a' preserved", x, row[x].Rune)
		}
	}
	if r := row[vt.Width-1].Rune; r != 0 && r != ' ' {
		t.Fatalf("last cell = %q after ESC[K, want blank", r)
	}
	// Erase is not a cursor op — pending wrap survives it.
	if !vt.PendingWrap {
		t.Fatal("erase cleared PendingWrap — erase ops do not move the cursor")
	}
}

// TestPendingWrapEraseDisplayLastCell covers ED mode 0's cursor-line segment.
func TestPendingWrapEraseDisplayLastCell(t *testing.T) {
	vt := New(10, 3)
	vt.Write([]byte("\x1b[2;1Hbb\x1b[1;1H")) // 'bb' on row index 1, cursor home
	fillRowToEdge(vt, "a")
	vt.Write([]byte("\x1b[J"))
	for x := 0; x < vt.Width-1; x++ {
		if r := vt.Screen[0][x].Rune; r != 'a' {
			t.Fatalf("cell %d = %q after ESC[J, want 'a' preserved", x, r)
		}
	}
	if r := vt.Screen[0][vt.Width-1].Rune; r != 0 && r != ' ' {
		t.Fatalf("last cell = %q after ESC[J, want blank", r)
	}
	for x := 0; x < vt.Width; x++ {
		if r := vt.Screen[1][x].Rune; r != 0 && r != ' ' {
			t.Fatalf("row 1 cell %d = %q after ESC[J, want blank", x, r)
		}
	}
}

// TestPendingWrapCursorReport covers the DSR off-by-one: during pending wrap
// the report must say Width, not Width+1.
func TestPendingWrapCursorReport(t *testing.T) {
	vt := New(10, 3)
	var responses []string
	vt.SetResponseWriter(func(b []byte) {
		responses = append(responses, string(b))
	})
	fillRowToEdge(vt, "a")
	vt.Write([]byte("\x1b[6n"))
	if len(responses) != 1 {
		t.Fatalf("responses = %v, want exactly one", responses)
	}
	if responses[0] != "\x1b[1;10R" {
		t.Fatalf("CPR = %q, want %q (column 10, not 11)", responses[0], "\x1b[1;10R")
	}
}

// TestPendingWrapClearedByCursorOp verifies a cursor op (\r) ends the state
// and a subsequent ESC[K erases the whole line from column 0.
func TestPendingWrapClearedByCursorOp(t *testing.T) {
	vt := New(10, 3)
	fillRowToEdge(vt, "a")
	vt.Write([]byte("\r"))
	if vt.PendingWrap {
		t.Fatal("carriageReturn left PendingWrap set")
	}
	vt.Write([]byte("\x1b[K"))
	for x := 0; x < vt.Width; x++ {
		if r := vt.Screen[0][x].Rune; r != 0 && r != ' ' {
			t.Fatalf("cell %d = %q after \\r ESC[K, want blank", x, r)
		}
	}
}

// TestPendingWrapClearedByNextWrite verifies the deferred wrap still fires:
// a putChar in the pending state wraps to the next row and clears the flag.
func TestPendingWrapClearedByNextWrite(t *testing.T) {
	vt := New(10, 3)
	fillRowToEdge(vt, "a")
	vt.Write([]byte("b"))
	if vt.PendingWrap {
		t.Fatal("PendingWrap still set after the wrapping write")
	}
	if vt.CursorY != 1 || vt.CursorX != 1 {
		t.Fatalf("cursor at (%d,%d), want (1,1) after wrap", vt.CursorX, vt.CursorY)
	}
	if r := vt.Screen[1][0].Rune; r != 'b' {
		t.Fatalf("wrapped cell = %q, want 'b'", r)
	}
}

// TestPendingWrapCursorPositionAbsolute covers CHA — a non-clamped direct
// cursor set — ending the state.
func TestPendingWrapCursorPositionAbsolute(t *testing.T) {
	vt := New(10, 3)
	fillRowToEdge(vt, "a")
	vt.Write([]byte("\x1b[3G"))
	if vt.PendingWrap {
		t.Fatal("CHA left PendingWrap set")
	}
	if vt.CursorX != 2 {
		t.Fatalf("CursorX = %d, want 2", vt.CursorX)
	}
}
