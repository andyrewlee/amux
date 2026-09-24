package vterm

import "testing"

// TestSGRColonUnderlineStyle pins the grouped-subparameter fix: colon-form
// underline selectors must not execute their subparameters as standalone
// codes. Flattening made "\x1b[4:3m" (curly underline) set underline AND
// italic, and "\x1b[4:0m" (underline-off) set underline then full-reset.
func TestSGRColonUnderlineStyle(t *testing.T) {
	t.Parallel()
	vt := New(80, 24)

	// Curly underline (Smulx form): underline on, no italic side effect.
	vt.Write([]byte("\x1b[4:3m"))
	if !vt.CurrentStyle.Underline {
		t.Error("4:3 should set Underline")
	}
	if vt.CurrentStyle.Italic {
		t.Error("4:3 must not set Italic (subparameter 3 is not code 3)")
	}

	// Colon-form underline-off: clears underline, preserves other attrs.
	vt.Write([]byte("\x1b[31m\x1b[1m")) // red + bold
	vt.Write([]byte("\x1b[4:0m"))
	if vt.CurrentStyle.Underline {
		t.Error("4:0 should clear Underline")
	}
	if vt.CurrentStyle.Fg.Type != ColorIndexed || vt.CurrentStyle.Fg.Value != 1 {
		t.Error("4:0 must not reset other attributes (red FG lost)")
	}
	if !vt.CurrentStyle.Bold {
		t.Error("4:0 must not reset other attributes (bold lost)")
	}
}

// TestSGRColonRGBEmptyColorspace pins the ITU recommended form: the empty
// colorspace-ID subparameter in "38:2::r:g:b" is skipped, not read as red=0.
func TestSGRColonRGBEmptyColorspace(t *testing.T) {
	t.Parallel()
	vt := New(80, 24)

	vt.Write([]byte("\x1b[38:2::255:128:0m"))
	if vt.CurrentStyle.Fg.Type != ColorRGB {
		t.Fatalf("Expected ColorRGB, got %v", vt.CurrentStyle.Fg.Type)
	}
	expected := uint32(255)<<16 | uint32(128)<<8 | uint32(0)
	if vt.CurrentStyle.Fg.Value != expected {
		t.Errorf("Expected RGB value %d, got %d", expected, vt.CurrentStyle.Fg.Value)
	}
}

// TestSGRColonUnderlineColorIgnored pins 58 (Setulc) as parse-and-discard:
// its interior values must not be interpreted as standalone codes.
func TestSGRColonUnderlineColorIgnored(t *testing.T) {
	t.Parallel()
	vt := New(80, 24)

	vt.Write([]byte("\x1b[31m\x1b[1m")) // red + bold
	vt.Write([]byte("\x1b[58:2::1:2:3m"))
	if vt.CurrentStyle.Fg.Type != ColorIndexed || vt.CurrentStyle.Fg.Value != 1 {
		t.Error("58:2::1:2:3 must not alter FG (interior values are not codes)")
	}
	if !vt.CurrentStyle.Bold {
		t.Error("58:2::1:2:3 must not reset Bold")
	}
	if vt.CurrentStyle.Dim {
		t.Error("58:2::1:2:3 must not set Dim (interior 2 is not code 2)")
	}
}

// TestSGRColonMixedWithSemicolons confirms grouped and flat parameters
// coexist in one sequence.
func TestSGRColonMixedWithSemicolons(t *testing.T) {
	t.Parallel()
	vt := New(80, 24)

	vt.Write([]byte("\x1b[4:3;1;31m"))
	if !vt.CurrentStyle.Underline || !vt.CurrentStyle.Bold {
		t.Errorf("4:3;1 should set underline+bold, got %+v", vt.CurrentStyle)
	}
	if vt.CurrentStyle.Italic {
		t.Error("4:3 must not set Italic")
	}
	if vt.CurrentStyle.Fg.Type != ColorIndexed || vt.CurrentStyle.Fg.Value != 1 {
		t.Errorf("31 should set red FG, got %+v", vt.CurrentStyle.Fg)
	}
}

// TestSGRColonBackgroundRGB covers the 48 colon form.
func TestSGRColonBackgroundRGB(t *testing.T) {
	t.Parallel()
	vt := New(80, 24)

	vt.Write([]byte("\x1b[48:2:10:20:30m"))
	if vt.CurrentStyle.Bg.Type != ColorRGB {
		t.Fatalf("Expected ColorRGB BG, got %v", vt.CurrentStyle.Bg.Type)
	}
	expected := uint32(10)<<16 | uint32(20)<<8 | uint32(30)
	if vt.CurrentStyle.Bg.Value != expected {
		t.Errorf("Expected BG RGB %d, got %d", expected, vt.CurrentStyle.Bg.Value)
	}
}
