package vterm

import (
	"testing"
	"unicode/utf8"
)

// assertVTermInvariants pins the screen-geometry contract the render path
// depends on: cursor in bounds (CursorX == Width is legal — tmux-style
// wrap-on-next-write keeps it there until the next printable byte), the
// screen always has Height rows of Width cells, and scrollback never
// outgrows MaxScrollback.
func assertVTermInvariants(t *testing.T, vt *VTerm) {
	t.Helper()
	if vt.CursorY < 0 || vt.CursorY >= vt.Height {
		t.Fatalf("CursorY = %d out of bounds [0,%d)", vt.CursorY, vt.Height)
	}
	if vt.CursorX < 0 || vt.CursorX > vt.Width {
		t.Fatalf("CursorX = %d out of bounds [0,%d]", vt.CursorX, vt.Width)
	}
	if len(vt.Screen) != vt.Height {
		t.Fatalf("screen has %d rows, want %d", len(vt.Screen), vt.Height)
	}
	for i, row := range vt.Screen {
		if len(row) != vt.Width {
			t.Fatalf("screen row %d has %d cells, want %d", i, len(row), vt.Width)
		}
	}
	if len(vt.Scrollback) > MaxScrollback {
		t.Fatalf("scrollback has %d rows, exceeds MaxScrollback %d", len(vt.Scrollback), MaxScrollback)
	}
	for i, row := range vt.Scrollback {
		// Scrollback stores trimmed physical rows — empty rows are len 0
		// and a populated row never exceeds Width.
		if len(row) > vt.Width {
			t.Fatalf("scrollback row %d has %d cells, exceeds width %d", i, len(row), vt.Width)
		}
	}
}

func FuzzANSIParser(f *testing.F) {
	f.Add([]byte("hello"))
	f.Add([]byte("\x1b[31mred\x1b[0m"))
	f.Add([]byte("\x1b[?1049h\x1b[H\x1b[2J"))
	// OSC8 hyperlink, SGR with params, CSI mid-OSC (cross-state), C1-ST
	// OSC termination, DCS, and a truncated multi-byte grapheme.
	f.Add([]byte("\x1b]8;;https://example.com\x07link\x1b]8;;\x07"))
	f.Add([]byte("\x1b[1;38;5;196mcolor\x1b[0m"))
	f.Add([]byte("\x1b]0;t\x1b[31m"))
	f.Add([]byte("\x1b]0;c1title\x9ctail"))
	f.Add([]byte("\x1bPq#0;2;0;0;0\x1b\\after"))
	f.Add([]byte{'h', 'i', 0xF0, 0x9F})
	f.Fuzz(func(t *testing.T, data []byte) {
		vt := New(80, 24)
		p := NewParser(vt)
		p.Parse(data)
		assertVTermInvariants(t, vt)
	})
}

func FuzzRenderInvariant(f *testing.F) {
	f.Add([]byte("line1\nline2"))
	f.Add([]byte("\x1b[1mBold\x1b[0m"))
	f.Add([]byte("\x1b]0;title\x07"))
	f.Add([]byte("\x1b]0;title\x9c")) // C1 ST — 8-bit control producer path
	f.Add([]byte("\x1bPq+dcs payload\x1b\\normal"))
	f.Add([]byte("\x1b[?1049halt\x1b[?1049l back"))
	f.Add([]byte{'\xE2', '\x82'}) // truncated 3-byte UTF-8
	f.Add([]byte("\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\nscroll"))
	f.Fuzz(func(t *testing.T, data []byte) {
		vt := New(80, 24)
		vt.Write(data)
		out := vt.Render()
		if !utf8.ValidString(out) {
			t.Fatalf("render output is not valid utf-8")
		}
		assertVTermInvariants(t, vt)
	})
}
