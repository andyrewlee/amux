package common

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
)

// KeyToBytes converts a key press message to bytes for the terminal.
func KeyToBytes(msg tea.KeyPressMsg) []byte {
	key := msg.Key()
	logging.Debug("KeyToBytes: code=%d mod=%d str=%q", key.Code, key.Mod, msg.String())

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
		}
	}

	switch key.Code {
	case tea.KeyEnter:
		if key.Mod&tea.ModShift != 0 {
			return []byte{0x1b, '[', '1', '3', ';', '2', 'u'}
		}
		return []byte{'\r'}
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeyTab:
		if key.Mod&tea.ModShift != 0 {
			return []byte{0x1b, '[', 'Z'}
		}
		return []byte{'\t'}
	case tea.KeySpace:
		return []byte{' '}
	case tea.KeyEscape:
		return []byte{0x1b}
	case tea.KeyUp:
		if key.Mod&tea.ModAlt != 0 {
			return []byte{0x1b, '[', '1', ';', '3', 'A'}
		}
		return []byte{0x1b, '[', 'A'}
	case tea.KeyDown:
		if key.Mod&tea.ModAlt != 0 {
			return []byte{0x1b, '[', '1', ';', '3', 'B'}
		}
		return []byte{0x1b, '[', 'B'}
	case tea.KeyRight:
		if key.Mod&tea.ModAlt != 0 {
			return []byte{0x1b, '[', '1', ';', '3', 'C'}
		}
		return []byte{0x1b, '[', 'C'}
	case tea.KeyLeft:
		if key.Mod&tea.ModAlt != 0 {
			return []byte{0x1b, '[', '1', ';', '3', 'D'}
		}
		return []byte{0x1b, '[', 'D'}
	case tea.KeyHome:
		return []byte{0x1b, '[', 'H'}
	case tea.KeyEnd:
		return []byte{0x1b, '[', 'F'}
	case tea.KeyDelete:
		return []byte{0x1b, '[', '3', '~'}
	case tea.KeyPgUp:
		return []byte{0x1b, '[', '5', '~'}
	case tea.KeyPgDown:
		return []byte{0x1b, '[', '6', '~'}
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
