package common

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/andyrewlee/amux/internal/logging"
)

const (
	OSC52ClipboardEnv      = "AMUX_ENABLE_OSC52_CLIPBOARD"
	OSC52ClipboardMaxBytes = 64 * 1024
)

// OSC52ClipboardText returns text that is allowed to be copied from an OSC 52
// terminal sequence. OSC 52 is disabled by default because terminal output is an
// untrusted boundary; enable with AMUX_ENABLE_OSC52_CLIPBOARD=1.
func OSC52ClipboardText(payload []byte) (string, bool) {
	if len(payload) == 0 {
		return "", false
	}
	if os.Getenv(OSC52ClipboardEnv) != "1" {
		return "", false
	}
	if len(payload) > OSC52ClipboardMaxBytes {
		logging.Warn("Ignoring OSC 52 clipboard payload of %d bytes (max %d)", len(payload), OSC52ClipboardMaxBytes)
		return "", false
	}
	return string(payload), true
}

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

// clipboardTool is a candidate external clipboard command. The table IS the
// documentation of supported clipboard paths — keep it accurate when adding
// tools (e.g. termux-clipboard-set if termux support ever matters).
//
// Order is preference, not detection: Wayland (wl-copy) then X11 (xclip,
// xsel), then pbcopy/clip so the fallback also reaches odd environments —
// pbcopy via compatibility layers off-darwin, clip under WSL/Git-Bash Windows.
// Session-aware ordering ($WAYLAND_DISPLAY vs X11) can refine this if it ever
// matters.
var clipboardTools = []struct {
	name string
	args []string
}{
	{name: "wl-copy"},
	{name: "xclip", args: []string{"-selection", "clipboard"}},
	{name: "xsel", args: []string{"--clipboard", "--input"}},
	{name: "pbcopy"},
	{name: "clip"},
}

// Test seams — no real clipboard tool is exec'd from tests.
var (
	clipboardToolExists = func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	}
	runClipboardTool = func(name string, args []string, text string) error {
		cmd := exec.Command(name, args...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
)

// CopyToClipboard writes text to the system clipboard. macOS prefers pbcopy;
// other platforms (or a failed pbcopy) fall back to the first available tool
// in clipboardTools — the same set atotto/clipboard shelled out to, without
// the unmaintained dependency.
func CopyToClipboard(text string) error {
	// Prioritize pbcopy on macOS as it is more reliable in various environments.
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	return copyWithTool(text)
}

func copyWithTool(text string) error {
	var ran []string
	var lastErr error
	for _, tool := range clipboardTools {
		if !clipboardToolExists(tool.name) {
			continue
		}
		ran = append(ran, tool.name)
		if err := runClipboardTool(tool.name, tool.args, text); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if len(ran) == 0 {
		names := make([]string, len(clipboardTools))
		for i, tool := range clipboardTools {
			names[i] = tool.name
		}
		return fmt.Errorf("no clipboard tool found on PATH (checked: %s)", strings.Join(names, ", "))
	}
	return fmt.Errorf("clipboard tools failed (tried: %s): %w", strings.Join(ran, ", "), lastErr)
}
