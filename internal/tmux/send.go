package tmux

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Enter-key pacing for text sends.
//
// TUI agents process typed characters asynchronously. If Enter arrives before
// the UI has applied all bytes, the command may execute while trailing text
// remains in the input box. We scale a small delay by text length so longer
// prompts (for example /review commands) get more settle time.
const (
	enterDelayBase       = 80 * time.Millisecond
	enterDelayPerRune    = 1 * time.Millisecond
	enterDelayMaxExtra   = 600 * time.Millisecond
	enterDelayMaxTotal   = 700 * time.Millisecond
	enterDelayMinNonZero = 120 * time.Millisecond

	// For long prompts, wait briefly until the typed text appears in pane output
	// before sending Enter. This avoids Enter racing ahead of text insertion.
	enterEchoProbeMinRunes = 48
	enterEchoProbeTimeout  = 2 * time.Second
	enterEchoProbeInterval = 50 * time.Millisecond
	enterEchoProbeLines    = 60
)

func enterSendDelay(text string) time.Duration {
	if text == "" {
		return enterDelayBase
	}
	runes := utf8.RuneCountInString(text)
	if runes < 0 {
		runes = 0
	}
	extra := time.Duration(runes) * enterDelayPerRune
	if extra > enterDelayMaxExtra {
		extra = enterDelayMaxExtra
	}
	delay := enterDelayBase + extra
	if delay < enterDelayMinNonZero {
		delay = enterDelayMinNonZero
	}
	if delay > enterDelayMaxTotal {
		delay = enterDelayMaxTotal
	}
	return delay
}

func shouldProbeEnterEcho(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	// Slash commands are common in coding TUIs and are sensitive to Enter races.
	if strings.HasPrefix(trimmed, "/") && utf8.RuneCountInString(trimmed) >= 8 {
		return true
	}
	// Multi-line sends should always wait for echo before Enter.
	if strings.Contains(trimmed, "\n") {
		return true
	}
	return utf8.RuneCountInString(trimmed) >= enterEchoProbeMinRunes
}

func waitForEnterEcho(sessionName, text string, opts Options) {
	target := normalizeEchoText(text)
	if target == "" {
		return
	}

	deadline := time.Now().Add(enterEchoProbeTimeout)
	for time.Now().Before(deadline) {
		capture, ok := CapturePaneTail(sessionName, enterEchoProbeLines, opts)
		if ok && echoContainsTargetNearEnd(capture, target) {
			return
		}
		time.Sleep(enterEchoProbeInterval)
	}
}

func normalizeEchoText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if prevSpace {
				continue
			}
			b.WriteByte(' ')
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

func echoContainsTargetNearEnd(capture, target string) bool {
	if target == "" {
		return false
	}
	normCapture := normalizeEchoText(capture)
	if normCapture == "" {
		return false
	}

	// Focus on tail content so old history matches do not trigger early.
	maxTail := len(target)*4 + 256
	if maxTail < 512 {
		maxTail = 512
	}
	if len(normCapture) > maxTail {
		normCapture = normCapture[len(normCapture)-maxTail:]
	}
	return strings.Contains(normCapture, target)
}

// SendKeys sends text to a tmux session via send-keys.
// If enter is true, an Enter key is sent after the text.
func SendKeys(sessionName, text string, enter bool, opts Options) error {
	if sessionName == "" {
		return nil
	}
	if err := EnsureAvailable(); err != nil {
		return err
	}

	args := []string{"send-keys", "-l", "-t", sessionName, "--", text}
	cmd, cancel := tmuxCommand(opts, args...)
	defer cancel()
	if err := cmd.Run(); err != nil {
		return err
	}

	if enter {
		if shouldProbeEnterEcho(text) {
			waitForEnterEcho(sessionName, text, opts)
		}

		// Brief pause so the TUI finishes processing the text insertion
		// before we deliver the carriage return.
		time.Sleep(enterSendDelay(text))

		// Use -H 0D (hex carriage return) instead of the named "Enter" key.
		// Some TUI agents (e.g. Cline) use raw terminal mode where the named
		// Enter key is dropped, but the raw CR byte is always delivered.
		enterCmd, enterCancel := tmuxCommand(opts, "send-keys", "-H", "-t", sessionName, "0D")
		defer enterCancel()
		return enterCmd.Run()
	}
	return nil
}

// SendInterrupt sends Ctrl-C to a tmux session.
func SendInterrupt(sessionName string, opts Options) error {
	if sessionName == "" {
		return nil
	}
	if err := EnsureAvailable(); err != nil {
		return err
	}
	cmd, cancel := tmuxCommand(opts, "send-keys", "-t", sessionName, "C-c")
	defer cancel()
	return cmd.Run()
}
