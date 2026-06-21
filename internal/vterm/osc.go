package vterm

import (
	"encoding/base64"
	"strings"
)

// dispatchOSC parses a buffered OSC payload (the bytes between "ESC ]" and the
// terminator) and applies recognized commands to the VTerm. Unrecognized or
// malformed sequences are ignored.
func (p *Parser) dispatchOSC() {
	payload := p.oscBuf.String()
	cmd, rest, ok := strings.Cut(payload, ";")
	if !ok {
		return
	}
	switch cmd {
	case "0", "1", "2": // window / icon / tab title
		p.vt.setOSCTitle(rest)
	case "7": // working directory
		p.vt.setOSCWorkingDir(rest)
	case "52": // clipboard: <selection>;<base64-or-?>
		_, data, ok := strings.Cut(rest, ";")
		if !ok || data == "?" {
			return // query or malformed — ignore
		}
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return
		}
		p.vt.setPendingClipboard(decoded)
	}
}
