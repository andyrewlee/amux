package app

import (
	"os"
	"strings"
)

func setEnvIfNonEmpty(key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	_ = os.Setenv(key, value)
}
