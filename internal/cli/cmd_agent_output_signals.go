package cli

import (
	"fmt"
	"strings"
)

// compactAgentOutput strips known TUI chrome lines and collapses output to
// concise non-empty lines suitable for chat notifications.
func compactAgentOutput(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	dropPromptContinuation := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
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
	if isAgentProgressNoiseLine(line) {
		return true
	}
	switch {
	case strings.HasPrefix(line, "╭"),
		strings.HasPrefix(line, "╰"),
		strings.HasPrefix(line, "│"),
		strings.HasPrefix(line, "─"),
		strings.HasPrefix(line, "────────────────"),
		strings.HasPrefix(line, "└ "),
		strings.HasPrefix(line, "⎿ "),
		strings.HasPrefix(line, "↳ Interacted with "),
		strings.HasPrefix(line, "› "),
		strings.HasPrefix(line, "❯"),
		strings.HasPrefix(line, "? for shortcuts"),
		strings.HasPrefix(line, "✶ "),
		strings.HasPrefix(line, "✻ "),
		line == "✻",
		line == "|",
		strings.HasPrefix(line, "▟"),
		strings.HasPrefix(line, "▐"),
		strings.HasPrefix(line, "▝"),
		strings.HasPrefix(line, "▘"),
		strings.HasPrefix(line, "Tip:"),
		strings.HasPrefix(line, "• Ran "),
		strings.Contains(line, "Claude Code v"),
		strings.Contains(line, "· Claude Max"),
		strings.HasPrefix(line, "model:"),
		strings.HasPrefix(line, "directory:"),
		strings.HasPrefix(line, "cwd:"),
		strings.HasPrefix(line, "workspace:"),
		strings.Contains(line, "chatgpt.com/codex"):
		return true
	default:
		return false
	}
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
	trimmed := strings.TrimSpace(line)
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
			return true, normalizeNeedsInputHint(line)
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

func looksLikeExplicitNeedsInputLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}

	// Explicit confirmation prompts and permission gates.
	markers := []string{
		"(y/n)",
		"[y/n]",
		"(yes/no)",
		"[yes/no]",
		"press enter",
		"press return",
		"press any key",
		"do you want",
		"would you like",
		"should i ",
		"which option",
		"select an option",
		"choose an option",
		"awaiting your input",
		"waiting for your input",
		"needs your approval",
		"requires approval",
		"permission required",
		"bypass permissions on",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

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
		"which",
		"what",
		"where",
		"when",
		"how",
		"choose",
		"select",
		"proceed",
		"continue",
	}
	for _, marker := range questionMarkers {
		if strings.Contains(lower, marker) {
			return true
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
