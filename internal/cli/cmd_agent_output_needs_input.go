package cli

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// Heuristic "reply with ..." patterns. Keep these intentionally conservative;
	// broader matching can produce false positives for narrative output.
	replyWithDigitsChoiceRegex  = regexp.MustCompile(`\b\d+\s*(?:/|or)\s*\d+\b|\b\d+\s+to\s+[a-z].*(?:\bor\b|,)\s*\d+\s+to\s+[a-z]`)
	replyWithLettersChoiceRegex = regexp.MustCompile(`\b[a-z](?:/[a-z])+\b|\b[a-z]\s*(?:,|or)\s*[a-z]\b|\b[a-z]\s+to\s+[a-z].*(?:\bor\b|,)\s*[a-z]\s+to\s+[a-z]`)
	replyWithPromptCueRegex     = regexp.MustCompile(`\b(choose|pick|select|option|confirm|continue|proceed)\b`)
)

func looksLikeExplicitNeedsInputLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}

	if looksLikeReplyWithChoicePrompt(lower) {
		return true
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
		"pick one",
		"choose one",
		"enter 1",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func looksLikeReplyWithChoicePrompt(lower string) bool {
	idx := strings.Index(lower, "reply with ")
	if idx < 0 {
		return false
	}
	if !looksLikeReplyWithPromptPrefix(lower[:idx]) {
		return false
	}
	rest := strings.TrimSpace(lower[idx+len("reply with "):])
	if rest == "" {
		return false
	}

	if strings.Contains(rest, "the number") ||
		strings.Contains(rest, "a number") ||
		strings.Contains(rest, "one number") ||
		strings.Contains(rest, "the letter") ||
		strings.Contains(rest, "a letter") ||
		strings.Contains(rest, "one letter") ||
		strings.Contains(rest, "yes/no") ||
		strings.Contains(rest, "yes or no") ||
		strings.Contains(rest, "y/n") {
		return true
	}
	if looksLikeSingleReplyWithConfirmation(rest) {
		return true
	}
	if replyWithDigitsChoiceRegex.MatchString(rest) {
		return true
	}
	if replyWithLettersChoiceRegex.MatchString(rest) {
		return true
	}
	return false
}

func looksLikeSingleReplyWithConfirmation(rest string) bool {
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return false
	}
	first := strings.Trim(fields[0], ".,;:!?\"'`()[]{}")
	switch first {
	case "yes", "no", "y", "n":
		return true
	default:
		return false
	}
}

func looksLikeReplyWithPromptPrefix(prefix string) bool {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		return true
	}
	if strings.HasSuffix(trimmed, "please") {
		return true
	}

	for _, suffix := range []string{
		"to continue",
		"to proceed",
		"to confirm",
		"to choose",
		"to select",
	} {
		if strings.HasSuffix(trimmed, suffix) {
			return true
		}
	}

	// Accept imperative-style wording like "choose one and reply with ..."
	// while ignoring generic first-person narration ("I'll reply with ...").
	if replyWithPromptCueRegex.MatchString(trimmed) {
		return true
	}

	last := trimmed[len(trimmed)-1]
	switch last {
	case '.', ',', ':', ';', '!', '?', '(', ')':
		return strings.HasPrefix(trimmed, "to ") || strings.HasPrefix(trimmed, "please")
	default:
		return false
	}
}

func trailingNeedsInputOptionLines(lines []string, start int) string {
	if start < 0 || start >= len(lines) {
		return ""
	}
	options := make([]string, 0, 9)
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(stripANSIEscape(lines[i]))
		if line == "" {
			if len(options) > 0 {
				break
			}
			continue
		}
		if shouldDropAgentChromeLine(line) {
			continue
		}
		if looksLikeEnumeratedOptionLine(line) {
			options = append(options, line)
			if len(options) >= 9 {
				break
			}
			continue
		}
		if len(options) > 0 {
			break
		}
	}
	return strings.Join(options, "\n")
}

func looksLikeEnumeratedOptionLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	for n := 1; n <= 9; n++ {
		pDot := fmt.Sprintf("%d.", n)
		pParen := fmt.Sprintf("%d)", n)
		if strings.HasPrefix(trimmed, pDot) {
			return strings.TrimSpace(trimmed[len(pDot):]) != ""
		}
		if strings.HasPrefix(trimmed, pParen) {
			return strings.TrimSpace(trimmed[len(pParen):]) != ""
		}
	}
	for _, prefix := range []string{
		"A.", "A)", "B.", "B)", "C.", "C)", "D.", "D)", "E.", "E)",
		"a.", "a)", "b.", "b)", "c.", "c)", "d.", "d)", "e.", "e)",
	} {
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimSpace(trimmed[len(prefix):]) != ""
		}
	}
	return false
}
