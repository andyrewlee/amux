package compositor

import uv "github.com/charmbracelet/ultraviolet"

// BorderRunes describes the rune set used to draw a border.
type BorderRunes struct {
	TopLeft     rune
	TopRight    rune
	BottomLeft  rune
	BottomRight rune
	Horizontal  rune
	Vertical    rune
}

// BorderDrawable draws pane borders directly as cells without ANSI parsing.
type BorderDrawable struct {
	x, y          int
	width, height int
	style         uv.Style
	runes         BorderRunes
}

var _ uv.Drawable = (*BorderDrawable)(nil)

// NewBorderDrawable creates a border drawable.
func NewBorderDrawable(x, y, width, height int, style uv.Style, runes BorderRunes) *BorderDrawable {
	return &BorderDrawable{
		x:      x,
		y:      y,
		width:  width,
		height: height,
		style:  style,
		runes:  runes,
	}
}

func (d *BorderDrawable) Draw(screen uv.Screen, r uv.Rectangle) {
	if d == nil || d.width < 2 || d.height < 2 {
		return
	}

	// Top and bottom edges.
	for x := 0; x < d.width; x++ {
		topRune := d.runes.Horizontal
		bottomRune := d.runes.Horizontal
		if x == 0 {
			topRune = d.runes.TopLeft
			bottomRune = d.runes.BottomLeft
		} else if x == d.width-1 {
			topRune = d.runes.TopRight
			bottomRune = d.runes.BottomRight
		}
		d.drawCell(screen, r, d.x+x, d.y, topRune)
		d.drawCell(screen, r, d.x+x, d.y+d.height-1, bottomRune)
	}

	// Vertical edges.
	for y := 1; y < d.height-1; y++ {
		d.drawCell(screen, r, d.x, d.y+y, d.runes.Vertical)
		d.drawCell(screen, r, d.x+d.width-1, d.y+y, d.runes.Vertical)
	}
}

func (d *BorderDrawable) drawCell(screen uv.Screen, r uv.Rectangle, x, y int, ch rune) {
	if x < r.Min.X || x >= r.Max.X || y < r.Min.Y || y >= r.Max.Y {
		return
	}
	cell := getCell()
	cell.Content = runeToString(ch)
	cell.Width = 1
	cell.Style = d.style
	screen.SetCell(x, y, cell)
	putCell(cell)
}
