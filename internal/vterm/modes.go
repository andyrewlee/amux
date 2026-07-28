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
			if p.vt.IgnoreCursorVisibilityControls {
				// Keep cursor visibility fixed for hosts that emit frequent
				// hide/show toggles during streaming output.
				continue
			}
			hidden := !set
			prevHidden := p.vt.CursorHiddenForRender()
			p.vt.CursorHidden = hidden
			if prevHidden != p.vt.CursorHiddenForRender() {
				p.vt.bumpVersion()
			}
		case 47, 1047, 1049: // Alternate screen buffer
			p.vt.setAltScreenMode(param, set)
		case 2026: // Synchronized output
			p.vt.setSynchronizedOutput(set)
		case 2004: // Bracketed paste mode
			// Ignore
		case 1000, 1002, 1003: // XTerm mouse reporting modes
			p.vt.setMouseTrackingMode(param, set)
		case 1006: // SGR extended mouse coordinates
			p.vt.mouseSGRMode = set
		}
	}
}

// setAltScreenMode applies one of the three alternate-screen private modes.
// In a real terminal they differ along two axes — whether the cursor is saved
// and restored across the switch, and whether the alternate buffer is cleared:
//
//	47    switch buffers only.
//	1047  switch buffers; clear the alternate screen on the way out.
//	1049  save the cursor, switch, and clear; restore the cursor on the way out.
//
// Only the cursor axis is a real distinction here, and it is the one that
// matters: 1049 is what essentially every modern full-screen application uses
// precisely because it is the one that puts the cursor back. Treating all three
// alike — as this did — silently gave 47 and 1047 a guarantee they do not make.
//
// The clearing axis collapses because amux does not keep a persistent alternate
// buffer: exitAltScreen discards it outright and enterAltScreen always hands out
// a freshly blanked one. There is therefore no stale frame a 1047 exit-clear
// could remove, which makes 47 and 1047 identical in this emulator. That costs
// nothing an application can observe — the difference between them is only
// visible to one that re-enters the alternate screen expecting its previous
// contents to still be there, which amux never provides for any of the three.
func (v *VTerm) setAltScreenMode(mode int, set bool) {
	const modeWithCursorSave = 1049
	savesCursor := mode == modeWithCursorSave

	if set {
		v.enterAltScreen(savesCursor)
		return
	}
	v.exitAltScreen(savesCursor)
}

func (v *VTerm) setMouseTrackingMode(mode int, enabled bool) {
	if enabled {
		v.mouseTrackingMode = mode
		return
	}
	if v.mouseTrackingMode == mode {
		v.mouseTrackingMode = 0
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
