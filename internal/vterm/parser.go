package vterm

import (
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
