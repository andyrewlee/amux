package cli

import (
	"fmt"
	"regexp"
	"strings"
)

var versionBannerChromeLineRegex = regexp.MustCompile(`^v0\.[0-9]+(?:\.[0-9]+){0,2}$`)

// compactAgentOutput strips known TUI chrome lines and collapses output to
// concise non-empty lines suitable for chat notifications.
func compactAgentOutput(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	dropPromptContinuation := false
	for _, raw := range lines {
		line := strings.TrimSpace(stripANSIEscape(raw))
		if line == "" {
			continue
		}
		if dropPromptContinuation {
			if isPromptContinuationLine(raw, line) {
				continue
			}
			dropPromptContinuation = false
		}
		if shouldDropAgentChromeLine(line) {
			if isPromptChromeLine(line) {
				dropPromptContinuation = true
			}
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func stripANSIEscape(line string) string {
	if !strings.Contains(line, "\x1b") {
		return line
	}
	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] != 0x1b {
			b.WriteByte(line[i])
			i++
			continue
		}
		i++ // Skip ESC
		if i >= len(line) {
			break
		}
		switch line[i] {
		case '[':
			// CSI sequence: ESC [ ... final-byte
			i++
			for i < len(line) {
				c := line[i]
				i++
				if c >= '@' && c <= '~' {
					break
				}
			}
		case ']':
			// OSC sequence: ESC ] ... (BEL | ESC \)
			i++
			for i < len(line) {
				if line[i] == 0x07 {
					i++
					break
				}
				if line[i] == 0x1b && i+1 < len(line) && line[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
		case '(', ')', '*', '+', '-', '.', '/':
			// Designate character-set sequence: ESC <prefix> <byte>
			if i+1 < len(line) {
				i += 2
			} else {
				i++
			}
		default:
			// Two-byte escape sequence: ESC <byte>
			i++
		}
	}
	return b.String()
}

func isPromptChromeLine(line string) bool {
	return strings.HasPrefix(line, "› ") || strings.HasPrefix(line, "❯ ")
}

func isPromptContinuationLine(raw, line string) bool {
	if !hasLeadingIndent(raw) {
		return false
	}
	if looksLikeExplicitNeedsInputLine(line) || looksLikeQuestionNeedsInputLine(line) {
		return false
	}
	switch {
	case strings.HasPrefix(line, "• "),
		strings.HasPrefix(line, "- "),
		strings.HasPrefix(line, "* "),
		strings.HasPrefix(line, "1."),
		strings.HasPrefix(line, "2."),
		strings.HasPrefix(line, "3."),
		strings.HasPrefix(line, "```"):
		return false
	default:
		return true
	}
}

func hasLeadingIndent(raw string) bool {
	if raw == "" {
		return false
	}
	return raw[0] == ' ' || raw[0] == '\t'
}

func shouldDropAgentChromeLine(line string) bool {
	clean := strings.TrimSpace(stripANSIEscape(line))
	if isAgentProgressNoiseLine(clean) {
		return true
	}
	// Keep generic "> " lines (Markdown quotes, shell echoes, diff snippets).
	// Only drop known inline prompt chrome patterns.
	if isInlinePromptChromeLine(clean) {
		return true
	}
	if isVersionBannerChromeLine(clean) {
		return true
	}
	switch {
	case strings.HasPrefix(clean, "╭"),
		strings.HasPrefix(clean, "╰"),
		strings.HasPrefix(clean, "│"),
		strings.HasPrefix(clean, "─"),
		strings.HasPrefix(clean, "────────────────"),
		strings.HasPrefix(clean, "└ "),
		strings.HasPrefix(clean, "⎿ "),
		strings.HasPrefix(clean, "↳ Interacted with "),
		strings.HasPrefix(clean, "› "),
		strings.HasPrefix(clean, "❯"),
		strings.HasPrefix(clean, "? for shortcuts"),
		strings.HasPrefix(clean, "✶ "),
		strings.HasPrefix(clean, "✻ "),
		clean == "✻",
		clean == "|",
		strings.HasPrefix(clean, "▟"),
		strings.HasPrefix(clean, "▐"),
		strings.HasPrefix(clean, "▝"),
		strings.HasPrefix(clean, "▘"),
		strings.HasPrefix(clean, "Tip:"),
		strings.HasPrefix(clean, "Try \""),
		strings.HasPrefix(clean, "• Ran "),
		(strings.Contains(clean, "❯") && strings.Contains(clean, "Try \"")),
		strings.Contains(clean, "Claude Code v"),
		strings.Contains(clean, "· Claude Max"),
		strings.HasPrefix(clean, "model:"),
		strings.HasPrefix(clean, "directory:"),
		strings.HasPrefix(clean, "cwd:"),
		strings.HasPrefix(clean, "workspace:"),
		strings.HasPrefix(clean, "Current folder:"),
		strings.HasPrefix(clean, "ENTER to send"),
		strings.HasPrefix(clean, "You are standing in an open terminal."),
		strings.HasPrefix(clean, "Auto (High) - allow all commands"),
		strings.Contains(clean, "[Z.AI Coding Plan]"),
		strings.Contains(clean, "shift+tab to cycle modes (auto/spec)"),
		strings.Contains(clean, "ctrl+N to cycle"),
		(strings.Contains(strings.ToLower(clean), "autonomy") && strings.Contains(strings.ToLower(clean), "models")),
		(strings.HasPrefix(clean, "[⏱") && strings.Contains(strings.ToLower(clean), "for help")),
		strings.Contains(clean, "chatgpt.com/codex"):
		return true
	default:
		return false
	}
}

func isVersionBannerChromeLine(line string) bool {
	return versionBannerChromeLineRegex.MatchString(strings.ToLower(strings.TrimSpace(line)))
}

func isInlinePromptChromeLine(line string) bool {
	if !strings.HasPrefix(line, "> ") {
		return false
	}
	prompt := strings.TrimSpace(strings.TrimPrefix(line, "> "))
	if prompt == "" {
		return true
	}
	lower := strings.ToLower(prompt)
	return strings.HasPrefix(lower, "reply exactly ") && strings.HasSuffix(lower, " in one line.")
}

func isAgentProgressNoiseLine(line string) bool {
	normalized := normalizeStatusLine(line)
	lower := strings.ToLower(normalized)
	if lower == "" {
		return false
	}
	if strings.HasPrefix(normalized, "Thinking ") {
		return true
	}
	if strings.HasPrefix(normalized, "Working (") && strings.Contains(lower, "esc to interrupt") {
		return true
	}
	if strings.Contains(lower, "esc to interrupt") && !looksLikeNeedsInputLine(normalized) {
		return true
	}
	return false
}

func normalizeStatusLine(line string) string {
	trimmed := strings.TrimSpace(stripANSIEscape(line))
	switch {
	case strings.HasPrefix(trimmed, "• "):
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "• "))
	case strings.HasPrefix(trimmed, "- "):
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
	case strings.HasPrefix(trimmed, "* "):
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "* "))
	default:
		return trimmed
	}
}

// detectNeedsInput returns true when output indicates the agent is waiting on
// user confirmation or a direct question.
func detectNeedsInput(content string) (bool, string) {
	if ok, hint := detectNeedsInputPrompt(content); ok {
		return true, hint
	}

	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || shouldDropAgentChromeLine(line) {
			continue
		}
		if looksLikeQuestionNeedsInputLine(line) {
			return true, normalizeNeedsInputHint(line)
		}
	}
	return false, ""
}

// detectNeedsInputPrompt detects explicit prompt/approval gates that require
// an immediate user response (safe for early-return wait semantics).
func detectNeedsInputPrompt(content string) (bool, string) {
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if shouldDropAgentChromeLine(line) {
			continue
		}
		if looksLikeExplicitNeedsInputLine(line) {
			hint := normalizeNeedsInputHint(line)
			if optionHint := trailingNeedsInputOptionLines(lines, i+1); optionHint != "" {
				hint = hint + "\n" + optionHint
			}
			return true, hint
		}
	}
	return false, ""
}

func normalizeNeedsInputHint(line string) string {
	hint := strings.TrimSpace(line)
	lower := strings.ToLower(hint)
	if strings.Contains(lower, "bypass permissions on") {
		return "Assistant is waiting for local permission-mode selection."
	}
	return hint
}

func looksLikeNeedsInputLine(line string) bool {
	return looksLikeExplicitNeedsInputLine(line) || looksLikeQuestionNeedsInputLine(line)
}

// looksLikeQuestionNeedsInputLine detects direct user-facing questions that
// likely require an operator reply.
func looksLikeQuestionNeedsInputLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	// Heuristic fallback for direct assistant questions.
	if !strings.HasSuffix(lower, "?") {
		return false
	}
	questionMarkers := []string{
		"do you",
		"would you",
		"should i",
		"can i",
		"could you",
		"can you",
		"should we",
		"would you like",
		"which option",
		"which do you",
		"what should",
		"where should",
		"when should",
		"how should",
		"choose",
		"select",
		"proceed",
		"continue",
	}
	for _, marker := range questionMarkers {
		if lower == marker || strings.HasPrefix(lower, marker+" ") || strings.HasPrefix(lower, marker+"?") {
			return true
		}
	}
	// Also check the last sentence in multi-sentence lines.
	if idx := strings.LastIndex(lower, ". "); idx >= 0 {
		lastSentence := strings.TrimSpace(lower[idx+2:])
		for _, marker := range questionMarkers {
			if lastSentence == marker || strings.HasPrefix(lastSentence, marker+" ") || strings.HasPrefix(lastSentence, marker+"?") {
				return true
			}
		}
	}
	return false
}

func lastNonEmptyLine(content string) string {
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || shouldDropAgentChromeLine(line) {
			continue
		}
		return line
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

func summarizeWatchEvent(
	eventType string,
	latestLine string,
	needsInput bool,
	inputHint string,
	idleSeconds float64,
) string {
	if needsInput {
		if strings.TrimSpace(inputHint) != "" {
			return "Needs input: " + strings.TrimSpace(inputHint)
		}
		return "Needs input"
	}
	if strings.TrimSpace(latestLine) != "" {
		return strings.TrimSpace(latestLine)
	}
	switch eventType {
	case "idle":
		return fmt.Sprintf("Idle %.1fs", idleSeconds)
	case "heartbeat":
		return fmt.Sprintf("Still working (idle %.1fs)", idleSeconds)
	default:
		return ""
	}
}

func summarizeWaitResponse(status, latestLine string, needsInput bool, inputHint string) string {
	if needsInput {
		if strings.TrimSpace(inputHint) != "" {
			return "Needs input: " + strings.TrimSpace(inputHint)
		}
		return "Needs input"
	}
	if strings.TrimSpace(latestLine) != "" {
		return strings.TrimSpace(latestLine)
	}
	switch status {
	case "timed_out":
		return "Timed out waiting for agent response."
	case "session_exited":
		return "Agent session exited while waiting."
	case "idle":
		return "Agent step completed."
	case "needs_input":
		return "Needs input"
	default:
		return ""
	}
}
