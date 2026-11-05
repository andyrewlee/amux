package common

import (
	"bytes"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestKeyToBytes pins the exact byte output of every KeyToBytes branch. It is
// the fast-suite guard for the keystroke→PTY translation that the tmux-gated
// e2e close-loop test only covers when tmux is present.
func TestKeyToBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  tea.KeyPressMsg
		want []byte
	}{
		// Named-Enter regression guard: Enter must be CR (\r), never LF (\n).
		// This is the documented historical regression the e2e test cites.
		{"enter is CR not LF", tea.KeyPressMsg{Code: tea.KeyEnter}, []byte{'\r'}},

		// Ctrl+letter control bytes (ctrl+i/tab and ctrl+m/enter arrive as
		// KeyTab/KeyEnter — they are handled by the non-ctrl switch below).
		{"ctrl+a", tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}, []byte{0x01}},
		{"ctrl+b", tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}, []byte{0x02}},
		{"ctrl+c", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, []byte{0x03}},
		{"ctrl+d", tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}, []byte{0x04}},
		{"ctrl+e", tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}, []byte{0x05}},
		{"ctrl+f", tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}, []byte{0x06}},
		{"ctrl+g", tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}, []byte{0x07}},
		{"ctrl+h", tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl}, []byte{0x08}},
		{"ctrl+j", tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}, []byte{0x0a}},
		{"ctrl+k", tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}, []byte{0x0b}},
		{"ctrl+l", tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl}, []byte{0x0c}},
		{"ctrl+n", tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}, []byte{0x0e}},
		{"ctrl+o", tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}, []byte{0x0f}},
		{"ctrl+p", tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}, []byte{0x10}},
		{"ctrl+q", tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl}, []byte{0x11}},
		{"ctrl+r", tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}, []byte{0x12}},
		{"ctrl+s", tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}, []byte{0x13}},
		{"ctrl+t", tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}, []byte{0x14}},
		{"ctrl+u", tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, []byte{0x15}},
		{"ctrl+v", tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl}, []byte{0x16}},
		{"ctrl+w", tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}, []byte{0x17}},
		{"ctrl+x", tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}, []byte{0x18}},
		{"ctrl+y", tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}, []byte{0x19}},
		{"ctrl+z", tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl}, []byte{0x1a}},

		// Terminals deliver ctrl+i as Tab and ctrl+m as Enter; keys.go routes
		// them through the non-ctrl switch (the "handled below" comment).
		{"ctrl+i as tab", tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModCtrl}, []byte{'\t'}},
		{"ctrl+m as enter", tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}, []byte{'\r'}},

		// Ctrl+] reaches a focused terminal as its C0 byte; the center pane
		// reserves it for tab cycling before the encoder ever sees it.
		{"ctrl+]", tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl}, []byte{0x1d}},

		// Editing and whitespace keys. Alt forms are the ESC-prefixed legacy
		// encodings; keypad Enter is CR (the form every agent parser reads).
		{"backspace", tea.KeyPressMsg{Code: tea.KeyBackspace}, []byte{0x7f}},
		{"tab", tea.KeyPressMsg{Code: tea.KeyTab}, []byte{'\t'}},
		{"alt+tab", tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModAlt}, []byte{0x1b, '\t'}},
		{"shift+tab", tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, []byte{0x1b, '[', 'Z'}},
		{"alt+shift+tab", tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift | tea.ModAlt}, []byte{0x1b, 0x1b, '[', 'Z'}},
		{"space", tea.KeyPressMsg{Code: tea.KeySpace}, []byte{' '}},
		{"ctrl+space is NUL", tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl}, []byte{0x00}},
		{"ctrl+alt+space", tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl | tea.ModAlt}, []byte{0x1b, 0x00}},
		{"alt+space", tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModAlt}, []byte{0x1b, ' '}},
		{"escape", tea.KeyPressMsg{Code: tea.KeyEscape}, []byte{0x1b}},
		{"alt+escape", tea.KeyPressMsg{Code: tea.KeyEscape, Mod: tea.ModAlt}, []byte{0x1b, 0x1b}},
		{"alt+enter", tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}, []byte{0x1b, '\r'}},
		{"alt+backspace", tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}, []byte{0x1b, 0x7f}},
		{"keypad enter is CR", tea.KeyPressMsg{Code: tea.KeyKpEnter}, []byte{'\r'}},
		{"delete", tea.KeyPressMsg{Code: tea.KeyDelete}, []byte{0x1b, '[', '3', '~'}},
		{"insert", tea.KeyPressMsg{Code: tea.KeyInsert}, []byte{0x1b, '[', '2', '~'}},

		// F-keys: F1-F4 ride SS3 unmodified, CSI 1;mX modified; F5-F12 use
		// tilde finals that skip 16 and 22 (xterm reserved them). Modifier
		// combinations are pinned exhaustively by the matrix test below.
		{"f1", tea.KeyPressMsg{Code: tea.KeyF1}, []byte{0x1b, 'O', 'P'}},
		{"f4", tea.KeyPressMsg{Code: tea.KeyF4}, []byte{0x1b, 'O', 'S'}},
		{"f5", tea.KeyPressMsg{Code: tea.KeyF5}, []byte{0x1b, '[', '1', '5', '~'}},
		{"f12", tea.KeyPressMsg{Code: tea.KeyF12}, []byte{0x1b, '[', '2', '4', '~'}},

		// Arrow keys, plain and Alt-modified (CSI 1;3X). The full modifier
		// matrix lives in TestKeyToBytesXtermModifierMatrix; these two rows
		// stay as named regression guards for the bug that prompted it —
		// Codex answers plan-approval prompts with shift+left, and shells
		// word-jump on ctrl+right.
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}, []byte{0x1b, '[', 'A'}},
		{"down", tea.KeyPressMsg{Code: tea.KeyDown}, []byte{0x1b, '[', 'B'}},
		{"right", tea.KeyPressMsg{Code: tea.KeyRight}, []byte{0x1b, '[', 'C'}},
		{"left", tea.KeyPressMsg{Code: tea.KeyLeft}, []byte{0x1b, '[', 'D'}},
		{"alt+up", tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt}, []byte{0x1b, '[', '1', ';', '3', 'A'}},
		{"alt+down", tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt}, []byte{0x1b, '[', '1', ';', '3', 'B'}},
		{"alt+right", tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt}, []byte{0x1b, '[', '1', ';', '3', 'C'}},
		{"alt+left", tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt}, []byte{0x1b, '[', '1', ';', '3', 'D'}},
		{"shift+left", tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift}, []byte{0x1b, '[', '1', ';', '2', 'D'}},
		{"ctrl+right", tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl}, []byte{0x1b, '[', '1', ';', '5', 'C'}},

		// Navigation keys.
		{"home", tea.KeyPressMsg{Code: tea.KeyHome}, []byte{0x1b, '[', 'H'}},
		{"end", tea.KeyPressMsg{Code: tea.KeyEnd}, []byte{0x1b, '[', 'F'}},
		{"pgup", tea.KeyPressMsg{Code: tea.KeyPgUp}, []byte{0x1b, '[', '5', '~'}},
		{"pgdown", tea.KeyPressMsg{Code: tea.KeyPgDown}, []byte{0x1b, '[', '6', '~'}},

		// Alt+text is ESC-prefixed.
		{"alt+x", tea.KeyPressMsg{Code: 'x', Mod: tea.ModAlt, Text: "x"}, []byte{0x1b, 'x'}},

		// Plain text passes through as its UTF-8 bytes.
		{"plain letter", tea.KeyPressMsg{Code: 'a', Text: "a"}, []byte("a")},
		{"multi-byte rune", tea.KeyPressMsg{Code: '世', Text: "世"}, []byte("世")},

		// Fallback: no Text, but msg.String() is a single character.
		{"single-char fallback", tea.KeyPressMsg{Code: 'x'}, []byte("x")},

		// F13+ have no unambiguous legacy byte form (their tilde finals
		// collide with shifted-F-key conventions across terminals); amux
		// drops them rather than guessing.
		{"ambiguous F13 is nil", tea.KeyPressMsg{Code: tea.KeyF13}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := KeyToBytes(tt.msg)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("KeyToBytes(%v) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}

// xtermSpecModParams derives the CSI modifier parameter from the xterm
// specification (shift=1, alt=2, ctrl=4, offset by 1) independently of
// xtermModParam: if the encoder's derivation ever changes or breaks, this
// table disagrees rather than silently following the implementation.
var xtermSpecModParams = map[tea.KeyMod]string{
	tea.ModShift:                            "2",
	tea.ModAlt:                              "3",
	tea.ModShift | tea.ModAlt:               "4",
	tea.ModCtrl:                             "5",
	tea.ModShift | tea.ModCtrl:              "6",
	tea.ModAlt | tea.ModCtrl:                "7",
	tea.ModShift | tea.ModAlt | tea.ModCtrl: "8",
}

// TestKeyToBytesXtermModifierMatrix pins the modifier-parameter form for
// every letter- and tilde-form CSI key across every encodable modifier
// combination. This is the guard for the bug class where one combination
// (shift+left) degraded to its unmodified form and left an agent prompt
// unanswerable: a single dropped modifier now fails the whole family.
func TestKeyToBytesXtermModifierMatrix(t *testing.T) {
	t.Parallel()

	letters := map[rune]byte{
		tea.KeyUp: 'A', tea.KeyDown: 'B', tea.KeyRight: 'C', tea.KeyLeft: 'D',
		tea.KeyHome: 'H', tea.KeyEnd: 'F',
	}
	for code, final := range letters {
		for mod, param := range xtermSpecModParams {
			msg := tea.KeyPressMsg{Code: code, Mod: mod}
			want := "\x1b[1;" + param + string(final)
			t.Run(msg.String(), func(t *testing.T) {
				t.Parallel()
				if got := KeyToBytes(msg); !bytes.Equal(got, []byte(want)) {
					t.Errorf("KeyToBytes(%v) = %q, want %q", msg, got, want)
				}
			})
		}
	}

	tildes := map[rune]string{
		tea.KeyInsert: "2", tea.KeyDelete: "3", tea.KeyPgUp: "5", tea.KeyPgDown: "6",
		tea.KeyF5: "15", tea.KeyF6: "17", tea.KeyF7: "18", tea.KeyF8: "19",
		tea.KeyF9: "20", tea.KeyF10: "21", tea.KeyF11: "23", tea.KeyF12: "24",
	}
	for code, num := range tildes {
		for mod, param := range xtermSpecModParams {
			msg := tea.KeyPressMsg{Code: code, Mod: mod}
			want := "\x1b[" + num + ";" + param + "~"
			t.Run(msg.String(), func(t *testing.T) {
				t.Parallel()
				if got := KeyToBytes(msg); !bytes.Equal(got, []byte(want)) {
					t.Errorf("KeyToBytes(%v) = %q, want %q", msg, got, want)
				}
			})
		}
	}

	fkeys := map[rune]byte{
		tea.KeyF1: 'P', tea.KeyF2: 'Q', tea.KeyF3: 'R', tea.KeyF4: 'S',
	}
	for code, final := range fkeys {
		for mod, param := range xtermSpecModParams {
			msg := tea.KeyPressMsg{Code: code, Mod: mod}
			want := "\x1b[1;" + param + string(final)
			t.Run(msg.String(), func(t *testing.T) {
				t.Parallel()
				if got := KeyToBytes(msg); !bytes.Equal(got, []byte(want)) {
					t.Errorf("KeyToBytes(%v) = %q, want %q", msg, got, want)
				}
			})
		}
	}
}
