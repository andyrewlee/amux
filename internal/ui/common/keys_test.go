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

		// Ctrl+letter control bytes (ctrl+i/tab, ctrl+m/enter, and ctrl+q are
		// absent from the switch in keys.go; i and m arrive as KeyTab/KeyEnter).
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

		// Editing and whitespace keys.
		{"backspace", tea.KeyPressMsg{Code: tea.KeyBackspace}, []byte{0x7f}},
		{"tab", tea.KeyPressMsg{Code: tea.KeyTab}, []byte{'\t'}},
		{"shift+tab", tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, []byte{0x1b, '[', 'Z'}},
		{"space", tea.KeyPressMsg{Code: tea.KeySpace}, []byte{' '}},
		{"escape", tea.KeyPressMsg{Code: tea.KeyEscape}, []byte{0x1b}},
		{"delete", tea.KeyPressMsg{Code: tea.KeyDelete}, []byte{0x1b, '[', '3', '~'}},

		// Arrow keys, plain and Alt-modified (CSI 1;3X).
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}, []byte{0x1b, '[', 'A'}},
		{"down", tea.KeyPressMsg{Code: tea.KeyDown}, []byte{0x1b, '[', 'B'}},
		{"right", tea.KeyPressMsg{Code: tea.KeyRight}, []byte{0x1b, '[', 'C'}},
		{"left", tea.KeyPressMsg{Code: tea.KeyLeft}, []byte{0x1b, '[', 'D'}},
		{"alt+up", tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt}, []byte{0x1b, '[', '1', ';', '3', 'A'}},
		{"alt+down", tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt}, []byte{0x1b, '[', '1', ';', '3', 'B'}},
		{"alt+right", tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt}, []byte{0x1b, '[', '1', ';', '3', 'C'}},
		{"alt+left", tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt}, []byte{0x1b, '[', '1', ';', '3', 'D'}},

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

		// Unmapped keys produce no bytes.
		{"unmapped key is nil", tea.KeyPressMsg{Code: tea.KeyF1}, nil},
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
