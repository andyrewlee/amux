package vterm

import (
	"strconv"
	"strings"
)

// Render returns the terminal content as a string with ANSI codes
func (v *VTerm) Render() string {
	screen, scrollbackLen := v.RenderBuffers()
	if v.ViewOffset > 0 {
		return v.renderWithScrollbackFrom(screen, scrollbackLen)
	}
	if v.syncActive {
		return v.renderScreenFrom(screen)
	}
	return v.renderScreenCached(screen)
}

// RenderBuffers returns the current screen buffer and scrollback length.
// During synchronized output, it returns the frozen snapshot.
func (v *VTerm) RenderBuffers() ([][]Cell, int) {
	if v.syncActive && v.syncScreen != nil {
		scrollbackLen := v.syncScrollbackLen
		if scrollbackLen > len(v.Scrollback) {
			scrollbackLen = len(v.Scrollback)
		}
		return v.syncScreen, scrollbackLen
	}
	return v.Screen, len(v.Scrollback)
}

// VisibleScreen returns the currently visible screen buffer as a copy.
func (v *VTerm) VisibleScreen() [][]Cell {
	screen, scrollbackLen := v.RenderBuffers()
	width := v.Width
	height := v.Height

	lines := make([][]Cell, height)

	// If scrolled, pull from scrollback + screen.
	if v.ViewOffset > 0 {
		if scrollbackLen > len(v.Scrollback) {
			scrollbackLen = len(v.Scrollback)
		}
		screenLen := len(screen)
		startLine := scrollbackLen + screenLen - height - v.ViewOffset
		if startLine < 0 {
			startLine = 0
		}

		for i := 0; i < height; i++ {
			lineIdx := startLine + i
			var row []Cell
			if lineIdx < scrollbackLen {
				row = v.Scrollback[lineIdx]
			} else if lineIdx-scrollbackLen < screenLen {
				row = screen[lineIdx-scrollbackLen]
			}
			line := MakeBlankLine(width)
			if row != nil {
				copy(line, row)
			}
			lines[i] = line
		}
		return lines
	}

	// Live screen.
	for y := 0; y < height; y++ {
		line := MakeBlankLine(width)
		if y < len(screen) {
			copy(line, screen[y])
		}
		lines[y] = line
	}
	return lines
}

// VisibleScreenWithSelection returns the visible screen with selection highlighting applied.
func (v *VTerm) VisibleScreenWithSelection() [][]Cell {
	lines := v.VisibleScreen()
	if !v.selActive {
		return lines
	}

	for y := 0; y < len(lines); y++ {
		row := lines[y]
		for x := 0; x < len(row); x++ {
			if v.IsInSelection(x, y) {
				cell := row[x]
				cell.Style.Reverse = !cell.Style.Reverse
				row[x] = cell
			}
		}
		lines[y] = row
	}

	return lines
}

// IsInSelection checks if coordinate (x, y) is within the selection
func (v *VTerm) IsInSelection(x, y int) bool {
	if !v.selActive {
		return false
	}

	// Normalize selection so start is before end
	startX, startY := v.selStartX, v.selStartY
	endX, endY := v.selEndX, v.selEndY
	if startY > endY || (startY == endY && startX > endX) {
		startX, endX = endX, startX
		startY, endY = endY, startY
	}

	// Check if (x, y) is in selection range
	if y < startY || y > endY {
		return false
	}
	if y == startY && y == endY {
		// Single line selection
		return x >= startX && x <= endX
	}
	if y == startY {
		return x >= startX
	}
	if y == endY {
		return x <= endX
	}
	// Middle lines are fully selected
	return true
}

// renderScreenFrom renders the given screen buffer
func (v *VTerm) renderScreenFrom(screen [][]Cell) string {
	var buf strings.Builder
	buf.Grow(v.Width * v.Height * 2) // Rough estimate

	for y, row := range screen {
		buf.WriteString(v.renderRow(row, y))

		if y < len(v.Screen)-1 {
			buf.WriteString("\n")
		}
	}

	// Reset styles at end
	buf.WriteString("\x1b[0m")

	return buf.String()
}

func (v *VTerm) renderScreenCached(screen [][]Cell) string {
	v.ensureRenderCache(len(screen))

	// Invalidate cursor lines if cursor state changed
	if v.ShowCursor != v.lastShowCursor || v.CursorHidden != v.lastCursorHidden || v.CursorX != v.lastCursorX || v.CursorY != v.lastCursorY {
		// Mark old cursor line dirty
		if v.lastCursorY >= 0 && v.lastCursorY < len(v.renderDirty) {
			v.renderDirty[v.lastCursorY] = true
		}
		// Mark new cursor line dirty
		if v.CursorY >= 0 && v.CursorY < len(v.renderDirty) {
			v.renderDirty[v.CursorY] = true
		}
		v.lastShowCursor = v.ShowCursor
		v.lastCursorHidden = v.CursorHidden
		v.lastCursorX = v.CursorX
		v.lastCursorY = v.CursorY
	}

	lines := make([]string, len(screen))
	for y, row := range screen {
		if v.renderDirtyAll || v.renderDirty[y] || v.renderCache[y] == "" {
			lines[y] = v.renderRow(row, y)
			v.renderCache[y] = lines[y]
			v.renderDirty[y] = false
		} else {
			lines[y] = v.renderCache[y]
		}
	}
	v.renderDirtyAll = false

	out := strings.Join(lines, "\n")
	return out + "\x1b[0m"
}

func (v *VTerm) renderRow(row []Cell, y int) string {
	var buf strings.Builder
	buf.Grow(v.Width * 2)

	// Reset per-line to make cached lines independent.
	buf.WriteString("\x1b[0m")
	var lastStyle Style
	var lastReverse bool

	// Determine if cursor is on this row and should be shown
	// Don't show cursor if terminal app hid it via DECTCEM
	cursorOnRow := v.ShowCursor && !v.CursorHidden && y == v.CursorY && v.ViewOffset == 0

	for x, cell := range row {
		// Check if this cell is in selection
		inSel := v.IsInSelection(x, y)

		// Check if cursor is at this position
		isCursor := cursorOnRow && x == v.CursorX

		// Apply style changes (toggle reverse for selection or cursor)
		style := cell.Style
		if inSel || isCursor {
			style.Reverse = !style.Reverse
		}
		style = suppressBlankUnderline(cell, style)

		if style != lastStyle || inSel != lastReverse || isCursor {
			// Use delta encoding after the first style (which has reset)
			if x == 0 {
				buf.WriteString(StyleToANSI(style))
			} else {
				buf.WriteString(StyleToDeltaANSI(lastStyle, style))
			}
			lastStyle = style
			lastReverse = inSel
		}

		// Skip continuation cells (part of wide character)
		if cell.Width == 0 {
			continue
		}

		if cell.Rune == 0 {
			buf.WriteRune(' ')
		} else {
			buf.WriteRune(cell.Rune)
		}
	}

	return buf.String()
}

// renderWithScrollbackFrom renders content from scrollback + screen
func (v *VTerm) renderWithScrollbackFrom(screen [][]Cell, scrollbackLen int) string {
	var buf strings.Builder
	buf.Grow(v.Width * v.Height * 2)

	// Calculate which lines to show
	// ViewOffset = how many lines scrolled up into history
	if scrollbackLen > len(v.Scrollback) {
		scrollbackLen = len(v.Scrollback)
	}
	screenLen := len(screen)

	// Start position in the combined buffer (scrollback + screen)
	// When ViewOffset = scrollbackLen, we show from the start of scrollback
	// When ViewOffset = 0, we show the screen
	startLine := scrollbackLen + screenLen - v.Height - v.ViewOffset
	if startLine < 0 {
		startLine = 0
	}

	var lastStyle Style
	var lastReverse bool
	firstCell := true

	for i := 0; i < v.Height; i++ {
		lineIdx := startLine + i

		var row []Cell
		if lineIdx < scrollbackLen {
			row = v.Scrollback[lineIdx]
		} else if lineIdx-scrollbackLen < screenLen {
			row = screen[lineIdx-scrollbackLen]
		}

		// Render the row
		for x := 0; x < v.Width; x++ {
			var cell Cell
			if row != nil && x < len(row) {
				cell = row[x]
			} else {
				cell = DefaultCell()
			}

			// Check if this cell is in selection (i is the visible Y coord)
			inSel := v.IsInSelection(x, i)

			// Apply style changes (toggle reverse for selection)
			style := cell.Style
			if inSel {
				style.Reverse = !style.Reverse
			}
			style = suppressBlankUnderline(cell, style)

			if firstCell || style != lastStyle || inSel != lastReverse {
				buf.WriteString(StyleToANSI(style))
				lastStyle = style
				lastReverse = inSel
				firstCell = false
			}

			// Skip continuation cells (part of wide character)
			if cell.Width == 0 {
				continue
			}

			if cell.Rune == 0 {
				buf.WriteRune(' ')
			} else {
				buf.WriteRune(cell.Rune)
			}
		}

		if i < v.Height-1 {
			buf.WriteString("\n")
		}
	}

	buf.WriteString("\x1b[0m")
	return buf.String()
}

func suppressBlankUnderline(cell Cell, style Style) Style {
	// Some TUIs leave underline enabled while clearing rows; underline on spaces
	// renders as scanlines, so drop it for blank cells at render time.
	if !style.Underline {
		return style
	}
	if cell.Rune == 0 || cell.Rune == ' ' {
		style.Underline = false
	}
	return style
}

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
		if (prev.Bold && !next.Bold) || (prev.Dim && !next.Dim) {
			writeCode("22") // Normal intensity
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
		if !prev.Bold && next.Bold {
			writeCode("1")
		}
		if !prev.Dim && next.Dim {
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

// GetAllLines returns all content (scrollback + screen) as lines for search
func (v *VTerm) GetAllLines() []string {
	lines := make([]string, 0, len(v.Scrollback)+len(v.Screen))

	for _, row := range v.Scrollback {
		lines = append(lines, rowToString(row))
	}
	for _, row := range v.Screen {
		lines = append(lines, rowToString(row))
	}

	return lines
}

// rowToString converts a row of cells to a plain string (no ANSI)
func rowToString(row []Cell) string {
	var buf strings.Builder
	for _, cell := range row {
		if cell.Width == 0 {
			continue
		}
		if cell.Rune == 0 {
			buf.WriteRune(' ')
		} else {
			buf.WriteRune(cell.Rune)
		}
	}
	// Trim trailing spaces
	return strings.TrimRight(buf.String(), " ")
}

// Search finds all line indices matching query
func (v *VTerm) Search(query string) []int {
	if query == "" {
		return nil
	}

	query = strings.ToLower(query)
	lines := v.GetAllLines()
	var matches []int

	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), query) {
			matches = append(matches, i)
		}
	}

	return matches
}

// ScrollToLine scrolls view to show the given line index (in combined buffer)
func (v *VTerm) ScrollToLine(lineIdx int) {
	totalLines := len(v.Scrollback) + len(v.Screen)

	// Calculate ViewOffset to center this line
	targetOffset := totalLines - lineIdx - v.Height/2
	if targetOffset < 0 {
		targetOffset = 0
	}
	if targetOffset > len(v.Scrollback) {
		targetOffset = len(v.Scrollback)
	}

	v.ViewOffset = targetOffset
}

// GetSelectedText extracts text from the selection range.
// startX, startY, endX, endY are in visible screen coordinates (0-indexed).
// This handles scrollback by converting visible Y to absolute line numbers.
func (v *VTerm) GetSelectedText(startX, startY, endX, endY int) string {
	// Normalize coordinates so start is before end
	if startY > endY || (startY == endY && startX > endX) {
		startX, endX = endX, startX
		startY, endY = endY, startY
	}

	// Clamp to valid range
	if startX < 0 {
		startX = 0
	}
	if endX >= v.Width {
		endX = v.Width - 1
	}
	if startY < 0 {
		startY = 0
	}
	if endY >= v.Height {
		endY = v.Height - 1
	}

	// Convert visible Y coordinates to absolute line numbers
	// (matching the logic in renderWithScrollback)
	screen, scrollbackLen := v.RenderBuffers()
	screenLen := len(screen)
	startLine := scrollbackLen + screenLen - v.Height - v.ViewOffset
	if startLine < 0 {
		startLine = 0
	}

	var result strings.Builder

	for y := startY; y <= endY; y++ {
		absLineNum := startLine + y

		// Get the row from scrollback or screen
		var row []Cell
		if absLineNum < scrollbackLen {
			row = v.Scrollback[absLineNum]
		} else if absLineNum-scrollbackLen < screenLen {
			row = screen[absLineNum-scrollbackLen]
		}

		if row == nil {
			if y < endY {
				result.WriteRune('\n')
			}
			continue
		}

		// Determine X range for this line
		xStart := 0
		xEnd := len(row) - 1
		if y == startY {
			xStart = startX
		}
		if y == endY {
			xEnd = endX
		}
		if xEnd >= len(row) {
			xEnd = len(row) - 1
		}

		// Extract characters from the row
		for x := xStart; x <= xEnd; x++ {
			if x < len(row) {
				if row[x].Width == 0 {
					continue
				}
				r := row[x].Rune
				if r == 0 {
					r = ' '
				}
				result.WriteRune(r)
			}
		}

		// Add newline between lines (but not after the last line)
		if y < endY {
			result.WriteRune('\n')
		}
	}

	// Trim trailing spaces from each line
	lines := strings.Split(result.String(), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

// SetSelection stores selection coordinates for rendering with highlight.
// Pass nil coordinates to clear selection.
func (v *VTerm) SetSelection(startX, startY, endX, endY int, active bool) {
	v.selStartX = startX
	v.selStartY = startY
	v.selEndX = endX
	v.selEndY = endY
	v.selActive = active
	v.renderDirtyAll = true
}

// ClearSelection clears the current selection
func (v *VTerm) ClearSelection() {
	v.selActive = false
	v.renderDirtyAll = true
}
