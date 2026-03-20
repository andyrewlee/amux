package cli

import (
	"strings"
	"unicode"
)

func assistantStepBuildDeltaExcerpt(raw string, maxLines int) string {
	if maxLines <= 0 {
		maxLines = 3
	}

	lines := strings.Split(raw, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = assistantStepTrimLine(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Search "),
			strings.HasPrefix(line, "Read "),
			strings.HasPrefix(line, "List "),
			strings.HasPrefix(line, "Working "),
			strings.HasPrefix(line, "Thinking "),
			strings.HasPrefix(line, "• I "):
			continue
		}
		filtered = append(filtered, line)
	}

	if len(filtered) == 0 {
		return ""
	}
	start := len(filtered) - maxLines
	if start < 0 {
		start = 0
	}
	out := append([]string{}, filtered[start:]...)
	if start > 0 {
		out = append([]string{"..."}, out...)
	}
	return strings.Join(out, "\n")
}

func assistantStepExtractDeltaSummaryCandidate(raw string) string {
	lines := strings.Split(raw, "\n")
	candidate := ""
	fragment := ""

	for i := len(lines) - 1; i >= 0; i-- {
		line := assistantStepTrimLine(lines[i])
		if line == "" {
			continue
		}
		if assistantStepIsAgentProgressLine(line) || assistantStepIsJSONishFragment(line) {
			continue
		}
		if assistantStepIsWrappedFragmentLine(line) {
			if candidate == "" {
				candidate = line
			}
			fragment = line
			continue
		}
		if fragment != "" && !assistantStepIsBulletPrefix(line) {
			fragment = ""
		}
		if fragment != "" && assistantStepIsBulletPrefix(line) {
			line = assistantStepTrimLine(line + " " + fragment)
			if strings.HasSuffix(line, ":") {
				if candidate == "" {
					candidate = line
				}
				continue
			}
			return line
		}
		if assistantStepIsBulletPrefix(line) {
			if strings.Contains(line, "/") ||
				strings.Contains(line, ".go") ||
				strings.Contains(line, ".md") ||
				strings.Contains(line, ".sh") ||
				strings.Contains(line, ":") {
				if assistantStepIsFileOnlyBullet(line) {
					if candidate == "" {
						candidate = line
					}
					continue
				}
				if strings.HasSuffix(line, ":") {
					if candidate == "" {
						candidate = line
					}
					continue
				}
				return line
			}
			if candidate == "" {
				candidate = line
			}
			continue
		}
		if candidate == "" && len(line) >= 32 {
			candidate = line
		}
	}

	if candidate != "" && strings.HasSuffix(candidate, ":") {
		candidate = assistantStepTrimLine(strings.TrimSuffix(candidate, ":"))
	}
	return candidate
}

func assistantStepIsBulletPrefix(line string) bool {
	return strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "• ")
}

func assistantStepSanitizeSummaryText(raw string) string {
	text := assistantStepTrimLine(assistantStepStripANSI(raw))
	if text == "" {
		return ""
	}

	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "visit https://chatgpt.com/codex"),
		strings.Contains(lower, "app-landing-page=true"),
		strings.Contains(lower, "continue in your browser"):
		return ""
	}
	if assistantStepIsJSONishFragment(text) {
		return ""
	}
	for _, re := range assistantStepJSONTailREs {
		text = re.ReplaceAllString(text, "")
	}
	text = assistantStepTrimLine(text)
	if assistantStepIsChromeLine(text) {
		return ""
	}
	return text
}

func assistantStepSummaryIsWeak(summary string) bool {
	trimmed := assistantStepTrimLine(summary)
	if trimmed == "" {
		return true
	}
	if len(trimmed) < 24 {
		return true
	}
	switch strings.ToLower(trimmed) {
	case "output tracking.", "effort to fix these.", "these.", "done", "done.", "ok", "ok.", "complete", "complete.":
		return true
	default:
		return false
	}
}

func assistantStepLineHasFileSignal(value string) bool {
	switch {
	case strings.Contains(value, ".go"),
		strings.Contains(value, ".md"),
		strings.Contains(value, ".sh"),
		strings.Contains(value, "internal/"),
		strings.Contains(value, "cmd/"),
		strings.Contains(value, "skills/"),
		strings.Contains(value, "README."),
		strings.Contains(value, "Makefile"):
		return true
	default:
		return false
	}
}

func assistantStepIsFileOnlyBullet(line string) bool {
	trimmed := assistantStepTrimLine(line)
	if !assistantStepIsBulletPrefix(trimmed) {
		return false
	}
	value := strings.TrimPrefix(trimmed, "- ")
	value = strings.TrimPrefix(value, "• ")
	value = assistantStepTrimLine(value)
	if value == "" || strings.Contains(value, " ") || strings.Contains(value, ":") {
		return false
	}
	switch {
	case strings.HasSuffix(value, ".go"),
		strings.HasSuffix(value, ".md"),
		strings.HasSuffix(value, ".sh"),
		strings.HasSuffix(value, ".py"),
		strings.HasSuffix(value, ".ts"),
		strings.HasSuffix(value, ".tsx"),
		strings.HasSuffix(value, ".js"),
		strings.HasSuffix(value, ".jsx"),
		strings.HasSuffix(value, ".json"),
		strings.HasSuffix(value, ".yaml"),
		strings.HasSuffix(value, ".yml"),
		strings.HasSuffix(value, ".toml"),
		strings.Contains(value, "Makefile"),
		strings.Contains(value, "/"):
		return true
	default:
		return false
	}
}

func assistantStepIsWrappedFragmentLine(line string) bool {
	trimmed := assistantStepTrimLine(line)
	if trimmed == "" || assistantStepIsBulletPrefix(trimmed) {
		return false
	}
	if assistantStepStartsWithLowerAlphaNum(trimmed) && assistantStepLineHasFileSignal(trimmed) {
		return true
	}
	if assistantStepStartsWithLowerAlphaNum(trimmed) && strings.Contains(trimmed, "): ") {
		return true
	}
	return false
}

func assistantStepStartsWithLowerAlphaNum(line string) bool {
	for _, r := range line {
		return unicode.IsLower(r) || unicode.IsDigit(r)
	}
	return false
}

func assistantStepIsJSONishFragment(line string) bool {
	trimmed := assistantStepTrimLine(line)
	switch {
	case strings.HasPrefix(trimmed, "- {"), strings.HasPrefix(trimmed, "{"):
		return strings.Contains(trimmed, `":"`) || strings.Contains(trimmed, `{\"`) || strings.Contains(trimmed, `,\"`)
	default:
		return false
	}
}

func assistantStepIsAgentProgressLine(line string) bool {
	switch {
	case strings.HasPrefix(line, "Search "),
		strings.HasPrefix(line, "Read "),
		strings.HasPrefix(line, "List "),
		strings.HasPrefix(line, "Working "),
		strings.HasPrefix(line, "Thinking "),
		strings.HasPrefix(line, "• I "),
		strings.HasPrefix(line, "• I'll "),
		strings.HasPrefix(line, "• I’ll "),
		strings.HasPrefix(line, "If you want"),
		line == "No explicit TODO/FIXME debt markers were found":
		return true
	default:
		return false
	}
}

func assistantStepLastNonemptyLine(raw string) string {
	last := ""
	for _, line := range strings.Split(raw, "\n") {
		line = assistantStepTrimLine(line)
		if line == "" || assistantStepIsChromeLine(line) {
			continue
		}
		last = line
	}
	return last
}

func assistantStepCompactAgentText(raw string) string {
	lines := strings.Split(assistantStepStripANSI(raw), "\n")
	out := make([]string, 0, len(lines))
	substantiveSeen := false
	for _, line := range lines {
		line = assistantStepTrimLine(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "> ") && substantiveSeen && !assistantStepIsInlinePromptChromeLine(line) {
			out = append(out, line)
			continue
		}
		if assistantStepIsChromeLine(line) {
			continue
		}
		out = append(out, line)
		substantiveSeen = true
	}
	return strings.Join(out, "\n")
}

func assistantStepIsChromeLine(line string) bool {
	lower := strings.ToLower(line)
	collapsed := strings.Join(strings.Fields(lower), " ")

	switch {
	case line == "",
		line == "|",
		line == "✻",
		strings.HasPrefix(line, "╭"),
		strings.HasPrefix(line, "╰"),
		strings.HasPrefix(line, "│"),
		strings.HasPrefix(line, "─"),
		strings.HasPrefix(line, "└ "),
		strings.HasPrefix(line, "⎿ "),
		strings.HasPrefix(line, "↳ Interacted with "),
		strings.HasPrefix(line, "› "),
		strings.HasPrefix(line, "❯ "),
		strings.HasPrefix(line, "> "),
		strings.HasSuffix(line, "? for shortcuts"),
		strings.HasPrefix(line, "✶ "),
		strings.HasPrefix(line, "✻ "),
		strings.HasPrefix(line, "▟"),
		strings.HasPrefix(line, "▐"),
		strings.HasPrefix(line, "▝"),
		strings.HasPrefix(line, "▘"),
		strings.HasPrefix(line, "Tip:"),
		strings.HasPrefix(line, "model:"),
		strings.HasPrefix(line, "directory:"),
		strings.HasPrefix(line, "cwd:"),
		strings.HasPrefix(line, "workspace:"),
		strings.HasPrefix(line, "Current folder:"),
		strings.HasPrefix(line, "ENTER to send"),
		strings.HasPrefix(line, "You are standing in an open terminal."),
		strings.HasPrefix(line, "Auto (High) - allow all commands"),
		strings.HasPrefix(line, `Try "`),
		line == "• Explored",
		line == "• Exploring",
		strings.HasPrefix(line, "• Working ("),
		strings.HasPrefix(line, "Working ("),
		strings.HasPrefix(line, "Thinking "),
		strings.Contains(line, " no sandbox "),
		strings.Contains(line, "/model "),
		strings.HasPrefix(line, "~/.amux/"),
		strings.Contains(line, "sandbox   "),
		(strings.Contains(line, "sandbox ") && strings.HasSuffix(line, ")")),
		strings.HasPrefix(line, "shift+tab to accept edits"),
		strings.HasPrefix(line, "/ commands · @ files · ! shell"),
		strings.Contains(line, "? for help"),
		strings.Contains(line, "▄▄▄▄"),
		strings.Contains(line, "███"),
		strings.Contains(line, "▀▀▀"),
		strings.HasPrefix(line, ">   Type your message or @path/to/file"):
		return true
	}
	if assistantStepIsInlinePromptChromeLine(line) {
		return true
	}
	switch {
	case strings.Contains(line, "[Z.AI Coding Plan]"),
		strings.Contains(lower, "shift+tab to cycle modes (auto/spec)"),
		strings.Contains(lower, "ctrl+n to cycle"),
		strings.Contains(lower, "chatgpt.com/codex"),
		strings.Contains(collapsed, "autonomy models"),
		strings.HasPrefix(line, "[⏱") && strings.Contains(lower, "for help"),
		versionBannerChromeLineRegex.MatchString(lower):
		return true
	default:
		return false
	}
}

func assistantStepIsInlinePromptChromeLine(line string) bool {
	if !strings.HasPrefix(line, "> ") {
		return false
	}
	prompt := assistantStepTrimLine(strings.TrimPrefix(line, "> "))
	if prompt == "" {
		return true
	}
	lower := strings.ToLower(prompt)
	return strings.HasPrefix(lower, "reply exactly ") && strings.HasSuffix(lower, " in one line.")
}

func assistantStepTrimLine(line string) string {
	return strings.TrimSpace(line)
}

func assistantStepStripANSI(input string) string {
	if strings.Contains(input, "\x1b") {
		input = stripANSIEscape(input)
	}
	return strings.ReplaceAll(input, "\r", "")
}

func assistantStepRedactSecrets(input string) string {
	for _, redaction := range assistantStepSecretRedactions {
		input = redaction.re.ReplaceAllString(input, redaction.repl)
	}
	return input
}

func assistantStepWorkspaceFromAgentID(agentID string) string {
	agentID = strings.TrimSpace(agentID)
	if idx := strings.IndexByte(agentID, ':'); idx > 0 {
		return agentID[:idx]
	}
	return ""
}
