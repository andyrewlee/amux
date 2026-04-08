package common

import (
	"testing"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestTrimPTYOverflowPrefix_LeavesSafeBoundaryUntouched(t *testing.T) {
	in := []byte("hello world")
	got, carry := TrimPTYOverflowPrefix(in, len("hello "), vterm.ParserCarryState{})
	if string(got) != "world" {
		t.Fatalf("got %q, want %q", got, "world")
	}
	if carry != (vterm.ParserCarryState{}) {
		t.Fatalf("got carry %+v, want zero", carry)
	}
}

func TestTrimPTYOverflowPrefix_DropsTruncatedCSISequenceTail(t *testing.T) {
	in := []byte("abc\x1b[>1;10;0cxyz")
	got, _ := TrimPTYOverflowPrefix(in, len("abc\x1b[>"), vterm.ParserCarryState{})
	if string(got) != "xyz" {
		t.Fatalf("got %q, want %q", got, "xyz")
	}
}

func TestTrimPTYOverflowPrefix_DropsTruncatedOSCTail(t *testing.T) {
	in := []byte("abc\x1b]0;title\x07xyz")
	got, _ := TrimPTYOverflowPrefix(in, len("abc\x1b]0;"), vterm.ParserCarryState{})
	if string(got) != "xyz" {
		t.Fatalf("got %q, want %q", got, "xyz")
	}
}

func TestTrimPTYOverflowPrefix_DropsTruncatedUTF8RuneTail(t *testing.T) {
	in := append([]byte("a"), []byte("😀b")...)
	got, _ := TrimPTYOverflowPrefix(in, 2, vterm.ParserCarryState{})
	if string(got) != "b" {
		t.Fatalf("got %q, want %q", got, "b")
	}
}

func TestTrimPTYOverflowPrefix_DropsLeadingCSIContinuationAfterParserReset(t *testing.T) {
	in := []byte("31mvisible")
	got, _ := TrimPTYOverflowPrefix(in, 0, vterm.ParserCarryState{Mode: vterm.ParserCarryCSI})
	if string(got) != "visible" {
		t.Fatalf("got %q, want %q", got, "visible")
	}
}

func TestTrimPTYOverflowPrefix_DropsSecondaryDAAfterEscapeCarry(t *testing.T) {
	in := []byte("[>1;10;0cvisible")
	got, _ := TrimPTYOverflowPrefix(in, 0, vterm.ParserCarryState{Mode: vterm.ParserCarryEscape})
	if string(got) != "visible" {
		t.Fatalf("got %q, want %q", got, "visible")
	}
}

func TestTrimPTYOverflowPrefix_DropsSecondaryDAAfterCSIParamCarry(t *testing.T) {
	in := []byte("1;10;0cvisible")
	got, _ := TrimPTYOverflowPrefix(in, 0, vterm.ParserCarryState{Mode: vterm.ParserCarryCSIParam})
	if string(got) != "visible" {
		t.Fatalf("got %q, want %q", got, "visible")
	}
}

func TestTrimPTYOverflowPrefix_AllowsFreshEscapeAfterTruncatedOSC(t *testing.T) {
	in := []byte("title\x1b[31mred")
	got, _ := TrimPTYOverflowPrefix(in, 0, vterm.ParserCarryState{Mode: vterm.ParserCarryOSC})
	if string(got) != "\x1b[31mred" {
		t.Fatalf("got %q, want %q", got, "\x1b[31mred")
	}
}

func TestTrimPTYOverflowPrefix_AllowsFreshEscapeAfterTruncatedUTF8(t *testing.T) {
	in := []byte("\x1b[31mred")
	got, _ := TrimPTYOverflowPrefix(in, 0, vterm.ParserCarryState{UTF8Remaining: 1})
	if string(got) != "\x1b[31mred" {
		t.Fatalf("got %q, want %q", got, "\x1b[31mred")
	}
}

func TestTrimPTYOverflowPrefix_PreservesFirstValidByteAfterTruncatedUTF8Carry(t *testing.T) {
	in := []byte{0x80, 'A', 'B'}
	got, _ := TrimPTYOverflowPrefix(in, 1, vterm.ParserCarryState{UTF8Remaining: 2})
	if string(got) != "AB" {
		t.Fatalf("got %q, want %q", got, "AB")
	}
}

func TestTrimPTYOverflowPrefix_KeepsTextAfterUnsupportedEscapeFamily(t *testing.T) {
	in := []byte("abc\x1bXhidden")
	got, _ := TrimPTYOverflowPrefix(in, len("abc\x1b"), vterm.ParserCarryState{})
	if string(got) != "hidden" {
		t.Fatalf("got %q, want %q", got, "hidden")
	}
}

func TestTrimPTYOverflowPrefix_AllowsFreshEscapeAfterTruncatedEscapeCarry(t *testing.T) {
	in := []byte("\x1b[31mX")
	got, _ := TrimPTYOverflowPrefix(in, 0, vterm.ParserCarryState{Mode: vterm.ParserCarryEscape})
	if string(got) != "\x1b[31mX" {
		t.Fatalf("got %q, want %q", got, "\x1b[31mX")
	}
}

func TestTrimPTYOverflowPrefix_PreservesCarryWhenNoBoundaryRemains(t *testing.T) {
	in := []byte("title")
	got, carry := TrimPTYOverflowPrefix(in, 0, vterm.ParserCarryState{Mode: vterm.ParserCarryOSC})
	if got != nil {
		t.Fatalf("got %q, want nil", got)
	}
	if carry != (vterm.ParserCarryState{Mode: vterm.ParserCarryOSC}) {
		t.Fatalf("got carry %+v, want OSC carry", carry)
	}
}
