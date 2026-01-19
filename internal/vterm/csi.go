package vterm

import (
	"strconv"
	"strings"
)

func (p *Parser) parseCSI(b byte) {
	switch {
	case b >= '0' && b <= '9':
		p.paramBuf.WriteByte(b)
		p.state = stateCSIParam
	case b == ';':
		p.pushParam()
		p.state = stateCSIParam
	case b == '?', b == '>', b == '!', b == '<':
		p.intermediate = b
		p.state = stateCSIParam
	case b >= 0x20 && b <= 0x2f: // Intermediate bytes (e.g. '$')
		p.csiIntermediate = b
		p.state = stateCSIParam
	case b >= 0x40 && b <= 0x7e: // Final byte
		p.pushParam()
		p.executeCSI(b)
		p.state = stateGround
	case b == 0x1b: // Escape interrupts
		p.state = stateEscape
	default:
		// Intermediate bytes
		if b >= 0x20 && b <= 0x2f {
			p.csiIntermediate = b
		}
	}
}

func (p *Parser) parseCSIParam(b byte) {
	switch {
	case b >= '0' && b <= '9':
		p.paramBuf.WriteByte(b)
	case b == ';':
		p.pushParam()
	case b == ':': // Sub-parameter separator
		p.paramBuf.WriteByte(b)
	case b >= 0x20 && b <= 0x2f: // Intermediate bytes (e.g. '$')
		p.csiIntermediate = b
	case b >= 0x40 && b <= 0x7e: // Final byte
		p.pushParam()
		p.executeCSI(b)
		p.state = stateGround
	case b == 0x1b: // Escape interrupts
		p.state = stateEscape
	default:
		p.state = stateGround
	}
}

func (p *Parser) pushParam() {
	if p.paramBuf.Len() > 0 {
		s := p.paramBuf.String()
		// Handle sub-parameters (colon-separated values like "38:2:255:128:0")
		if strings.Contains(s, ":") {
			parts := strings.Split(s, ":")
			for _, part := range parts {
				if part == "" {
					p.params = append(p.params, 0)
				} else {
					val, _ := strconv.Atoi(part)
					p.params = append(p.params, val)
				}
			}
		} else {
			val, _ := strconv.Atoi(s)
			p.params = append(p.params, val)
		}
	} else {
		p.params = append(p.params, 0)
	}
	p.paramBuf.Reset()
}

func (p *Parser) getParam(idx, def int) int {
	if idx < len(p.params) && p.params[idx] != 0 {
		return p.params[idx]
	}
	return def
}

func (p *Parser) executeCSI(final byte) {
	switch final {
	case 'A': // CUU - cursor up
		p.vt.moveCursor(-p.getParam(0, 1), 0)
	case 'B': // CUD - cursor down
		p.vt.moveCursor(p.getParam(0, 1), 0)
	case 'C': // CUF - cursor forward
		p.vt.moveCursor(0, p.getParam(0, 1))
	case 'D': // CUB - cursor back
		p.vt.moveCursor(0, -p.getParam(0, 1))
	case 'E': // CNL - cursor next line
		oldX, oldY := p.vt.CursorX, p.vt.CursorY
		p.vt.CursorX = 0
		p.vt.moveCursor(p.getParam(0, 1), 0)
		p.vt.bumpVersionIfCursorMoved(oldX, oldY)
	case 'F': // CPL - cursor previous line
		oldX, oldY := p.vt.CursorX, p.vt.CursorY
		p.vt.CursorX = 0
		p.vt.moveCursor(-p.getParam(0, 1), 0)
		p.vt.bumpVersionIfCursorMoved(oldX, oldY)
	case 'G': // CHA - cursor horizontal absolute
		oldX, oldY := p.vt.CursorX, p.vt.CursorY
		p.vt.CursorX = p.getParam(0, 1) - 1
		if p.vt.CursorX < 0 {
			p.vt.CursorX = 0
		}
		if p.vt.CursorX >= p.vt.Width {
			p.vt.CursorX = p.vt.Width - 1
		}
		p.vt.bumpVersionIfCursorMoved(oldX, oldY)
	case 'H', 'f': // CUP - cursor position
		p.vt.setCursorPos(p.getParam(0, 1), p.getParam(1, 1))
	case 'J': // ED - erase display
		p.vt.eraseDisplay(p.getParam(0, 0))
	case 'K': // EL - erase line
		p.vt.eraseLine(p.getParam(0, 0))
	case 'L': // IL - insert lines
		p.vt.insertLines(p.getParam(0, 1))
	case 'M': // DL - delete lines
		p.vt.deleteLines(p.getParam(0, 1))
	case 'P': // DCH - delete chars
		p.vt.deleteChars(p.getParam(0, 1))
	case 'S': // SU - scroll up
		p.vt.scrollUp(p.getParam(0, 1))
	case 'T': // SD - scroll down
		p.vt.scrollDown(p.getParam(0, 1))
	case 'X': // ECH - erase chars
		p.vt.eraseChars(p.getParam(0, 1))
	case '@': // ICH - insert chars
		p.vt.insertChars(p.getParam(0, 1))
	case 'd': // VPA - vertical position absolute
		oldX, oldY := p.vt.CursorX, p.vt.CursorY
		row := p.getParam(0, 1)
		if p.vt.OriginMode {
			p.vt.CursorY = p.vt.ScrollTop + row - 1
		} else {
			p.vt.CursorY = row - 1
		}
		p.vt.clampCursor()
		p.vt.bumpVersionIfCursorMoved(oldX, oldY)
	case 'm': // SGR - select graphic rendition
		p.executeSGR()
	case 'n': // DSR - device status report
		p.executeDSR()
	case 'r': // DECSTBM - set scrolling region
		top := p.getParam(0, 1)
		bottom := p.getParam(1, p.vt.Height)
		p.vt.setScrollRegion(top, bottom)
	case 's': // SCP - save cursor position
		if p.intermediate == 0 && p.csiIntermediate == 0 {
			p.vt.saveCursor()
		}
	case 'u': // RCP - restore cursor position
		if p.intermediate == 0 && p.csiIntermediate == 0 {
			p.vt.restoreCursor()
		}
	case 'c': // DA - device attributes
		if p.intermediate == '>' {
			// Secondary DA - report VT220
			p.vt.respond([]byte("\x1b[>1;10;0c"))
		} else if p.intermediate == 0 {
			// Primary DA - report VT220 with ANSI color
			p.vt.respond([]byte("\x1b[?62;22c"))
		}
	case 'h': // SM/DECSET - set mode
		p.executeMode(true)
	case 'l': // RM/DECRST - reset mode
		p.executeMode(false)
	case 't': // Window operations
		// Ignore
	case 'p': // DECRQM - request mode report
		if p.intermediate == '?' && p.csiIntermediate == '$' {
			p.executeDECRQM()
		}
	}
}
