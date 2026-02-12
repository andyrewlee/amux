package vterm

import "fmt"

func (p *Parser) executeDSR() {
	if len(p.params) == 0 {
		return
	}

	switch p.params[0] {
	case 5: // Status report - respond "OK"
		p.vt.respond([]byte("\x1b[0n"))
	case 6: // Cursor position report
		// Response: ESC [ row ; col R (1-indexed)
		row := p.vt.CursorY + 1
		col := p.vt.CursorX + 1
		response := fmt.Sprintf("\x1b[%d;%dR", row, col)
		p.vt.respond([]byte(response))
	}
}

func (p *Parser) executeMode(set bool) {
	if p.intermediate != '?' {
		return
	}

	for _, param := range p.params {
		switch param {
		case 6: // DECOM - origin mode
			p.vt.OriginMode = set
			p.vt.CursorX = 0
			if set {
				p.vt.CursorY = p.vt.ScrollTop
			} else {
				p.vt.CursorY = 0
			}
			p.vt.clampCursor()
		case 1: // DECCKM - cursor keys mode
			// Ignore
		case 7: // DECAWM - auto-wrap mode
			// Always on
		case 12: // Blinking cursor
			// Ignore
		case 25: // DECTCEM - cursor visible
			hidden := !set
			prevHidden := p.vt.CursorHiddenForRender()
			p.vt.CursorHidden = hidden
			if prevHidden != p.vt.CursorHiddenForRender() {
				p.vt.bumpVersion()
			}
		case 47, 1047, 1049: // Alternate screen buffer
			if set {
				p.vt.enterAltScreen()
			} else {
				p.vt.exitAltScreen()
			}
		case 2026: // Synchronized output
			p.vt.setSynchronizedOutput(set)
		case 2004: // Bracketed paste mode
			// Ignore
		}
	}
}

func (p *Parser) executeDECRQM() {
	if len(p.params) == 0 {
		return
	}

	for _, param := range p.params {
		status := 0
		switch param {
		case 2026:
			if p.vt.syncActive {
				status = 1
			} else {
				status = 2
			}
		default:
			status = 0
		}
		response := fmt.Sprintf("\x1b[?%d;%d$y", param, status)
		p.vt.respond([]byte(response))
	}
}
