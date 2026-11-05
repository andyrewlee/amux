package common

import tea "charm.land/bubbletea/v2"

// xtermModParam returns the xterm CSI modifier parameter (1 + shift(1) +
// alt(2) + ctrl(4)) for the encodable modifier bits, or 0 when no encodable
// modifier is held. Super/hyper/meta have no legacy CSI encoding and are
// ignored, matching what terminals actually emit for these sequences.
func xtermModParam(mod tea.KeyMod) int {
	param := 0
	if mod&tea.ModShift != 0 {
		param += 1
	}
	if mod&tea.ModAlt != 0 {
		param += 2
	}
	if mod&tea.ModCtrl != 0 {
		param += 4
	}
	if param == 0 {
		return 0
	}
	return param + 1
}

// csiKey renders a letter-form CSI navigation key (arrows 'A'-'D', 'H' home,
// 'F' end) in its unmodified or modifier-parameter form; csiKey('D', 2) is
// shift+left ("\x1b[1;2D"). Agent TUIs key off the exact sequence — e.g.
// plan-approval prompts answer to shift+arrow — so dropping the modifier
// changes which key the agent sees.
func csiKey(final byte, mod tea.KeyMod) []byte {
	if p := xtermModParam(mod); p != 0 {
		return []byte{0x1b, '[', '1', ';', '0' + byte(p), final}
	}
	return []byte{0x1b, '[', final}
}

// csiTildeKey renders a tilde-form CSI key ('2' insert, '3' delete, '5'
// pgup, '6' pgdown, and the F5-F12 finals) in its unmodified or
// modifier-parameter form; csiTildeKey("3", 5) is ctrl+delete ("\x1b[3;5~").
func csiTildeKey(num string, mod tea.KeyMod) []byte {
	seq := append([]byte{0x1b, '['}, num...)
	if p := xtermModParam(mod); p != 0 {
		seq = append(seq, ';', '0'+byte(p))
	}
	return append(seq, '~')
}

// fKeySS3 and fKeyTilde encode F1-F12 in xterm form. F1-F4 ride SS3 P/Q/R/S
// unmodified (SS3 cannot carry modifiers, so modified forms use CSI 1;mX);
// F5-F12 use tilde finals, which skip 16 and 22 — xterm reserved them
// against historical rxvt conflicts.
var (
	fKeySS3 = map[rune]byte{
		tea.KeyF1: 'P', tea.KeyF2: 'Q', tea.KeyF3: 'R', tea.KeyF4: 'S',
	}
	fKeyTilde = map[rune]string{
		tea.KeyF5: "15", tea.KeyF6: "17", tea.KeyF7: "18", tea.KeyF8: "19",
		tea.KeyF9: "20", tea.KeyF10: "21", tea.KeyF11: "23", tea.KeyF12: "24",
	}
)

// fKeyBytes returns the xterm encoding for an F-key, or ok=false for F13+:
// their legacy tilde finals (25-34) collide with shifted-F-key conventions
// that differ across terminals, and under the kitty keyboard protocol (which
// amux does not forward) they are distinct keys — either way there is no
// single byte form worth guessing, so they are dropped.
func fKeyBytes(code rune, mod tea.KeyMod) ([]byte, bool) {
	if final, ok := fKeySS3[code]; ok {
		if xtermModParam(mod) != 0 {
			return csiKey(final, mod), true
		}
		return []byte{0x1b, 'O', final}, true
	}
	if num, ok := fKeyTilde[code]; ok {
		return csiTildeKey(num, mod), true
	}
	return nil, false
}

// KeyToBytes converts a key press message to bytes for the terminal. It is
// total over the legacy xterm key space — every key and modifier combination
// a terminal can distinguish becomes exactly the bytes that terminal would
// send — so an embedded agent TUI observes the same keyboard the user has.
// F13+ (ambiguous legacy finals) and super/hyper/meta have no legacy
// encoding and return nil.
func KeyToBytes(msg tea.KeyPressMsg) []byte {
	key := msg.Key()

	if key.Mod&tea.ModCtrl != 0 {
		switch key.Code {
		case 'a':
			return []byte{0x01}
		case 'b':
			return []byte{0x02}
		case 'c':
			return []byte{0x03}
		case 'd':
			return []byte{0x04}
		case 'e':
			return []byte{0x05}
		case 'f':
			return []byte{0x06}
		case 'g':
			return []byte{0x07}
		case 'h':
			return []byte{0x08}
		// ctrl+i is tab, ctrl+m is enter; handled below
		case 'j':
			return []byte{0x0a}
		case 'k':
			return []byte{0x0b}
		case 'l':
			return []byte{0x0c}
		case 'n':
			return []byte{0x0e}
		case 'o':
			return []byte{0x0f}
		case 'p':
			return []byte{0x10}
		case 'q':
			return []byte{0x11}
		case 'r':
			return []byte{0x12}
		case 's':
			return []byte{0x13}
		case 't':
			return []byte{0x14}
		case 'u':
			return []byte{0x15}
		case 'v':
			return []byte{0x16}
		case 'w':
			return []byte{0x17}
		case 'x':
			return []byte{0x18}
		case 'y':
			return []byte{0x19}
		case 'z':
			return []byte{0x1a}
		case ']':
			return []byte{0x1d}
		}
	}

	switch key.Code {
	case tea.KeyEnter, tea.KeyKpEnter:
		// Keypad Enter (SS3 M under DECKPAM) is Enter; CR is the form every
		// agent parser reads.
		if key.Mod&tea.ModAlt != 0 {
			return []byte{0x1b, '\r'}
		}
		return []byte{'\r'}
	case tea.KeyBackspace:
		if key.Mod&tea.ModAlt != 0 {
			return []byte{0x1b, 0x7f}
		}
		return []byte{0x7f}
	case tea.KeyTab:
		if key.Mod&tea.ModShift != 0 {
			if key.Mod&tea.ModAlt != 0 {
				return []byte{0x1b, 0x1b, '[', 'Z'}
			}
			return []byte{0x1b, '[', 'Z'}
		}
		if key.Mod&tea.ModAlt != 0 {
			return []byte{0x1b, '\t'}
		}
		return []byte{'\t'}
	case tea.KeySpace:
		// Terminals distinguish ctrl+space as NUL (bubbletea decodes 0x00 to
		// KeySpace+ModCtrl; ESC prefixes it when alt is also held); sending a
		// literal space would change the key the agent sees. The plain form
		// never reaches here through the TUI — amux binds it as the prefix
		// key — but ctrl+alt+space does, and non-TUI callers get the
		// faithful bytes.
		if key.Mod&tea.ModCtrl != 0 {
			if key.Mod&tea.ModAlt != 0 {
				return []byte{0x1b, 0x00}
			}
			return []byte{0x00}
		}
		if key.Mod&tea.ModAlt != 0 {
			return []byte{0x1b, ' '}
		}
		return []byte{' '}
	case tea.KeyEscape:
		if key.Mod&tea.ModAlt != 0 {
			return []byte{0x1b, 0x1b}
		}
		return []byte{0x1b}
	case tea.KeyUp:
		return csiKey('A', key.Mod)
	case tea.KeyDown:
		return csiKey('B', key.Mod)
	case tea.KeyRight:
		return csiKey('C', key.Mod)
	case tea.KeyLeft:
		return csiKey('D', key.Mod)
	case tea.KeyHome:
		return csiKey('H', key.Mod)
	case tea.KeyEnd:
		return csiKey('F', key.Mod)
	case tea.KeyInsert:
		return csiTildeKey("2", key.Mod)
	case tea.KeyDelete:
		return csiTildeKey("3", key.Mod)
	case tea.KeyPgUp:
		return csiTildeKey("5", key.Mod)
	case tea.KeyPgDown:
		return csiTildeKey("6", key.Mod)
	}

	if seq, ok := fKeyBytes(key.Code, key.Mod); ok {
		return seq
	}

	if key.Mod&tea.ModAlt != 0 && key.Text != "" {
		return append([]byte{0x1b}, []byte(key.Text)...)
	}

	if key.Text != "" {
		return []byte(key.Text)
	}

	if s := msg.String(); len(s) == 1 {
		return []byte(s)
	}

	return nil
}
