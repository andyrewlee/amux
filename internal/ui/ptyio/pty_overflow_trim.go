package ptyio

import "github.com/andyrewlee/amux/internal/vterm"

// TrimPTYOverflowPrefix drops at least drop bytes from a buffered PTY stream and,
// when the cut lands inside an ANSI/control sequence or UTF-8 rune, advances to
// the next parser-safe boundary. This avoids rendering the tail of a truncated
// control sequence as visible text after overflow backpressure.
//
// Parser continuity is modeled by vterm.AdvanceParserCarryState — the single
// shared chunk-boundary state machine — so trimming can never disagree with the
// terminal parser about where a sequence ends.
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

	state := vterm.AdvanceParserCarryState(seed, data[:drop])
	if drop < len(data) && parserCarryAtTextBoundary(state) {
		return data[drop:], vterm.ParserCarryState{}
	}

	start := drop
	for start < len(data) {
		if isSafePTYOverflowBoundary(state, data[start]) {
			return data[start:], vterm.ParserCarryState{}
		}
		state = vterm.AdvanceParserCarryState(state, data[start:start+1])
		start++
		if parserCarryAtTextBoundary(state) {
			return data[start:], vterm.ParserCarryState{}
		}
	}

	return nil, state
}

func parserCarryAtTextBoundary(state vterm.ParserCarryState) bool {
	return state.Mode == vterm.ParserCarryText && state.UTF8Remaining == 0
}

func isSafePTYOverflowBoundary(state vterm.ParserCarryState, b byte) bool {
	if state.UTF8Remaining > 0 {
		return b < 0x80 || b > 0xBF
	}
	if b != 0x1b {
		return false
	}
	switch state.Mode {
	case vterm.ParserCarryEscape, vterm.ParserCarryCSI, vterm.ParserCarryCSIParam, vterm.ParserCarryOSC, vterm.ParserCarryDCS:
		return true
	default:
		return false
	}
}
