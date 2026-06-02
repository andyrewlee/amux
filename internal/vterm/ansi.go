package vterm

import (
	"strconv"
	"strings"
)

// ANSI converts a Style to a full ANSI escape sequence, leading with a reset.
// Optimized to avoid allocations using strings.Builder.
func (s Style) ANSI() string {
	var b strings.Builder
	b.Grow(32) // Pre-allocate for typical SGR sequence

	b.WriteString("\x1b[0") // Reset first

	if s.Bold {
		b.WriteString(";1")
	}
	if s.Dim {
		b.WriteString(";2")
	}
	if s.Italic {
		b.WriteString(";3")
	}
	if s.Underline {
		b.WriteString(";4")
	}
	if s.Blink {
		b.WriteString(";5")
	}
	if s.Reverse {
		b.WriteString(";7")
	}
	if s.Hidden {
		b.WriteString(";8")
	}
	if s.Strike {
		b.WriteString(";9")
	}

	// Foreground color
	writeColorToBuilder(&b, s.Fg, true)

	// Background color
	writeColorToBuilder(&b, s.Bg, false)

	b.WriteByte('m')
	return b.String()
}

// DeltaANSI returns the minimal SGR escape sequence to transition from the
// receiver (the previous style) to next. It avoids the overhead of always
// emitting a full reset, and returns "" when nothing changed.
// Optimized to avoid allocations using strings.Builder.
func (s Style) DeltaANSI(next Style) string {
	if s == next {
		return ""
	}

	var b strings.Builder
	b.Grow(32) // Pre-allocate for typical SGR sequence
	first := true

	writeCode := func(code string) {
		if first {
			b.WriteString("\x1b[")
			first = false
		} else {
			b.WriteByte(';')
		}
		b.WriteString(code)
	}

	// When turning off more than one attribute, a full reset followed by the
	// surviving attributes is cheaper than disabling each individually.
	if s.attrsTurningOff(next) > 1 {
		next.appendResetThenActive(writeCode, &b, &first)
	} else {
		emitted22 := s.appendAttrDisables(next, writeCode)
		s.appendAttrEnables(next, emitted22, writeCode)
		s.appendColorChanges(next, writeCode, &b, &first)
	}

	if first {
		return "" // No codes written
	}

	b.WriteByte('m')
	return b.String()
}

// attrsTurningOff counts boolean attributes set in the receiver (previous style)
// but cleared in next. These cannot all be disabled with a single SGR code, so
// the count decides whether a full reset is the more compact transition.
func (s Style) attrsTurningOff(next Style) int {
	turningOff := 0
	if s.Bold && !next.Bold {
		turningOff++
	}
	if s.Dim && !next.Dim {
		turningOff++
	}
	if s.Italic && !next.Italic {
		turningOff++
	}
	if s.Underline && !next.Underline {
		turningOff++
	}
	if s.Blink && !next.Blink {
		turningOff++
	}
	if s.Reverse && !next.Reverse {
		turningOff++
	}
	if s.Hidden && !next.Hidden {
		turningOff++
	}
	if s.Strike && !next.Strike {
		turningOff++
	}
	return turningOff
}

// appendResetThenActive emits a reset (0) followed by every attribute and color
// active in the receiver style. Used when disabling attributes individually
// would be longer than starting from a clean reset.
func (s Style) appendResetThenActive(writeCode func(string), b *strings.Builder, first *bool) {
	writeCode("0")
	if s.Bold {
		writeCode("1")
	}
	if s.Dim {
		writeCode("2")
	}
	if s.Italic {
		writeCode("3")
	}
	if s.Underline {
		writeCode("4")
	}
	if s.Blink {
		writeCode("5")
	}
	if s.Reverse {
		writeCode("7")
	}
	if s.Hidden {
		writeCode("8")
	}
	if s.Strike {
		writeCode("9")
	}
	writeColorToBuilderFirst(b, s.Fg, true, first)
	writeColorToBuilderFirst(b, s.Bg, false, first)
}

// appendAttrDisables emits the individual SGR codes (22-29) that turn off
// attributes set in the receiver (previous style) but cleared in next. It
// reports whether code 22 (normal intensity) was emitted, since that resets
// both bold and dim and so the caller must re-enable either if next wants it.
func (s Style) appendAttrDisables(next Style, writeCode func(string)) (emitted22 bool) {
	if (s.Bold && !next.Bold) || (s.Dim && !next.Dim) {
		writeCode("22") // Normal intensity
		emitted22 = true
	}
	if s.Italic && !next.Italic {
		writeCode("23")
	}
	if s.Underline && !next.Underline {
		writeCode("24")
	}
	if s.Blink && !next.Blink {
		writeCode("25")
	}
	if s.Reverse && !next.Reverse {
		writeCode("27")
	}
	if s.Hidden && !next.Hidden {
		writeCode("28")
	}
	if s.Strike && !next.Strike {
		writeCode("29")
	}
	return emitted22
}

// appendAttrEnables emits the individual SGR codes that turn on attributes set
// in next but not in the receiver (previous style). When emitted22 is true,
// bold/dim were just reset by code 22 and must be re-emitted if next wants them.
func (s Style) appendAttrEnables(next Style, emitted22 bool, writeCode func(string)) {
	if (!s.Bold && next.Bold) || (emitted22 && next.Bold) {
		writeCode("1")
	}
	if (!s.Dim && next.Dim) || (emitted22 && next.Dim) {
		writeCode("2")
	}
	if !s.Italic && next.Italic {
		writeCode("3")
	}
	if !s.Underline && next.Underline {
		writeCode("4")
	}
	if !s.Blink && next.Blink {
		writeCode("5")
	}
	if !s.Reverse && next.Reverse {
		writeCode("7")
	}
	if !s.Hidden && next.Hidden {
		writeCode("8")
	}
	if !s.Strike && next.Strike {
		writeCode("9")
	}
}

// appendColorChanges emits foreground/background SGR codes only when the color
// differs between the receiver (previous style) and next, using 39/49 to return
// to the default color.
func (s Style) appendColorChanges(next Style, writeCode func(string), b *strings.Builder, first *bool) {
	if s.Fg != next.Fg {
		if next.Fg.Type == ColorDefault {
			writeCode("39")
		} else {
			writeColorToBuilderFirst(b, next.Fg, true, first)
		}
	}
	if s.Bg != next.Bg {
		if next.Bg.Type == ColorDefault {
			writeCode("49")
		} else {
			writeColorToBuilderFirst(b, next.Bg, false, first)
		}
	}
}

// writeColorToBuilder appends color codes to a strings.Builder.
// Assumes the builder already has "\x1b[" prefix and uses ";" separator.
func writeColorToBuilder(b *strings.Builder, c Color, fg bool) {
	switch c.Type {
	case ColorDefault:
		return
	case ColorIndexed:
		idx := c.Value
		b.WriteByte(';')
		if idx < 8 {
			if fg {
				b.WriteString(strconv.FormatUint(uint64(30+idx), 10))
			} else {
				b.WriteString(strconv.FormatUint(uint64(40+idx), 10))
			}
		} else if idx < 16 {
			if fg {
				b.WriteString(strconv.FormatUint(uint64(90+idx-8), 10))
			} else {
				b.WriteString(strconv.FormatUint(uint64(100+idx-8), 10))
			}
		} else {
			if fg {
				b.WriteString("38;5;")
			} else {
				b.WriteString("48;5;")
			}
			b.WriteString(strconv.FormatUint(uint64(idx), 10))
		}
	case ColorRGB:
		r := (c.Value >> 16) & 0xFF
		g := (c.Value >> 8) & 0xFF
		bv := c.Value & 0xFF
		if fg {
			b.WriteString(";38;2;")
		} else {
			b.WriteString(";48;2;")
		}
		b.WriteString(strconv.FormatUint(uint64(r), 10))
		b.WriteByte(';')
		b.WriteString(strconv.FormatUint(uint64(g), 10))
		b.WriteByte(';')
		b.WriteString(strconv.FormatUint(uint64(bv), 10))
	}
}

// writeColorToBuilderFirst appends color codes, handling first code specially.
func writeColorToBuilderFirst(b *strings.Builder, c Color, fg bool, first *bool) {
	switch c.Type {
	case ColorDefault:
		return
	case ColorIndexed:
		idx := c.Value
		if *first {
			b.WriteString("\x1b[")
			*first = false
		} else {
			b.WriteByte(';')
		}
		if idx < 8 {
			if fg {
				b.WriteString(strconv.FormatUint(uint64(30+idx), 10))
			} else {
				b.WriteString(strconv.FormatUint(uint64(40+idx), 10))
			}
		} else if idx < 16 {
			if fg {
				b.WriteString(strconv.FormatUint(uint64(90+idx-8), 10))
			} else {
				b.WriteString(strconv.FormatUint(uint64(100+idx-8), 10))
			}
		} else {
			if fg {
				b.WriteString("38;5;")
			} else {
				b.WriteString("48;5;")
			}
			b.WriteString(strconv.FormatUint(uint64(idx), 10))
		}
	case ColorRGB:
		r := (c.Value >> 16) & 0xFF
		g := (c.Value >> 8) & 0xFF
		bv := c.Value & 0xFF
		if *first {
			b.WriteString("\x1b[")
			*first = false
		} else {
			b.WriteByte(';')
		}
		if fg {
			b.WriteString("38;2;")
		} else {
			b.WriteString("48;2;")
		}
		b.WriteString(strconv.FormatUint(uint64(r), 10))
		b.WriteByte(';')
		b.WriteString(strconv.FormatUint(uint64(g), 10))
		b.WriteByte(';')
		b.WriteString(strconv.FormatUint(uint64(bv), 10))
	}
}
