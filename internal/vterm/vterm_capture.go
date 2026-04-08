package vterm

import "bytes"

func captureLines(data []byte, tmp *VTerm) [][]Cell {
	if tmp == nil {
		return nil
	}
	// Collect lines: scrollback first, then only the explicit row count tmux
	// emitted for this capture. That preserves real trailing blank rows while
	// still dropping the parser's implicit unused screen rows.
	lines := make([][]Cell, 0, len(tmp.Scrollback)+len(tmp.Screen))
	for _, line := range tmp.Scrollback {
		lines = append(lines, CopyLine(line))
	}
	screenRows := captureRowCount(data) - len(tmp.Scrollback)
	if screenRows <= 0 {
		return lines
	}
	if screenRows > len(tmp.Screen) {
		screenRows = len(tmp.Screen)
	}
	for i := 0; i < screenRows; i++ {
		lines = append(lines, CopyLine(tmp.Screen[i]))
	}
	return lines
}

func captureRowCount(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	trimmed := trimCaptureTrailingNewline(data)
	if len(trimmed) == 0 {
		return 1
	}
	return bytes.Count(trimmed, []byte{'\n'}) + 1
}

func isBlankScreen(screen [][]Cell) bool {
	if len(screen) == 0 {
		return true
	}
	for _, line := range screen {
		if !isBlankLine(line) {
			return false
		}
	}
	return true
}

func parseCaptureWithSize(data []byte, width, height int) *VTerm {
	if len(data) == 0 {
		return nil
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	tmp := New(width, height)
	// tmux capture-pane serializes rows with bare LF separators regardless of
	// the live terminal's newline mode, so history parsing must always treat LF
	// as a row break that returns to column 0.
	tmp.TreatLFAsCRLF = true
	tmp.Write(trimCaptureTrailingNewline(data))
	return tmp
}

func trimCaptureTrailingNewline(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	if bytes.HasSuffix(data, []byte("\r\n")) {
		return data[:len(data)-2]
	}
	if data[len(data)-1] == '\n' || data[len(data)-1] == '\r' {
		return data[:len(data)-1]
	}
	return data
}
