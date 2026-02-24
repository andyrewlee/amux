package common

import (
	"bytes"

	"github.com/charmbracelet/x/ansi"
)

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

// DrainKnownPTYNoiseTrailing flushes any carried line fragment at stream end.
func DrainKnownPTYNoiseTrailing(trailing *[]byte) []byte {
	if trailing == nil || len(*trailing) == 0 {
		return nil
	}
	out := append([]byte(nil), (*trailing)...)
	*trailing = (*trailing)[:0]
	return out
}

// FilterKnownPTYNoiseStream filters chunked PTY output while carrying a possible
// trailing diagnostic fragment between chunks so split lines can be removed.
func FilterKnownPTYNoiseStream(data []byte, trailing *[]byte) []byte {
	if trailing == nil {
		return FilterKnownPTYNoise(data)
	}

	carried := 0
	if len(*trailing) > 0 {
		carried = len(*trailing)
		combined := make([]byte, 0, carried+len(data))
		combined = append(combined, *trailing...)
		combined = append(combined, data...)
		*trailing = (*trailing)[:0]
		data = combined
	}

	if len(data) == 0 {
		return nil
	}

	// Preserve the zero-allocation fast path when we have no carry and no chance
	// of a malloc diagnostic in the current chunk.
	if carried == 0 && !mightContainMallocDiagnostic(data) {
		lastNewline := bytes.LastIndexByte(data, '\n')
		trailingLine := data
		if lastNewline >= 0 {
			trailingLine = data[lastNewline+1:]
		}
		if shouldBufferPotentialDiagnosticFragment(trailingLine) {
			*trailing = append((*trailing)[:0], trailingLine...)
			if lastNewline >= 0 {
				return data[:lastNewline+1]
			}
			return []byte{}
		}
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
		if !hasNewline {
			if shouldBufferPotentialDiagnosticFragment(line) {
				*trailing = append((*trailing)[:0], line...)
			} else {
				out = append(out, line...)
			}
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

	// If this call had no carry and no filtering/holding, preserve caller's
	// original slice to avoid needless allocation and copies.
	if carried == 0 && !removed && len(*trailing) == 0 && len(out) == len(data) {
		return data
	}
	if len(out) == 0 {
		return []byte{}
	}
	return out
}

func mightContainMallocDiagnostic(data []byte) bool {
	return containsASCIIFold(data, "malloc")
}

func isMacOSMallocDiagnosticLine(line []byte) bool {
	if bytes.IndexByte(line, 0x1b) >= 0 {
		line = []byte(ansi.Strip(string(line)))
	}
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
	if len(token) == 0 {
		return false
	}
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

func containsASCIIFold(haystack []byte, needle string) bool {
	n := len(needle)
	if n == 0 {
		return true
	}
	if len(haystack) < n {
		return false
	}
	for i := 0; i <= len(haystack)-n; i++ {
		matched := true
		for j := 0; j < n; j++ {
			if toLowerASCII(haystack[i+j]) != needle[j] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func toLowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func shouldBufferPotentialDiagnosticFragment(line []byte) bool {
	if len(line) == 0 {
		return false
	}
	if bytes.IndexByte(line, 0x1b) >= 0 {
		line = []byte(ansi.Strip(string(line)))
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return false
	}

	open := bytes.IndexByte(line, '(')
	if open <= 0 {
		return false
	}
	proc := line[:open]
	if !isProcessToken(proc) {
		return false
	}

	rest := line[open+1:]
	closeRel := bytes.IndexByte(rest, ')')
	if closeRel < 0 {
		return isPotentialPIDPrefix(rest)
	}

	inside := rest[:closeRel]
	if !startsWithPID(inside) {
		return false
	}

	after := bytes.TrimSpace(rest[closeRel+1:])
	if len(after) == 0 {
		return true
	}
	return isMallocTokenPrefix(after)
}

func isPotentialPIDPrefix(token []byte) bool {
	if len(token) == 0 {
		return true
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
	if token[i] != ',' {
		return false
	}
	if i == len(token)-1 {
		return true
	}
	for j := i + 1; j < len(token); j++ {
		b := token[j]
		switch {
		case b >= '0' && b <= '9':
		case b >= 'a' && b <= 'f':
		case b >= 'A' && b <= 'F':
		case b == 'x' || b == 'X':
		default:
			return false
		}
	}
	return true
}

func isMallocTokenPrefix(rest []byte) bool {
	const mallocWord = "malloc"
	n := len(rest)
	if n <= len(mallocWord) {
		for i := 0; i < n; i++ {
			if toLowerASCII(rest[i]) != mallocWord[i] {
				return false
			}
		}
		return true
	}
	for i := 0; i < len(mallocWord); i++ {
		if toLowerASCII(rest[i]) != mallocWord[i] {
			return false
		}
	}
	next := rest[len(mallocWord)]
	return next == ':' || next == ' ' || next == '\t'
}
