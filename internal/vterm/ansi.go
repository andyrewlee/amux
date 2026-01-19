package vterm

import (
	"strconv"
	"strings"
)

// StyleToANSI converts a Style to ANSI escape codes.
// Optimized to avoid allocations using strings.Builder.
func StyleToANSI(s Style) string {
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

// StyleToDeltaANSI returns the minimal SGR escape sequence to transition from prev to next style.
// This avoids the overhead of always emitting a full reset.
// Optimized to avoid allocations using strings.Builder.
func StyleToDeltaANSI(prev, next Style) string {
	if prev == next {
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

	// Check if we need to reset (turning OFF attributes that can't be individually disabled)
	turningOff := 0
	if prev.Bold && !next.Bold {
		turningOff++
	}
	if prev.Dim && !next.Dim {
		turningOff++
	}
	if prev.Italic && !next.Italic {
		turningOff++
	}
	if prev.Underline && !next.Underline {
		turningOff++
	}
	if prev.Blink && !next.Blink {
		turningOff++
	}
	if prev.Reverse && !next.Reverse {
		turningOff++
	}
	if prev.Hidden && !next.Hidden {
		turningOff++
	}
	if prev.Strike && !next.Strike {
		turningOff++
	}

	// If turning off multiple attributes, reset is more efficient
	if turningOff > 1 {
		writeCode("0")
		// After reset, add all active attributes
		if next.Bold {
			writeCode("1")
		}
		if next.Dim {
			writeCode("2")
		}
		if next.Italic {
			writeCode("3")
		}
		if next.Underline {
			writeCode("4")
		}
		if next.Blink {
			writeCode("5")
		}
		if next.Reverse {
			writeCode("7")
		}
		if next.Hidden {
			writeCode("8")
		}
		if next.Strike {
			writeCode("9")
		}
		// Colors after reset
		writeColorToBuilderFirst(&b, next.Fg, true, &first)
		writeColorToBuilderFirst(&b, next.Bg, false, &first)
	} else {
		// Emit individual changes only

		// Turn off attributes individually
		emitted22 := false
		if (prev.Bold && !next.Bold) || (prev.Dim && !next.Dim) {
			writeCode("22") // Normal intensity
			emitted22 = true
		}
		if prev.Italic && !next.Italic {
			writeCode("23")
		}
		if prev.Underline && !next.Underline {
			writeCode("24")
		}
		if prev.Blink && !next.Blink {
			writeCode("25")
		}
		if prev.Reverse && !next.Reverse {
			writeCode("27")
		}
		if prev.Hidden && !next.Hidden {
			writeCode("28")
		}
		if prev.Strike && !next.Strike {
			writeCode("29")
		}

		// Turn on attributes
		if (!prev.Bold && next.Bold) || (emitted22 && next.Bold) {
			writeCode("1")
		}
		if (!prev.Dim && next.Dim) || (emitted22 && next.Dim) {
			writeCode("2")
		}
		if !prev.Italic && next.Italic {
			writeCode("3")
		}
		if !prev.Underline && next.Underline {
			writeCode("4")
		}
		if !prev.Blink && next.Blink {
			writeCode("5")
		}
		if !prev.Reverse && next.Reverse {
			writeCode("7")
		}
		if !prev.Hidden && next.Hidden {
			writeCode("8")
		}
		if !prev.Strike && next.Strike {
			writeCode("9")
		}

		// Colors only if changed
		if prev.Fg != next.Fg {
			if next.Fg.Type == ColorDefault {
				writeCode("39")
			} else {
				writeColorToBuilderFirst(&b, next.Fg, true, &first)
			}
		}
		if prev.Bg != next.Bg {
			if next.Bg.Type == ColorDefault {
				writeCode("49")
			} else {
				writeColorToBuilderFirst(&b, next.Bg, false, &first)
			}
		}
	}

	if first {
		return "" // No codes written
	}

	b.WriteByte('m')
	return b.String()
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
