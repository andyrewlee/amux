package center

import (
	"fmt"
	"testing"
)

// TestANSIActivityMachineExhaustive verifies the visible-output detector's
// byte-level automaton by exhaustive quantification over its entire input
// alphabet: every one of the 256 byte values from every one of the 8 states.
// The per-state expectations live in the want* helpers below; the two
// cross-cutting properties pinned here are that an escape byte never
// produces visible output outside text state, and that visibility inside
// text state matches isVisibleByte exactly.
func TestANSIActivityMachineExhaustive(t *testing.T) {
	for state := ansiActivityText; state <= ansiActivityStringEsc; state++ {
		for b := 0; b < 256; b++ {
			visible, next := hasVisiblePTYOutput([]byte{byte(b)}, state)
			label := fmt.Sprintf("state=%d byte=0x%02x", state, b)

			var want ansiActivityState
			switch state {
			case ansiActivityText:
				want = wantTextState(t, label, byte(b), visible)
			case ansiActivityEsc:
				want = wantEscDispatch(byte(b))
			case ansiActivityEscSequence:
				want = wantEscSequence(byte(b))
			case ansiActivityCSI:
				want = wantCSI(byte(b))
			case ansiActivityOSC:
				want = wantOSC(byte(b))
			case ansiActivityOSCEsc:
				want = wantOSCEsc(byte(b))
			case ansiActivityString:
				want = wantString(byte(b))
			case ansiActivityStringEsc:
				want = wantStringEsc(byte(b))
			}
			if next != want {
				t.Errorf("%s: next = %d, want %d", label, next, want)
			}
			if state != ansiActivityText && visible {
				t.Errorf("%s: visibility outside text state", label)
			}
		}
	}
}

// wantTextState pins the text state: ESC enters esc; any other byte stays
// in text with visibility exactly matching isVisibleByte.
func wantTextState(t *testing.T, label string, b byte, visible bool) ansiActivityState {
	t.Helper()
	if b == 0x1b {
		return ansiActivityEsc
	}
	if want := isVisibleByte(b); visible != want {
		t.Errorf("%s: visibility = %v, want %v", label, visible, want)
	}
	return ansiActivityText
}

// wantEscDispatch pins ESC-byte dispatch: '[' → CSI, ']' → OSC, the string
// openers → string, intermediates → escSequence, finals → text. Quirk,
// pinned deliberately: a second ESC aborts to text instead of restarting
// the escape (ECMA-48 would restart). Changing this is a behavior change —
// do it consciously, not in a verification pass.
func wantEscDispatch(b byte) ansiActivityState {
	switch {
	case b == '[':
		return ansiActivityCSI
	case b == ']':
		return ansiActivityOSC
	case b == 'P' || b == 'X' || b == '^' || b == '_':
		return ansiActivityString
	case b >= 0x20 && b <= 0x2f:
		return ansiActivityEscSequence
	default:
		return ansiActivityText
	}
}

func wantEscSequence(b byte) ansiActivityState {
	switch {
	case b >= 0x30 && b <= 0x7e:
		return ansiActivityText
	case b == 0x1b:
		return ansiActivityEsc
	default:
		return ansiActivityEscSequence
	}
}

func wantCSI(b byte) ansiActivityState {
	switch {
	case b >= 0x40 && b <= 0x7e:
		return ansiActivityText
	case b == 0x1b:
		return ansiActivityEsc
	default:
		return ansiActivityCSI
	}
}

func wantOSC(b byte) ansiActivityState {
	switch {
	case b == 0x07:
		return ansiActivityText
	case b == 0x1b:
		return ansiActivityOSCEsc
	default:
		return ansiActivityOSC
	}
}

func wantOSCEsc(b byte) ansiActivityState {
	switch {
	case b == '\\':
		return ansiActivityText
	case b == 0x1b:
		return ansiActivityOSCEsc
	default:
		return ansiActivityOSC
	}
}

func wantString(b byte) ansiActivityState {
	if b == 0x1b {
		return ansiActivityStringEsc
	}
	return ansiActivityString
}

func wantStringEsc(b byte) ansiActivityState {
	switch {
	case b == '\\':
		return ansiActivityText
	case b == 0x1b:
		return ansiActivityStringEsc
	default:
		return ansiActivityString
	}
}
