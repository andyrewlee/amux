package vterm

import (
	"fmt"
	"strconv"
	"strings"
)

// Parser states
type parseState int

const (
	stateGround parseState = iota
	stateEscape
	stateCSI
	stateCSIParam
	stateOSC
	stateDCS
)

// Parser handles ANSI escape sequence parsing
type Parser struct {
	vt    *VTerm
	state parseState

	// CSI sequence building
	params          []int
	paramBuf        strings.Builder
	intermediate    byte
	csiIntermediate byte

	// OSC sequence building
	oscBuf strings.Builder

	// UTF-8 decoding state
	utf8Buf [4]byte
	utf8Len int // expected length
	utf8Pos int // current position
}

// NewParser creates a new parser for the given VTerm
func NewParser(vt *VTerm) *Parser {
	return &Parser{
		vt:     vt,
		state:  stateGround,
		params: make([]int, 0, 16),
	}
}

// Parse processes bytes from PTY output
func (p *Parser) Parse(data []byte) {
	for _, b := range data {
		p.parseByte(b)
	}
}

func (p *Parser) parseByte(b byte) {
	switch p.state {
	case stateGround:
		p.parseGround(b)
	case stateEscape:
		p.parseEscape(b)
	case stateCSI:
		p.parseCSI(b)
	case stateCSIParam:
		p.parseCSIParam(b)
	case stateOSC:
		p.parseOSC(b)
	case stateDCS:
		p.parseDCS(b)
	}
}

func (p *Parser) parseGround(b byte) {
	// Handle UTF-8 continuation if we're in the middle of a sequence
	if p.utf8Len > 0 {
		if b >= 0x80 && b <= 0xBF {
			// Valid continuation byte
			p.utf8Buf[p.utf8Pos] = b
			p.utf8Pos++
			if p.utf8Pos == p.utf8Len {
				// Complete UTF-8 sequence - decode it
				r := decodeUTF8(p.utf8Buf[:p.utf8Len])
				p.vt.putChar(r)
				p.utf8Len = 0
				p.utf8Pos = 0
			}
			return
		} else {
			// Invalid continuation - reset and process this byte normally
			p.utf8Len = 0
			p.utf8Pos = 0
		}
	}

	switch {
	case b == 0x1b: // ESC
		p.state = stateEscape
	case b == '\n': // LF
		p.vt.newline()
	case b == '\r': // CR
		p.vt.carriageReturn()
	case b == '\t': // Tab
		p.vt.tab()
	case b == '\b': // Backspace
		p.vt.backspace()
	case b == 0x07: // Bell
		// Ignore
	case b == 0x0e, b == 0x0f: // SI/SO (charset switching)
		// Ignore
	case b >= 0x20 && b < 0x7f: // Printable ASCII
		p.vt.putChar(rune(b))
	case b >= 0xC0 && b <= 0xDF: // 2-byte UTF-8 start
		p.utf8Buf[0] = b
		p.utf8Len = 2
		p.utf8Pos = 1
	case b >= 0xE0 && b <= 0xEF: // 3-byte UTF-8 start
		p.utf8Buf[0] = b
		p.utf8Len = 3
		p.utf8Pos = 1
	case b >= 0xF0 && b <= 0xF7: // 4-byte UTF-8 start
		p.utf8Buf[0] = b
		p.utf8Len = 4
		p.utf8Pos = 1
	}
}

// decodeUTF8 decodes a UTF-8 byte sequence into a rune
func decodeUTF8(b []byte) rune {
	switch len(b) {
	case 2:
		return rune(b[0]&0x1F)<<6 | rune(b[1]&0x3F)
	case 3:
		return rune(b[0]&0x0F)<<12 | rune(b[1]&0x3F)<<6 | rune(b[2]&0x3F)
	case 4:
		return rune(b[0]&0x07)<<18 | rune(b[1]&0x3F)<<12 | rune(b[2]&0x3F)<<6 | rune(b[3]&0x3F)
	default:
		return 0xFFFD // replacement character
	}
}

func (p *Parser) parseEscape(b byte) {
	switch b {
	case '[': // CSI
		p.state = stateCSI
		p.params = p.params[:0]
		p.paramBuf.Reset()
		p.intermediate = 0
		p.csiIntermediate = 0
	case ']': // OSC
		p.state = stateOSC
		p.oscBuf.Reset()
	case 'P': // DCS
		p.state = stateDCS
	case '(', ')': // Charset designation
		p.state = stateGround // Ignore next char
	case '7': // DECSC - save cursor
		p.vt.saveCursor()
		p.state = stateGround
	case '8': // DECRC - restore cursor
		p.vt.restoreCursor()
		p.state = stateGround
	case 'M': // RI - reverse index (scroll down)
		if p.vt.CursorY == p.vt.ScrollTop {
			p.vt.scrollDown(1)
		} else if p.vt.CursorY > 0 {
			p.vt.CursorY--
		}
		p.state = stateGround
	case 'D': // IND - index (scroll up)
		p.vt.newline()
		p.state = stateGround
	case 'E': // NEL - next line
		p.vt.carriageReturn()
		p.vt.newline()
		p.state = stateGround
	case 'c': // RIS - reset
		p.vt.CurrentStyle = Style{}
		p.vt.CursorX = 0
		p.vt.CursorY = 0
		p.state = stateGround
	case '=', '>': // DECKPAM/DECKPNM (keypad modes)
		p.state = stateGround
	default:
		p.state = stateGround
	}
}

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
		p.vt.CursorX = 0
		p.vt.moveCursor(p.getParam(0, 1), 0)
	case 'F': // CPL - cursor previous line
		p.vt.CursorX = 0
		p.vt.moveCursor(-p.getParam(0, 1), 0)
	case 'G': // CHA - cursor horizontal absolute
		p.vt.CursorX = p.getParam(0, 1) - 1
		if p.vt.CursorX < 0 {
			p.vt.CursorX = 0
		}
		if p.vt.CursorX >= p.vt.Width {
			p.vt.CursorX = p.vt.Width - 1
		}
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
		p.vt.CursorY = p.getParam(0, 1) - 1
		if p.vt.CursorY < 0 {
			p.vt.CursorY = 0
		}
		if p.vt.CursorY >= p.vt.Height {
			p.vt.CursorY = p.vt.Height - 1
		}
	case 'm': // SGR - select graphic rendition
		p.executeSGR()
	case 'n': // DSR - device status report
		p.executeDSR()
	case 'r': // DECSTBM - set scrolling region
		top := p.getParam(0, 1)
		bottom := p.getParam(1, p.vt.Height)
		p.vt.setScrollRegion(top, bottom)
	case 's': // SCP - save cursor position
		p.vt.saveCursor()
	case 'u': // RCP - restore cursor position
		p.vt.restoreCursor()
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

func (p *Parser) executeSGR() {
	if len(p.params) == 0 {
		p.params = []int{0}
	}

	for i := 0; i < len(p.params); i++ {
		param := p.params[i]
		switch param {
		case 0: // Reset
			p.vt.CurrentStyle = Style{}
		case 1:
			p.vt.CurrentStyle.Bold = true
		case 2:
			p.vt.CurrentStyle.Dim = true
		case 3:
			p.vt.CurrentStyle.Italic = true
		case 4:
			p.vt.CurrentStyle.Underline = true
		case 5, 6:
			p.vt.CurrentStyle.Blink = true
		case 7:
			p.vt.CurrentStyle.Reverse = true
		case 8:
			p.vt.CurrentStyle.Hidden = true
		case 9:
			p.vt.CurrentStyle.Strike = true
		case 21:
			p.vt.CurrentStyle.Bold = false
		case 22:
			p.vt.CurrentStyle.Bold = false
			p.vt.CurrentStyle.Dim = false
		case 23:
			p.vt.CurrentStyle.Italic = false
		case 24:
			p.vt.CurrentStyle.Underline = false
		case 25:
			p.vt.CurrentStyle.Blink = false
		case 27:
			p.vt.CurrentStyle.Reverse = false
		case 28:
			p.vt.CurrentStyle.Hidden = false
		case 29:
			p.vt.CurrentStyle.Strike = false
		case 30, 31, 32, 33, 34, 35, 36, 37: // FG colors 0-7
			p.vt.CurrentStyle.Fg = Color{Type: ColorIndexed, Value: uint32(param - 30)}
		case 38: // Extended FG
			i = p.parseExtendedColor(i, &p.vt.CurrentStyle.Fg)
		case 39: // Default FG
			p.vt.CurrentStyle.Fg = Color{Type: ColorDefault}
		case 40, 41, 42, 43, 44, 45, 46, 47: // BG colors 0-7
			p.vt.CurrentStyle.Bg = Color{Type: ColorIndexed, Value: uint32(param - 40)}
		case 48: // Extended BG
			i = p.parseExtendedColor(i, &p.vt.CurrentStyle.Bg)
		case 49: // Default BG
			p.vt.CurrentStyle.Bg = Color{Type: ColorDefault}
		case 90, 91, 92, 93, 94, 95, 96, 97: // Bright FG
			p.vt.CurrentStyle.Fg = Color{Type: ColorIndexed, Value: uint32(param - 90 + 8)}
		case 100, 101, 102, 103, 104, 105, 106, 107: // Bright BG
			p.vt.CurrentStyle.Bg = Color{Type: ColorIndexed, Value: uint32(param - 100 + 8)}
		}
	}
}

func (p *Parser) parseExtendedColor(i int, color *Color) int {
	if i+1 >= len(p.params) {
		return i
	}

	switch p.params[i+1] {
	case 2: // RGB
		if i+4 < len(p.params) {
			r := p.params[i+2]
			g := p.params[i+3]
			b := p.params[i+4]
			color.Type = ColorRGB
			color.Value = uint32(r)<<16 | uint32(g)<<8 | uint32(b)
			return i + 4
		}
	case 5: // 256 color
		if i+2 < len(p.params) {
			color.Type = ColorIndexed
			color.Value = uint32(p.params[i+2])
			return i + 2
		}
	}
	return i + 1
}

func (p *Parser) executeDSR() {
	if len(p.params) == 0 {
		return
	}

	switch p.params[0] {
	case 5: // Status report - respond "OK"
		p.vt.respond([]byte("\x1b[0n"))
	case 6: // Cursor position report
		// Response: ESC [ row ; col R (1-indexed)
		row := p.vt.CursorY + 1
		col := p.vt.CursorX + 1
		response := fmt.Sprintf("\x1b[%d;%dR", row, col)
		p.vt.respond([]byte(response))
	}
}

func (p *Parser) executeMode(set bool) {
	if p.intermediate != '?' {
		return
	}

	for _, param := range p.params {
		switch param {
		case 1: // DECCKM - cursor keys mode
			// Ignore
		case 7: // DECAWM - auto-wrap mode
			// Always on
		case 12: // Blinking cursor
			// Ignore
		case 25: // DECTCEM - cursor visible
			// Ignore
		case 1049: // Alternate screen buffer
			if set {
				p.vt.enterAltScreen()
			} else {
				p.vt.exitAltScreen()
			}
		case 2026: // Synchronized output
			p.vt.setSynchronizedOutput(set)
		case 2004: // Bracketed paste mode
			// Ignore
		}
	}
}

func (p *Parser) executeDECRQM() {
	if len(p.params) == 0 {
		return
	}

	for _, param := range p.params {
		status := 0
		switch param {
		case 2026:
			if p.vt.syncActive {
				status = 1
			} else {
				status = 2
			}
		default:
			status = 0
		}
		response := fmt.Sprintf("\x1b[?%d;%d$y", param, status)
		p.vt.respond([]byte(response))
	}
}

func (p *Parser) parseOSC(b byte) {
	if b == 0x07 || b == 0x1b { // BEL or ESC terminates
		if b == 0x1b {
			p.state = stateEscape // Will see \ next
		} else {
			p.executeOSC()
			p.state = stateGround
		}
		return
	}
	p.oscBuf.WriteByte(b)
}

func (p *Parser) executeOSC() {
	// OSC sequences - ignore for now
	// Could handle:
	// - OSC 0;title - set window title
	// - OSC 10;color - query/set foreground
	// - OSC 11;color - query/set background
}

func (p *Parser) parseDCS(b byte) {
	// DCS sequences - ignore
	if b == 0x1b {
		p.state = stateEscape
		return
	}
	// Stay in DCS until we see ESC \
}
