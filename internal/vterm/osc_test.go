package vterm

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestOSC covers OSC sequence parsing: title capture (OSC 0/1/2), working
// directory (OSC 7), clipboard (OSC 52), and the ST-termination bug fix.
func TestOSC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		input             string
		wantTitle         string
		wantWorkingDir    string
		wantClipboard     string // "" means nil expected
		wantClipboardNil  bool   // explicit nil expectation (query/invalid)
		wantSecondTakeNil bool   // second TakePendingClipboard should be nil
	}{
		{
			name:      "OSC 0 title BEL terminated",
			input:     "\x1b]0;hello\x07",
			wantTitle: "hello",
		},
		{
			name:      "OSC 2 title ST terminated",
			input:     "\x1b]2;world\x1b\\",
			wantTitle: "world",
		},
		{
			name:           "OSC 7 working directory",
			input:          "\x1b]7;file://h/tmp\x07",
			wantWorkingDir: "file://h/tmp",
		},
		{
			name:              "OSC 52 write round-trip",
			input:             "\x1b]52;c;aGk=\x07",
			wantClipboard:     "hi",
			wantSecondTakeNil: true,
		},
		{
			name:             "OSC 52 query is ignored",
			input:            "\x1b]52;c;?\x07",
			wantClipboardNil: true,
		},
		{
			name:             "OSC 52 invalid base64 is ignored",
			input:            "\x1b]52;c;!!!notbase64!!!\x07",
			wantClipboardNil: true,
		},
		{
			name:      "unrecognized OSC no panic title unchanged",
			input:     "\x1b]10;rgb:00/00/00\x07",
			wantTitle: "",
		},
		{
			name:  "OSC followed by normal text lands on screen",
			input: "\x1b]0;t\x07AB",
			// Assertions handled inline below (cell content check).
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := New(80, 24)
			v.Write([]byte(tc.input))

			if tc.wantTitle != "" && v.Title() != tc.wantTitle {
				t.Errorf("Title() = %q; want %q", v.Title(), tc.wantTitle)
			}
			if tc.wantWorkingDir != "" && v.WorkingDir() != tc.wantWorkingDir {
				t.Errorf("WorkingDir() = %q; want %q", v.WorkingDir(), tc.wantWorkingDir)
			}

			if tc.wantClipboard != "" {
				got := v.TakePendingClipboard()
				if string(got) != tc.wantClipboard {
					t.Errorf("TakePendingClipboard() = %q; want %q", string(got), tc.wantClipboard)
				}
				if tc.wantSecondTakeNil {
					second := v.TakePendingClipboard()
					if second != nil {
						t.Errorf("second TakePendingClipboard() = %q; want nil", string(second))
					}
				}
			}

			if tc.wantClipboardNil {
				got := v.TakePendingClipboard()
				if got != nil {
					t.Errorf("TakePendingClipboard() = %q; want nil", string(got))
				}
			}

			// Case: OSC followed by normal text must not corrupt the screen.
			if tc.name == "OSC followed by normal text lands on screen" {
				screen := v.VisibleScreen()
				if len(screen) == 0 || len(screen[0]) < 2 {
					t.Fatal("screen too small to check")
				}
				if screen[0][0].Rune != 'A' {
					t.Errorf("screen[0][0].Rune = %q; want 'A'", screen[0][0].Rune)
				}
				if screen[0][1].Rune != 'B' {
					t.Errorf("screen[0][1].Rune = %q; want 'B'", screen[0][1].Rune)
				}
			}
		})
	}
}

func TestOSC52OversizedPayloadIgnored(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("x", maxOSC52ClipboardBytes+1)
	encoded := base64.StdEncoding.EncodeToString([]byte(payload))
	v := New(80, 24)
	v.Write([]byte("\x1b]52;c;" + encoded + "\x07"))

	if got := v.TakePendingClipboard(); got != nil {
		t.Fatalf("expected oversized OSC52 payload to be ignored, got %d bytes", len(got))
	}
}
