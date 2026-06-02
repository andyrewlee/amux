package common

import (
	"os/exec"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"

	"github.com/andyrewlee/amux/internal/logging"
)

// CopyToClipboardWithLog copies text to the clipboard (a no-op for empty text),
// logging success or failure with label for context. It shells out to pbcopy on
// macOS, so callers MUST NOT hold a tab/terminal mutex while calling it — capture
// the text under the lock, release it, then call this.
func CopyToClipboardWithLog(text, label string) {
	if text == "" {
		return
	}
	if err := CopyToClipboard(text); err != nil {
		logging.Error("Failed to copy %s: %v", label, err)
		return
	}
	logging.Info("Copied %d chars (%s)", len(text), label)
}

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
