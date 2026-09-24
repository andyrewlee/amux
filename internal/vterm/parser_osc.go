package vterm

func (p *Parser) parseOSC(b byte) {
	if b == 0x07 {
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
	case 0x07:
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
	// Stay in DCS until we see ESC \
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
