package common

import "github.com/andyrewlee/amux/internal/vterm"

type ptyOverflowTrimState uint8

const (
	ptyOverflowTrimText ptyOverflowTrimState = iota
	ptyOverflowTrimEsc
	ptyOverflowTrimCSI
	ptyOverflowTrimCSIParam
	ptyOverflowTrimOSC
	ptyOverflowTrimDCS
	ptyOverflowTrimCharset
)

// TrimPTYOverflowPrefix drops at least drop bytes from a buffered PTY stream and,
// when the cut lands inside an ANSI/control sequence or UTF-8 rune, advances to
// the next parser-safe boundary. This avoids rendering the tail of a truncated
// control sequence as visible text after overflow backpressure.
func TrimPTYOverflowPrefix(data []byte, drop int, seed vterm.ParserCarryState) ([]byte, vterm.ParserCarryState) {
	if len(data) == 0 {
		return data, seed
	}
	if drop < 0 {
		drop = 0
	}
	if drop >= len(data) {
		drop = len(data)
	}

	state := parserCarryToOverflowTrimState(seed.Mode)
	utf8Remaining := seed.UTF8Remaining
	for i := 0; i < drop; i++ {
		state, utf8Remaining = advancePTYOverflowTrimState(state, utf8Remaining, data[i])
	}
	if drop < len(data) && state == ptyOverflowTrimText && utf8Remaining == 0 {
		return data[drop:], vterm.ParserCarryState{}
	}

	start := drop
	for start < len(data) {
		if isSafePTYOverflowBoundary(state, utf8Remaining, data[start]) {
			return data[start:], vterm.ParserCarryState{}
		}
		state, utf8Remaining = advancePTYOverflowTrimState(state, utf8Remaining, data[start])
		start++
		if state == ptyOverflowTrimText && utf8Remaining == 0 {
			return data[start:], vterm.ParserCarryState{}
		}
	}

	return nil, overflowTrimStateToParserCarry(state, utf8Remaining)
}

func isSafePTYOverflowBoundary(state ptyOverflowTrimState, utf8Remaining int, b byte) bool {
	if utf8Remaining > 0 {
		return b < 0x80 || b > 0xBF
	}
	if b != 0x1b {
		return false
	}
	switch state {
	case ptyOverflowTrimCSI, ptyOverflowTrimCSIParam, ptyOverflowTrimOSC, ptyOverflowTrimDCS:
		return true
	default:
		return false
	}
}

func parserCarryToOverflowTrimState(mode vterm.ParserCarryMode) ptyOverflowTrimState {
	switch mode {
	case vterm.ParserCarryEscape:
		return ptyOverflowTrimEsc
	case vterm.ParserCarryCSI:
		return ptyOverflowTrimCSI
	case vterm.ParserCarryCSIParam:
		return ptyOverflowTrimCSIParam
	case vterm.ParserCarryOSC:
		return ptyOverflowTrimOSC
	case vterm.ParserCarryDCS:
		return ptyOverflowTrimDCS
	case vterm.ParserCarryCharset:
		return ptyOverflowTrimCharset
	default:
		return ptyOverflowTrimText
	}
}

func overflowTrimStateToParserCarry(state ptyOverflowTrimState, utf8Remaining int) vterm.ParserCarryState {
	mode := vterm.ParserCarryText
	switch state {
	case ptyOverflowTrimEsc:
		mode = vterm.ParserCarryEscape
	case ptyOverflowTrimCSI:
		mode = vterm.ParserCarryCSI
	case ptyOverflowTrimCSIParam:
		mode = vterm.ParserCarryCSIParam
	case ptyOverflowTrimOSC:
		mode = vterm.ParserCarryOSC
	case ptyOverflowTrimDCS:
		mode = vterm.ParserCarryDCS
	case ptyOverflowTrimCharset:
		mode = vterm.ParserCarryCharset
	}
	return vterm.ParserCarryState{
		Mode:          mode,
		UTF8Remaining: utf8Remaining,
	}
}

func advancePTYOverflowTrimState(state ptyOverflowTrimState, utf8Remaining int, b byte) (ptyOverflowTrimState, int) {
	if utf8Remaining > 0 {
		if b >= 0x80 && b <= 0xBF {
			utf8Remaining--
			return state, utf8Remaining
		}
		utf8Remaining = 0
	}

	switch state {
	case ptyOverflowTrimText:
		switch {
		case b == 0x1b:
			state = ptyOverflowTrimEsc
		case b >= 0xC0 && b <= 0xDF:
			utf8Remaining = 1
		case b >= 0xE0 && b <= 0xEF:
			utf8Remaining = 2
		case b >= 0xF0 && b <= 0xF7:
			utf8Remaining = 3
		}

	case ptyOverflowTrimEsc:
		switch b {
		case '[':
			state = ptyOverflowTrimCSI
		case ']':
			state = ptyOverflowTrimOSC
		case 'P':
			state = ptyOverflowTrimDCS
		case '(', ')':
			state = ptyOverflowTrimCharset
		default:
			state = ptyOverflowTrimText
		}

	case ptyOverflowTrimCSI:
		switch {
		case b >= '0' && b <= '9':
			state = ptyOverflowTrimCSIParam
		case b == ';':
			state = ptyOverflowTrimCSIParam
		case b == '?', b == '>', b == '!', b == '<':
			state = ptyOverflowTrimCSIParam
		case b >= 0x20 && b <= 0x2f:
			state = ptyOverflowTrimCSIParam
		case b >= 0x40 && b <= 0x7e:
			state = ptyOverflowTrimText
		case b == 0x1b:
			state = ptyOverflowTrimEsc
		}

	case ptyOverflowTrimCSIParam:
		switch {
		case b >= '0' && b <= '9':
		case b == ';':
		case b == ':':
		case b >= 0x20 && b <= 0x2f:
		case b >= 0x40 && b <= 0x7e:
			state = ptyOverflowTrimText
		case b == 0x1b:
			state = ptyOverflowTrimEsc
		default:
			state = ptyOverflowTrimText
		}

	case ptyOverflowTrimOSC:
		if b == 0x07 {
			state = ptyOverflowTrimText
		} else if b == 0x1b {
			state = ptyOverflowTrimEsc
		}

	case ptyOverflowTrimDCS:
		if b == 0x1b {
			state = ptyOverflowTrimEsc
		}

	case ptyOverflowTrimCharset:
		state = ptyOverflowTrimText
	}

	return state, utf8Remaining
}
