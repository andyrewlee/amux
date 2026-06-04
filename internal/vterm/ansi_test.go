package vterm

import "testing"

// goldenStyle builds Style/Color literals tersely for the table below.
func idxColor(v uint32) Color { return Color{Type: ColorIndexed, Value: v} }
func rgbColor(v uint32) Color { return Color{Type: ColorRGB, Value: v} }

// TestStyleANSIAndDeltaGolden is a characterization test locking the exact SGR
// output of Style.ANSI and Style.DeltaANSI across both transition branches
// (full-reset and individual-change), the bold/dim code-22 interaction, every
// color kind (indexed <8, bright 8-15, 256-color, RGB) and return-to-default.
// The golden strings were captured from the original free-function
// implementation so any behavior drift in the method form fails loudly.
func TestStyleANSIAndDeltaGolden(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		prev, next  Style
		wantDelta   string
		wantFullNxt string
	}{
		{"nochange", Style{Bold: true, Fg: idxColor(3)}, Style{Bold: true, Fg: idxColor(3)}, "", "\x1b[0;1;33m"},
		{"blank_to_bold", Style{}, Style{Bold: true}, "\x1b[1m", "\x1b[0;1m"},
		{"blank_to_all_attrs", Style{}, Style{Bold: true, Dim: true, Italic: true, Underline: true, Blink: true, Reverse: true, Hidden: true, Strike: true}, "\x1b[1;2;3;4;5;7;8;9m", "\x1b[0;1;2;3;4;5;7;8;9m"},
		{"bold_off_single", Style{Bold: true}, Style{}, "\x1b[22m", "\x1b[0m"},
		{"dim_off_single", Style{Dim: true}, Style{}, "\x1b[22m", "\x1b[0m"},
		{"italic_off", Style{Italic: true}, Style{}, "\x1b[23m", "\x1b[0m"},
		{"underline_off", Style{Underline: true}, Style{}, "\x1b[24m", "\x1b[0m"},
		{"two_off_triggers_reset", Style{Bold: true, Italic: true}, Style{}, "\x1b[0m", "\x1b[0m"},
		{"three_off_reset_keep_underline", Style{Bold: true, Italic: true, Blink: true, Underline: true}, Style{Underline: true}, "\x1b[0;4m", "\x1b[0;4m"},
		{"bold_to_dim_emitted22", Style{Bold: true}, Style{Dim: true}, "\x1b[22;2m", "\x1b[0;2m"},
		{"dim_to_bold_emitted22", Style{Dim: true}, Style{Bold: true}, "\x1b[22;1m", "\x1b[0;1m"},
		{"bolddim_to_bold", Style{Bold: true, Dim: true}, Style{Bold: true}, "\x1b[22;1m", "\x1b[0;1m"},
		{"fg_indexed_low", Style{}, Style{Fg: idxColor(2)}, "\x1b[32m", "\x1b[0;32m"},
		{"fg_indexed_bright", Style{}, Style{Fg: idxColor(12)}, "\x1b[94m", "\x1b[0;94m"},
		{"fg_indexed_256", Style{}, Style{Fg: idxColor(200)}, "\x1b[38;5;200m", "\x1b[0;38;5;200m"},
		{"fg_rgb", Style{}, Style{Fg: rgbColor(0x1affc0)}, "\x1b[38;2;26;255;192m", "\x1b[0;38;2;26;255;192m"},
		{"bg_indexed_low", Style{}, Style{Bg: idxColor(1)}, "\x1b[41m", "\x1b[0;41m"},
		{"bg_rgb", Style{}, Style{Bg: rgbColor(0x102030)}, "\x1b[48;2;16;32;48m", "\x1b[0;48;2;16;32;48m"},
		{"fg_to_default", Style{Fg: idxColor(5)}, Style{}, "\x1b[39m", "\x1b[0m"},
		{"bg_to_default", Style{Bg: idxColor(5)}, Style{}, "\x1b[49m", "\x1b[0m"},
		{"fg_change", Style{Fg: idxColor(1)}, Style{Fg: idxColor(4)}, "\x1b[34m", "\x1b[0;34m"},
		{"bg_change", Style{Bg: rgbColor(0x010203)}, Style{Bg: rgbColor(0x040506)}, "\x1b[48;2;4;5;6m", "\x1b[0;48;2;4;5;6m"},
		{"attr_and_color", Style{}, Style{Bold: true, Underline: true, Fg: idxColor(2), Bg: rgbColor(0xabcdef)}, "\x1b[1;4;32;48;2;171;205;239m", "\x1b[0;1;4;32;48;2;171;205;239m"},
		{"reset_with_color", Style{Bold: true, Italic: true, Fg: idxColor(1)}, Style{Fg: idxColor(2)}, "\x1b[0;32m", "\x1b[0;32m"},
		{"complex_mix", Style{Bold: true, Dim: true, Fg: idxColor(7), Bg: idxColor(0)}, Style{Italic: true, Fg: rgbColor(0x00ff00)}, "\x1b[0;3;38;2;0;255;0m", "\x1b[0;3;38;2;0;255;0m"},
		{"strike_blink_on", Style{}, Style{Strike: true, Blink: true}, "\x1b[5;9m", "\x1b[0;5;9m"},
		{"reverse_hidden_off", Style{Reverse: true, Hidden: true}, Style{}, "\x1b[0m", "\x1b[0m"},
		{"one_off_one_on", Style{Italic: true}, Style{Bold: true}, "\x1b[23;1m", "\x1b[0;1m"},
		{"fg_rgb_to_indexed", Style{Fg: rgbColor(0x123456)}, Style{Fg: idxColor(9)}, "\x1b[91m", "\x1b[0;91m"},
		{"all_to_blank", Style{Bold: true, Dim: true, Italic: true, Underline: true, Blink: true, Reverse: true, Hidden: true, Strike: true, Fg: idxColor(3), Bg: idxColor(4)}, Style{}, "\x1b[0m", "\x1b[0m"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.prev.DeltaANSI(c.next); got != c.wantDelta {
				t.Errorf("DeltaANSI: got %q, want %q", got, c.wantDelta)
			}
			if got := c.next.ANSI(); got != c.wantFullNxt {
				t.Errorf("ANSI: got %q, want %q", got, c.wantFullNxt)
			}
		})
	}
}
