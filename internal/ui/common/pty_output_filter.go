package common

import "bytes"

// FilterKnownPTYNoise removes known host-runtime diagnostic lines that should
// not be rendered inside interactive agent prompts.
func FilterKnownPTYNoise(data []byte) []byte {
	if len(data) == 0 || !mightContainMallocDiagnostic(data) {
		return data
	}

	out := make([]byte, 0, len(data))
	removed := false
	start := 0

	for start < len(data) {
		rel := bytes.IndexByte(data[start:], '\n')
		end := len(data)
		hasNewline := false
		if rel >= 0 {
			end = start + rel
			hasNewline = true
		}

		line := data[start:end]
		// PTY chunks may end mid-line; avoid filtering incomplete trailing
		// fragments because the remainder may arrive in a future flush.
		if !hasNewline {
			out = append(out, line...)
			break
		}
		trimmed := bytes.TrimRight(line, "\r")
		if isMacOSMallocDiagnosticLine(trimmed) {
			removed = true
		} else {
			out = append(out, line...)
			out = append(out, '\n')
		}

		start = end + 1
	}

	if !removed {
		return data
	}
	return out
}

func mightContainMallocDiagnostic(data []byte) bool {
	return bytes.Contains(data, []byte("malloc")) ||
		bytes.Contains(data, []byte("Malloc")) ||
		bytes.Contains(data, []byte("MALLOC"))
}

func isMacOSMallocDiagnosticLine(line []byte) bool {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return false
	}

	open := bytes.IndexByte(line, '(')
	if open <= 0 {
		return false
	}
	closeIdx := bytes.IndexByte(line, ')')
	if closeIdx <= open+1 {
		return false
	}

	proc := line[:open]
	if !isProcessToken(proc) {
		return false
	}

	inside := line[open+1 : closeIdx]
	if !startsWithPID(inside) {
		return false
	}

	rest := bytes.TrimSpace(line[closeIdx+1:])
	if len(rest) < len("malloc") {
		return false
	}
	if !bytes.EqualFold(rest[:len("malloc")], []byte("malloc")) {
		return false
	}
	if len(rest) > len("malloc") {
		next := rest[len("malloc")]
		if next != ':' && next != ' ' && next != '\t' {
			return false
		}
	}

	return true
}

func isProcessToken(token []byte) bool {
	for _, b := range token {
		switch {
		case b >= 'a' && b <= 'z':
		case b >= 'A' && b <= 'Z':
		case b >= '0' && b <= '9':
		case b == '_' || b == '-' || b == '.':
		default:
			return false
		}
	}
	return true
}

func startsWithPID(token []byte) bool {
	if len(token) == 0 {
		return false
	}
	i := 0
	for i < len(token) && token[i] >= '0' && token[i] <= '9' {
		i++
	}
	if i == 0 {
		return false
	}
	if i == len(token) {
		return true
	}
	return token[i] == ','
}
