package cli

import (
	"regexp"
	"strings"
)

var safeShellValue = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./:-]+$`)

func shellQuoteCommandValue(value string) string {
	if value == "" {
		return "''"
	}
	if safeShellValue.MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
