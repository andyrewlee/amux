package vterm

func (p *Parser) parseOSC(b byte) {
	// BEL and the 8-bit C1 ST both terminate OSC; a producer in 8-bit control
	// mode emits 0x9c instead of ESC \.
	if b == 0x07 || b == 0x9c {
		p.executeOSC()
		p.oscBuf.Reset()
		p.state = stateGround
		return
	}
	if b == 0x1b {
		p.state = stateOSCEscape
		return
	}
	if p.oscBuf.Len() >= maxOSCSequenceBytes {
		p.oscBuf.Reset()
		p.state = stateOSCIgnore
		return
	}
	p.oscBuf.WriteByte(b)
}

func (p *Parser) parseOSCEscape(b byte) {
	if b == '\\' {
		p.executeOSC()
		p.oscBuf.Reset()
		p.state = stateGround
		return
	}
	p.oscBuf.Reset()
	if b == 0x1b {
		p.state = stateEscape
		return
	}
	p.state = stateEscape
	p.parseEscape(b)
}

func (p *Parser) executeOSC() {
	p.dispatchOSC()
}

func (p *Parser) parseOSCIgnore(b byte) {
	switch b {
	case 0x07, 0x9c:
		p.state = stateGround
	case 0x1b:
		p.state = stateOSCIgnoreEscape
	}
}

func (p *Parser) parseOSCIgnoreEscape(b byte) {
	if b == '\\' {
		p.state = stateGround
		return
	}
	if b == 0x1b {
		p.state = stateEscape
		return
	}
	p.state = stateEscape
	p.parseEscape(b)
}

func (p *Parser) parseDCS(b byte) {
	// DCS sequences - ignore
	if b == 0x1b {
		p.state = stateDCSEscape
		return
	}
	// The 8-bit C1 ST also terminates DCS — without it a 0x9c-terminated
	// sequence swallows every subsequent byte.
	if b == 0x9c {
		p.state = stateGround
		return
	}
	// Stay in DCS until we see ESC \ or C1 ST
}

func (p *Parser) parseDCSEscape(b byte) {
	if b == '\\' {
		p.state = stateGround
		return
	}
	if b == 0x1b {
		p.state = stateDCSEscape
		return
	}
	p.state = stateDCS
}
