package vterm

import (
	"strconv"
	"strings"
)

// ContainsSGRParam reports whether s contains an SGR escape sequence (CSI ... m)
// carrying the given numeric parameter. vterm owns ANSI/SGR scanning, so tests
// in any package that renders via vterm can share this instead of re-deriving it.
func ContainsSGRParam(s string, target int) bool {
	targetStr := strconv.Itoa(target)
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b || i+1 >= len(s) || s[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(s) && s[j] != 'm' {
			j++
		}
		if j >= len(s) {
			break
		}
		params := strings.Split(s[i+2:j], ";")
		for _, param := range params {
			if param == targetStr {
				return true
			}
		}
		i = j
	}
	return false
}
