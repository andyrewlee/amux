package vterm

import (
	"encoding/base64"
	"strings"
)

const (
	maxOSCMetadataBytes    = 4 * 1024
	maxOSC52ClipboardBytes = 64 * 1024
	maxOSCSequenceBytes    = ((maxOSC52ClipboardBytes + 2) / 3 * 4) + 1024
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
		if len(rest) > maxOSCMetadataBytes {
			return
		}
		p.vt.setOSCTitle(rest)
	case "7": // working directory
		if len(rest) > maxOSCMetadataBytes {
			return
		}
		p.vt.setOSCWorkingDir(rest)
	case "52": // clipboard: <selection>;<base64-or-?>
		_, data, ok := strings.Cut(rest, ";")
		if !ok || data == "?" {
			return // query or malformed — ignore
		}
		if base64.StdEncoding.DecodedLen(len(data)) > maxOSC52ClipboardBytes {
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return
		}
		if len(decoded) > maxOSC52ClipboardBytes {
			return
		}
		p.vt.setPendingClipboard(decoded)
	}
}
