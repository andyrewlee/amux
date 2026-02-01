package app

import (
	"os"
	"strings"
)

func setEnvOrUnset(key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		_ = os.Unsetenv(key)
		return
	}
	_ = os.Setenv(key, value)
}

func setEnvIfNonEmpty(key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	_ = os.Setenv(key, value)
}
