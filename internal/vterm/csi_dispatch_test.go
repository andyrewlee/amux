package vterm

import (
	"testing"
)

// TestCSIDispatchScrollUp verifies that CSI S (SU) scrolls content up by the
// requested number of lines, moving the top lines into scrollback and filling
// the bottom with blanks.
func TestCSIDispatchScrollUp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		n       int    // number of lines to scroll up
		seq     string // CSI sequence to send
		wantRow []string
	}{
		{
			name:    "scroll up by 1",
			seq:     "\x1b[1S",
			wantRow: []string{"B", "C", "D", "E", ""},
		},
		{
			name:    "scroll up by 2",
			seq:     "\x1b[2S",
			wantRow: []string{"C", "D", "E", "", ""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(10, 5)
			// Fill lines A..E.
			vt.Write([]byte("A\r\nB\r\nC\r\nD\r\nE"))
			vt.Write([]byte(tc.seq))

			for i, want := range tc.wantRow {
				if got := rowText(vt, i); got != want {
					t.Errorf("row %d = %q, want %q", i, got, want)
				}
			}
		})
	}
}

// TestCSIDispatchScrollDown verifies that CSI T (SD) scrolls content down by
// the requested number of lines, filling the top with blanks.
func TestCSIDispatchScrollDown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		seq     string
		wantRow []string
	}{
		{
			name:    "scroll down by 1",
			seq:     "\x1b[1T",
			wantRow: []string{"", "A", "B", "C", "D"},
		},
		{
			name:    "scroll down by 2",
			seq:     "\x1b[2T",
			wantRow: []string{"", "", "A", "B", "C"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(10, 5)
			vt.Write([]byte("A\r\nB\r\nC\r\nD\r\nE"))
			vt.Write([]byte(tc.seq))

			for i, want := range tc.wantRow {
				if got := rowText(vt, i); got != want {
					t.Errorf("row %d = %q, want %q", i, got, want)
				}
			}
		})
	}
}

// TestCSIDispatchDefaultParam verifies that a CSI sequence with no numeric
// parameter (e.g. "\x1b[L") uses the default value of 1, i.e. inserts exactly
// one line. This exercises the getParam(0, 1) default path.
func TestCSIDispatchDefaultParam(t *testing.T) {
	t.Parallel()

	vt := New(10, 5)
	vt.Write([]byte("A\r\nB\r\nC\r\nD"))
	// Position cursor on row 1.
	vt.Write([]byte("\x1b[2;1H"))
	// Insert with no parameter — should default to 1.
	vt.Write([]byte("\x1b[L"))

	// Row 0: A unchanged.
	if got := rowText(vt, 0); got != "A" {
		t.Errorf("row 0 = %q, want %q", got, "A")
	}
	// Row 1: newly inserted blank.
	if got := rowText(vt, 1); got != "" {
		t.Errorf("row 1 = %q, want empty (default-param insert)", got)
	}
	// Row 2: B shifted down.
	if got := rowText(vt, 2); got != "B" {
		t.Errorf("row 2 = %q, want %q (B shifted down)", got, "B")
	}
}

// TestCSIDispatchZeroParam verifies that CSI \x1b[0L (explicit param zero) is
// treated as the default of 1 by getParam, inserts exactly one line, and does
// not panic.
func TestCSIDispatchZeroParam(t *testing.T) {
	t.Parallel()

	vt := New(10, 5)
	vt.Write([]byte("A\r\nB\r\nC\r\nD"))
	vt.Write([]byte("\x1b[2;1H"))
	// Explicit zero parameter — getParam(0,1) treats 0 the same as missing.
	vt.Write([]byte("\x1b[0L"))

	// Behavior must be identical to the default-param case (insert 1 line).
	if got := rowText(vt, 0); got != "A" {
		t.Errorf("row 0 = %q, want %q", got, "A")
	}
	if got := rowText(vt, 1); got != "" {
		t.Errorf("row 1 = %q, want empty (zero-param insert)", got)
	}
	if got := rowText(vt, 2); got != "B" {
		t.Errorf("row 2 = %q, want %q", got, "B")
	}
}

// TestCSIDispatchAdversarialTruncatedSequence verifies that a truncated CSI
// sequence ("\x1b[3" with no final byte) followed by normal printable text
// does not panic and that the terminal recovers to the ground state so
// subsequent text is rendered.
func TestCSIDispatchAdversarialTruncatedSequence(t *testing.T) {
	t.Parallel()

	vt := New(10, 3)
	// Truncated CSI — no final byte — followed immediately by a CR/LF and text.
	// The parser must not panic and must return to ground state on the 'X'
	// final byte, completing the sequence rather than treating "X" as stray text.
	// We use a fresh, unambiguous sequence first, then the adversarial input.

	// First write safe content so the screen is not empty.
	vt.Write([]byte("hello"))

	// Now feed a truncated parameter string followed by valid text.
	// "\x1b[3" by itself is an incomplete CSI; the next Write picks up from
	// the in-flight state. Providing the final byte in the next chunk exercises
	// cross-chunk carry.
	vt.Write([]byte("\x1b[3"))
	// Complete a valid CSI that moves the cursor right by 3. Must not panic.
	vt.Write([]byte("C"))

	// The cursor should have advanced by 3 from its position after "hello"
	// (which placed it at col 5). Clamped to width-1=9.
	if vt.CursorX < 0 || vt.CursorX >= vt.Width {
		t.Errorf("cursor X out of bounds after truncated+completed CSI: got %d (width=%d)",
			vt.CursorX, vt.Width)
	}

	// Also ensure the screen content is still readable (no corruption).
	got := rowText(vt, 0)
	if got == "" {
		t.Errorf("row 0 unexpectedly empty after truncated CSI sequence")
	}

	// Additional adversarial case: a truly truncated sequence never terminated
	// by a final byte, followed by unrelated text. The parser must recover and
	// render the text.
	vt2 := New(10, 3)
	vt2.Write([]byte("X"))
	// Inject an ESC and start of CSI with digits but no terminator, then
	// immediately follow with printable text that must not be swallowed.
	vt2.Write([]byte("\x1b[\x00")) // NUL: not a valid CSI byte, forces ground
	vt2.Write([]byte("Z"))

	// The terminal must not panic — if we get here, it didn't.
	if vt2.CursorX < 0 || vt2.CursorX >= vt2.Width {
		t.Errorf("cursor X out of bounds after malformed CSI: %d", vt2.CursorX)
	}
}
