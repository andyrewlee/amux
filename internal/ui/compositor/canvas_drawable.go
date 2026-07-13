package compositor

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// parsedCell is a single decoded grapheme from a StringDrawable's content,
// ready to be blitted onto the screen without re-parsing ANSI sequences.
type parsedCell struct {
	content string
	style   uv.Style
	width   int
}

// StringDrawable wraps a styled ANSI string to implement uv.Drawable.
// This allows string-based content to be composed onto a lipgloss.Canvas.
type StringDrawable struct {
	content string
	x, y    int
	width   int
	height  int
	lines   []string
	cells   [][]parsedCell
}

// Ensure StringDrawable implements uv.Drawable
var _ uv.Drawable = (*StringDrawable)(nil)

// NewStringDrawable creates a drawable from a styled string at the given position.
//
// ANSI decoding (SGR styling + grapheme segmentation) happens once here
// rather than on every Draw call, since content is immutable per instance
// and drawableCache/borderCache reuse the same *StringDrawable across frames
// for byte-identical content.
func NewStringDrawable(content string, x, y int) *StringDrawable {
	lines := strings.Split(content, "\n")
	width := 0
	totalWidth := 0
	for _, line := range lines {
		w := ansi.StringWidth(line)
		totalWidth += w
		if w > width {
			width = w
		}
	}

	d := &StringDrawable{
		content: content,
		x:       x,
		y:       y,
		width:   width,
		height:  len(lines),
		lines:   lines,
	}

	if content == "" {
		return d
	}

	p := ansi.GetParser()
	defer ansi.PutParser(p)

	// Total cell count across all lines never exceeds the sum of display
	// widths (every grapheme is at least 1 column wide), so totalWidth gives
	// an exact-or-over capacity bound. Cells for all lines share one flat
	// backing array, sized once, to keep allocations to a small constant
	// number instead of one growing slice per line.
	cells := make([][]parsedCell, len(lines))
	flat := make([]parsedCell, 0, totalWidth)
	var style uv.Style
	var state byte
	for lineIdx, line := range lines {
		start := len(flat)
		for len(line) > 0 {
			seq, w, n, newState := ansi.DecodeSequence(line, state, p)
			if n == 0 {
				break
			}

			if w == 0 {
				// Control sequence - check for SGR
				cmd := ansi.Cmd(p.Command())
				if cmd.Final() == 'm' {
					style = applySGR(style, p.Params())
				}
			} else {
				// Printable grapheme
				flat = append(flat, parsedCell{content: seq, style: style, width: w})
			}

			line = line[n:]
			state = newState
		}
		cells[lineIdx] = flat[start:len(flat):len(flat)]
	}
	d.cells = cells

	return d
}

// Draw renders the string onto the screen buffer by blitting the cells
// parsed once in NewStringDrawable.
func (d *StringDrawable) Draw(screen uv.Screen, r uv.Rectangle) {
	if d.content == "" {
		return
	}

	for lineIdx, lineCells := range d.cells {
		screenY := d.y + lineIdx
		if screenY < r.Min.Y || screenY >= r.Max.Y {
			continue
		}

		screenX := d.x
		for _, pc := range lineCells {
			if screenX >= r.Min.X && screenX < r.Max.X {
				cell := getCell()
				cell.Content = pc.content
				cell.Style = pc.style
				cell.Width = pc.width
				screen.SetCell(screenX, screenY, cell)
				putCell(cell)
			}
			screenX += pc.width
		}
	}
}

// applySGR updates the style based on SGR parameters.
func applySGR(style uv.Style, params ansi.Params) uv.Style {
	if len(params) == 0 {
		return uv.Style{}
	}

	for i := 0; i < len(params); i++ {
		p, _, _ := params.Param(i, 0)
		switch {
		case p == 0:
			style = uv.Style{}
		case p == 1:
			style.Attrs |= uv.AttrBold
		case p == 2:
			style.Attrs |= uv.AttrFaint
		case p == 3:
			style.Attrs |= uv.AttrItalic
		case p == 4:
			style.Underline = uv.UnderlineSingle
		case p == 5:
			style.Attrs |= uv.AttrBlink
		case p == 7:
			style.Attrs |= uv.AttrReverse
		case p == 8:
			style.Attrs |= uv.AttrConceal
		case p == 9:
			style.Attrs |= uv.AttrStrikethrough
		case p == 22:
			style.Attrs &^= (uv.AttrBold | uv.AttrFaint)
		case p == 23:
			style.Attrs &^= uv.AttrItalic
		case p == 24:
			style.Underline = uv.UnderlineNone
		case p == 25:
			style.Attrs &^= uv.AttrBlink
		case p == 27:
			style.Attrs &^= uv.AttrReverse
		case p == 28:
			style.Attrs &^= uv.AttrConceal
		case p == 29:
			style.Attrs &^= uv.AttrStrikethrough
		case p >= 30 && p <= 37:
			style.Fg = ansiColor(p - 30)
		case p == 38:
			// Extended foreground color
			if i+2 < len(params) {
				mode, _, _ := params.Param(i+1, 0)
				if mode == 5 {
					idx, _, _ := params.Param(i+2, 0)
					style.Fg = sgrIndexedColor(idx)
					i += 2
				} else if mode == 2 && i+4 < len(params) {
					rv, _, _ := params.Param(i+2, 0)
					gv, _, _ := params.Param(i+3, 0)
					bv, _, _ := params.Param(i+4, 0)
					style.Fg = rgbColorVal{sgrColorComponent(rv), sgrColorComponent(gv), sgrColorComponent(bv)}
					i += 4
				}
			}
		case p == 39:
			style.Fg = nil
		case p >= 40 && p <= 47:
			style.Bg = ansiColor(p - 40)
		case p == 48:
			// Extended background color
			if i+2 < len(params) {
				mode, _, _ := params.Param(i+1, 0)
				if mode == 5 {
					idx, _, _ := params.Param(i+2, 0)
					style.Bg = sgrIndexedColor(idx)
					i += 2
				} else if mode == 2 && i+4 < len(params) {
					rv, _, _ := params.Param(i+2, 0)
					gv, _, _ := params.Param(i+3, 0)
					bv, _, _ := params.Param(i+4, 0)
					style.Bg = rgbColorVal{sgrColorComponent(rv), sgrColorComponent(gv), sgrColorComponent(bv)}
					i += 4
				}
			}
		case p == 49:
			style.Bg = nil
		case p >= 90 && p <= 97:
			style.Fg = ansiColor(p - 90 + 8)
		case p >= 100 && p <= 107:
			style.Bg = ansiColor(p - 100 + 8)
		}
	}
	return style
}

func sgrIndexedColor(v int) ansiColor {
	return ansiColor(sgrColorIndex(v))
}

func sgrColorIndex(v int) uint32 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint32(v)
}

func sgrColorComponent(v int) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v)
}

// rgbColorVal is an RGB color value.
type rgbColorVal [3]uint8

func (c rgbColorVal) RGBA() (r, g, b, a uint32) {
	return uint32(c[0]) * 257, uint32(c[1]) * 257, uint32(c[2]) * 257, 65535
}
