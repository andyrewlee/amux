package center

type ansiActivityState uint8

const (
	ansiActivityText ansiActivityState = iota
	ansiActivityEsc
	ansiActivityEscSequence
	ansiActivityCSI
	ansiActivityOSC
	ansiActivityOSCEsc
	ansiActivityString
	ansiActivityStringEsc
)

func hasVisiblePTYOutput(data []byte, state ansiActivityState) (bool, ansiActivityState) {
	if len(data) == 0 {
		return false, state
	}
	visible := false
	for _, b := range data {
		switch state {
		case ansiActivityText:
			switch b {
			case 0x1b:
				state = ansiActivityEsc
			default:
				if isVisibleByte(b) {
					visible = true
				}
			}

		case ansiActivityEsc:
			switch b {
			case '[':
				state = ansiActivityCSI
			case ']':
				state = ansiActivityOSC
			case 'P', 'X', '^', '_':
				state = ansiActivityString
			default:
				switch {
				case b >= 0x20 && b <= 0x2f:
					state = ansiActivityEscSequence
				case b >= 0x30 && b <= 0x7e:
					state = ansiActivityText
				default:
					state = ansiActivityText
				}
			}

		case ansiActivityEscSequence:
			if b >= 0x30 && b <= 0x7e {
				state = ansiActivityText
			} else if b == 0x1b {
				state = ansiActivityEsc
			}

		case ansiActivityCSI:
			if b >= 0x40 && b <= 0x7e {
				state = ansiActivityText
			} else if b == 0x1b {
				state = ansiActivityEsc
			}

		case ansiActivityOSC:
			if b == 0x07 {
				state = ansiActivityText
			} else if b == 0x1b {
				state = ansiActivityOSCEsc
			}

		case ansiActivityOSCEsc:
			if b == '\\' {
				state = ansiActivityText
			} else if b != 0x1b {
				state = ansiActivityOSC
			}

		case ansiActivityString:
			if b == 0x1b {
				state = ansiActivityStringEsc
			}

		case ansiActivityStringEsc:
			if b == '\\' {
				state = ansiActivityText
			} else if b != 0x1b {
				state = ansiActivityString
			}
		}
	}
	return visible, state
}

func isVisibleByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return false
	}
	return b >= 0x20 && b != 0x7f
}
