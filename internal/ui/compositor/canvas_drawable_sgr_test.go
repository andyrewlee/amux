package compositor

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// TestApplySGR exercises applySGR directly across every SGR family it handles:
// attribute set/reset, basic/bright/256/truecolor fg+bg, default resets, and
// malformed/truncated extended-color sequences. colorEqualRGBA and the
// rgbColorVal/ansiColor helpers live in canvas_drawable_test.go (same package).
func TestApplySGR(t *testing.T) {
	bold := uv.Style{Attrs: uv.AttrBold}

	tests := []struct {
		name   string
		start  uv.Style
		params ansi.Params
		check  func(t *testing.T, got uv.Style)
	}{
		{
			name:   "no params resets to zero style",
			start:  bold,
			params: ansi.Params{},
			check: func(t *testing.T, got uv.Style) {
				if (got != uv.Style{}) {
					t.Errorf("expected zero style, got %#v", got)
				}
			},
		},
		{
			name:   "reset param 0 clears existing style",
			start:  bold,
			params: ansi.Params{ansi.Param(0)},
			check: func(t *testing.T, got uv.Style) {
				if (got != uv.Style{}) {
					t.Errorf("expected zero style after reset, got %#v", got)
				}
			},
		},
		{
			name:   "bold sets bold attr",
			params: ansi.Params{ansi.Param(1)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&uv.AttrBold == 0 {
					t.Errorf("expected bold, attrs = %d", got.Attrs)
				}
			},
		},
		{
			name:   "faint sets faint attr",
			params: ansi.Params{ansi.Param(2)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&uv.AttrFaint == 0 {
					t.Errorf("expected faint, attrs = %d", got.Attrs)
				}
			},
		},
		{
			name:   "italic sets italic attr",
			params: ansi.Params{ansi.Param(3)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&uv.AttrItalic == 0 {
					t.Errorf("expected italic, attrs = %d", got.Attrs)
				}
			},
		},
		{
			name:   "underline sets single underline",
			params: ansi.Params{ansi.Param(4)},
			check: func(t *testing.T, got uv.Style) {
				if got.Underline != uv.UnderlineSingle {
					t.Errorf("expected single underline, got %v", got.Underline)
				}
			},
		},
		{
			name:   "blink sets blink attr",
			params: ansi.Params{ansi.Param(5)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&uv.AttrBlink == 0 {
					t.Errorf("expected blink, attrs = %d", got.Attrs)
				}
			},
		},
		{
			name:   "reverse sets reverse attr",
			params: ansi.Params{ansi.Param(7)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&uv.AttrReverse == 0 {
					t.Errorf("expected reverse, attrs = %d", got.Attrs)
				}
			},
		},
		{
			name:   "conceal sets conceal attr",
			params: ansi.Params{ansi.Param(8)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&uv.AttrConceal == 0 {
					t.Errorf("expected conceal, attrs = %d", got.Attrs)
				}
			},
		},
		{
			name:   "strikethrough sets strikethrough attr",
			params: ansi.Params{ansi.Param(9)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&uv.AttrStrikethrough == 0 {
					t.Errorf("expected strikethrough, attrs = %d", got.Attrs)
				}
			},
		},
		{
			name:   "22 clears bold and faint but keeps others",
			start:  uv.Style{Attrs: uv.AttrBold | uv.AttrFaint | uv.AttrItalic},
			params: ansi.Params{ansi.Param(22)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&(uv.AttrBold|uv.AttrFaint) != 0 {
					t.Errorf("expected bold/faint cleared, attrs = %d", got.Attrs)
				}
				if got.Attrs&uv.AttrItalic == 0 {
					t.Errorf("expected italic preserved, attrs = %d", got.Attrs)
				}
			},
		},
		{
			name:   "23 clears italic",
			start:  uv.Style{Attrs: uv.AttrItalic},
			params: ansi.Params{ansi.Param(23)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&uv.AttrItalic != 0 {
					t.Errorf("expected italic cleared, attrs = %d", got.Attrs)
				}
			},
		},
		{
			name:   "24 clears underline",
			start:  uv.Style{Underline: uv.UnderlineSingle},
			params: ansi.Params{ansi.Param(24)},
			check: func(t *testing.T, got uv.Style) {
				if got.Underline != uv.UnderlineNone {
					t.Errorf("expected underline none, got %v", got.Underline)
				}
			},
		},
		{
			name:   "25 clears blink",
			start:  uv.Style{Attrs: uv.AttrBlink},
			params: ansi.Params{ansi.Param(25)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&uv.AttrBlink != 0 {
					t.Errorf("expected blink cleared, attrs = %d", got.Attrs)
				}
			},
		},
		{
			name:   "27 clears reverse",
			start:  uv.Style{Attrs: uv.AttrReverse},
			params: ansi.Params{ansi.Param(27)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&uv.AttrReverse != 0 {
					t.Errorf("expected reverse cleared, attrs = %d", got.Attrs)
				}
			},
		},
		{
			name:   "28 clears conceal",
			start:  uv.Style{Attrs: uv.AttrConceal},
			params: ansi.Params{ansi.Param(28)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&uv.AttrConceal != 0 {
					t.Errorf("expected conceal cleared, attrs = %d", got.Attrs)
				}
			},
		},
		{
			name:   "29 clears strikethrough",
			start:  uv.Style{Attrs: uv.AttrStrikethrough},
			params: ansi.Params{ansi.Param(29)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&uv.AttrStrikethrough != 0 {
					t.Errorf("expected strikethrough cleared, attrs = %d", got.Attrs)
				}
			},
		},
		{
			name:   "30-37 sets basic foreground",
			params: ansi.Params{ansi.Param(32)}, // green
			check: func(t *testing.T, got uv.Style) {
				if !colorEqualRGBA(t, got.Fg, ansiColor(2)) {
					t.Errorf("expected ansi color 2, got %#v", got.Fg)
				}
			},
		},
		{
			name:   "39 resets foreground to default (nil)",
			start:  uv.Style{Fg: ansiColor(5)},
			params: ansi.Params{ansi.Param(39)},
			check: func(t *testing.T, got uv.Style) {
				if got.Fg != nil {
					t.Errorf("expected nil fg, got %#v", got.Fg)
				}
			},
		},
		{
			name:   "40-47 sets basic background",
			params: ansi.Params{ansi.Param(44)}, // blue bg
			check: func(t *testing.T, got uv.Style) {
				if !colorEqualRGBA(t, got.Bg, ansiColor(4)) {
					t.Errorf("expected ansi color 4 bg, got %#v", got.Bg)
				}
			},
		},
		{
			name:   "49 resets background to default (nil)",
			start:  uv.Style{Bg: ansiColor(5)},
			params: ansi.Params{ansi.Param(49)},
			check: func(t *testing.T, got uv.Style) {
				if got.Bg != nil {
					t.Errorf("expected nil bg, got %#v", got.Bg)
				}
			},
		},
		{
			name:   "90-97 sets bright foreground (offset by 8)",
			params: ansi.Params{ansi.Param(91)}, // bright red -> ansi 9
			check: func(t *testing.T, got uv.Style) {
				if !colorEqualRGBA(t, got.Fg, ansiColor(9)) {
					t.Errorf("expected ansi color 9, got %#v", got.Fg)
				}
			},
		},
		{
			name:   "100-107 sets bright background (offset by 8)",
			params: ansi.Params{ansi.Param(105)}, // bright magenta bg -> ansi 13
			check: func(t *testing.T, got uv.Style) {
				if !colorEqualRGBA(t, got.Bg, ansiColor(13)) {
					t.Errorf("expected ansi color 13 bg, got %#v", got.Bg)
				}
			},
		},
		{
			name:   "38;5;n sets 256-color foreground",
			params: ansi.Params{ansi.Param(38), ansi.Param(5), ansi.Param(200)},
			check: func(t *testing.T, got uv.Style) {
				if !colorEqualRGBA(t, got.Fg, ansiColor(200)) {
					t.Errorf("expected ansi color 200, got %#v", got.Fg)
				}
			},
		},
		{
			name:   "38;2;r;g;b sets truecolor foreground",
			params: ansi.Params{ansi.Param(38), ansi.Param(2), ansi.Param(10), ansi.Param(20), ansi.Param(30)},
			check: func(t *testing.T, got uv.Style) {
				if !colorEqualRGBA(t, got.Fg, rgbColorVal{10, 20, 30}) {
					t.Errorf("expected rgb(10,20,30), got %#v", got.Fg)
				}
			},
		},
		{
			name:   "48;5;n sets 256-color background",
			params: ansi.Params{ansi.Param(48), ansi.Param(5), ansi.Param(123)},
			check: func(t *testing.T, got uv.Style) {
				if !colorEqualRGBA(t, got.Bg, ansiColor(123)) {
					t.Errorf("expected ansi color 123 bg, got %#v", got.Bg)
				}
			},
		},
		{
			name:   "48;2;r;g;b sets truecolor background",
			params: ansi.Params{ansi.Param(48), ansi.Param(2), ansi.Param(1), ansi.Param(2), ansi.Param(3)},
			check: func(t *testing.T, got uv.Style) {
				if !colorEqualRGBA(t, got.Bg, rgbColorVal{1, 2, 3}) {
					t.Errorf("expected rgb(1,2,3) bg, got %#v", got.Bg)
				}
			},
		},
		{
			name:   "38 with truncated 256-color params is ignored safely",
			params: ansi.Params{ansi.Param(38), ansi.Param(5)}, // missing index
			check: func(t *testing.T, got uv.Style) {
				if got.Fg != nil {
					t.Errorf("expected no fg from truncated params, got %#v", got.Fg)
				}
			},
		},
		{
			name:   "38;2 with truncated rgb params is ignored safely",
			params: ansi.Params{ansi.Param(38), ansi.Param(2), ansi.Param(10)}, // missing g,b
			check: func(t *testing.T, got uv.Style) {
				if got.Fg != nil {
					t.Errorf("expected no fg from truncated rgb params, got %#v", got.Fg)
				}
			},
		},
		{
			name:   "unknown param is a no-op",
			start:  bold,
			params: ansi.Params{ansi.Param(73)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&uv.AttrBold == 0 {
					t.Errorf("expected bold preserved through unknown param, attrs = %d", got.Attrs)
				}
			},
		},
		{
			name:   "compound params apply in sequence",
			params: ansi.Params{ansi.Param(1), ansi.Param(3), ansi.Param(31)},
			check: func(t *testing.T, got uv.Style) {
				if got.Attrs&uv.AttrBold == 0 || got.Attrs&uv.AttrItalic == 0 {
					t.Errorf("expected bold+italic, attrs = %d", got.Attrs)
				}
				if !colorEqualRGBA(t, got.Fg, ansiColor(1)) {
					t.Errorf("expected red fg, got %#v", got.Fg)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applySGR(tt.start, tt.params)
			tt.check(t, got)
		})
	}
}
