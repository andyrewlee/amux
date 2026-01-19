package vterm

func (p *Parser) executeSGR() {
	if len(p.params) == 0 {
		p.params = []int{0}
	}

	for i := 0; i < len(p.params); i++ {
		param := p.params[i]
		switch param {
		case 0: // Reset
			p.vt.CurrentStyle = Style{}
		case 1:
			p.vt.CurrentStyle.Bold = true
		case 2:
			p.vt.CurrentStyle.Dim = true
		case 3:
			p.vt.CurrentStyle.Italic = true
		case 4:
			p.vt.CurrentStyle.Underline = true
		case 5, 6:
			p.vt.CurrentStyle.Blink = true
		case 7:
			p.vt.CurrentStyle.Reverse = true
		case 8:
			p.vt.CurrentStyle.Hidden = true
		case 9:
			p.vt.CurrentStyle.Strike = true
		case 21:
			p.vt.CurrentStyle.Bold = false
		case 22:
			p.vt.CurrentStyle.Bold = false
			p.vt.CurrentStyle.Dim = false
		case 23:
			p.vt.CurrentStyle.Italic = false
		case 24:
			p.vt.CurrentStyle.Underline = false
		case 25:
			p.vt.CurrentStyle.Blink = false
		case 27:
			p.vt.CurrentStyle.Reverse = false
		case 28:
			p.vt.CurrentStyle.Hidden = false
		case 29:
			p.vt.CurrentStyle.Strike = false
		case 30, 31, 32, 33, 34, 35, 36, 37: // FG colors 0-7
			p.vt.CurrentStyle.Fg = Color{Type: ColorIndexed, Value: uint32(param - 30)}
		case 38: // Extended FG
			i = p.parseExtendedColor(i, &p.vt.CurrentStyle.Fg)
		case 39: // Default FG
			p.vt.CurrentStyle.Fg = Color{Type: ColorDefault}
		case 40, 41, 42, 43, 44, 45, 46, 47: // BG colors 0-7
			p.vt.CurrentStyle.Bg = Color{Type: ColorIndexed, Value: uint32(param - 40)}
		case 48: // Extended BG
			i = p.parseExtendedColor(i, &p.vt.CurrentStyle.Bg)
		case 49: // Default BG
			p.vt.CurrentStyle.Bg = Color{Type: ColorDefault}
		case 90, 91, 92, 93, 94, 95, 96, 97: // Bright FG
			p.vt.CurrentStyle.Fg = Color{Type: ColorIndexed, Value: uint32(param - 90 + 8)}
		case 100, 101, 102, 103, 104, 105, 106, 107: // Bright BG
			p.vt.CurrentStyle.Bg = Color{Type: ColorIndexed, Value: uint32(param - 100 + 8)}
		}
	}
}

func (p *Parser) parseExtendedColor(i int, color *Color) int {
	if i+1 >= len(p.params) {
		return i
	}

	switch p.params[i+1] {
	case 2: // RGB
		if i+4 < len(p.params) {
			r := p.params[i+2]
			g := p.params[i+3]
			b := p.params[i+4]
			color.Type = ColorRGB
			color.Value = uint32(r)<<16 | uint32(g)<<8 | uint32(b)
			return i + 4
		}
	case 5: // 256 color
		if i+2 < len(p.params) {
			color.Type = ColorIndexed
			color.Value = uint32(p.params[i+2])
			return i + 2
		}
	}
	return i + 1
}
