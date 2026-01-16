package common

import (
	"os/exec"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"
)

// CopyToClipboard writes text to the system clipboard with a macOS pbcopy fallback.
func CopyToClipboard(text string) error {
	// Prioritize pbcopy on macOS as it is more reliable in various environments.
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// Fallback to library for other OS or if pbcopy fails.
	return clipboard.WriteAll(text)
}
