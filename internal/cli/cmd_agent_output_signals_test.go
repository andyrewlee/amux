package cli

import "testing"

func TestCompactAgentOutput_DropsChromeLines(t *testing.T) {
	raw := "╭────╮\n│ >_ OpenAI Codex │\nmodel: gpt-5\n• useful line\n? for shortcuts\n"
	got := compactAgentOutput(raw)
	if got != "• useful line" {
		t.Fatalf("compactAgentOutput() = %q, want %q", got, "• useful line")
	}
}

func TestCompactAgentOutput_DropsToolExecutionNoise(t *testing.T) {
	raw := "• Ran go test ./...\n└ ok github.com/andyrewlee/amux/internal/cli\n↳ Interacted with background terminal · go test ./...\n⎿ waiting\n• Final summary line"
	got := compactAgentOutput(raw)
	if got != "• Final summary line" {
		t.Fatalf("compactAgentOutput() = %q, want %q", got, "• Final summary line")
	}
}

func TestCompactAgentOutput_DropsBulletedWorkingNoise(t *testing.T) {
	raw := "• Working (33s • esc to interrupt)\n• Added parser helper"
	got := compactAgentOutput(raw)
	if got != "• Added parser helper" {
		t.Fatalf("compactAgentOutput() = %q, want %q", got, "• Added parser helper")
	}
}

func TestCompactAgentOutput_DropsClaudeBannerNoise(t *testing.T) {
	raw := "✻\n|\n▟█▙     Claude Code v2.1.45\n▐▛███▜▌   Opus 4.6 · Claude Max\n▝▜█████▛▘  ~/.amux/workspaces/amux/refactor\n▘▘ ▝▝\n❯ Review files\n✻ Baking…\n✶ Fermenting…\n• useful line"
	got := compactAgentOutput(raw)
	if got != "• useful line" {
		t.Fatalf("compactAgentOutput() = %q, want %q", got, "• useful line")
	}
}

func TestCompactAgentOutput_DropsPromptWrappedContinuation(t *testing.T) {
	raw := "❯ Review uncommitted changes in this workspace and report critical findings\n   first.\n• useful line"
	got := compactAgentOutput(raw)
	if got != "• useful line" {
		t.Fatalf("compactAgentOutput() = %q, want %q", got, "• useful line")
	}
}

func TestCompactAgentOutput_DropsDroidChromeNoise(t *testing.T) {
	raw := "v0.60.0\nYou are standing in an open terminal. An AI awaits your commands.\nENTER to send • \\ + ENTER for a new line • @ to mention files\nCurrent folder: /tmp/repo\n> Reply exactly READY in one line.\n⛬ READY\n⛬ Status: Fresh repo, no pending changes.\nAuto (High) - allow all commands             GLM-5 [Z.AI Coding Plan] [custom]\nshift+tab to cycle modes (auto/spec), ctrl+L for        ctrl+N to cycle\nautonomy                                                models\n[⏱ 1m 18s]? for help                                                     IDE ◌"
	got := compactAgentOutput(raw)
	want := "⛬ READY\n⛬ Status: Fresh repo, no pending changes."
	if got != want {
		t.Fatalf("compactAgentOutput() = %q, want %q", got, want)
	}
}

func TestCompactAgentOutput_PreservesVersionPrefixedContent(t *testing.T) {
	raw := "v0.6 beta\n• useful line"
	got := compactAgentOutput(raw)
	if got != raw {
		t.Fatalf("compactAgentOutput() = %q, want %q", got, raw)
	}
}

func TestCompactAgentOutput_PreservesQuotedLines(t *testing.T) {
	raw := "Plan recap:\n> The user asked to fix the login bug\n> npm run build\nDone."
	got := compactAgentOutput(raw)
	if got != raw {
		t.Fatalf("compactAgentOutput() = %q, want %q", got, raw)
	}
}

func TestStripANSIEscape_StripsOSCAndSingleCharEscapes(t *testing.T) {
	raw := "\x1b]0;window title\x07hello\x1b= world\x1b(0!"
	got := stripANSIEscape(raw)
	if got != "hello world!" {
		t.Fatalf("stripANSIEscape() = %q, want %q", got, "hello world!")
	}
}

func TestCompactAgentOutput_DropsANSIWrappedPromptChrome(t *testing.T) {
	raw := "\x1b[38;5;39m❯\u00a0Try \"fix lint errors\"\x1b[0m\n• useful line"
	got := compactAgentOutput(raw)
	if got != "• useful line" {
		t.Fatalf("compactAgentOutput() = %q, want %q", got, "• useful line")
	}
}

func TestDetectNeedsInput_ConfirmationPrompt(t *testing.T) {
	content := "Plan complete\nDo you want me to proceed? (y/N)"
	ok, hint := detectNeedsInput(content)
	if !ok {
		t.Fatalf("detectNeedsInput() = false, want true")
	}
	if hint != "Do you want me to proceed? (y/N)" {
		t.Fatalf("hint = %q, want %q", hint, "Do you want me to proceed? (y/N)")
	}
}

func TestDetectNeedsInput_QuestionFallback(t *testing.T) {
	content := "I can continue with either option A or B. Which do you prefer?"
	ok, hint := detectNeedsInput(content)
	if !ok {
		t.Fatalf("detectNeedsInput() = false, want true")
	}
	if hint != "I can continue with either option A or B. Which do you prefer?" {
		t.Fatalf("hint = %q", hint)
	}
}

func TestDetectNeedsInput_QuotedQuestionFallback(t *testing.T) {
	content := "> Do you want me to proceed?"
	ok, hint := detectNeedsInput(content)
	if !ok {
		t.Fatalf("detectNeedsInput() = false, want true")
	}
	if hint != content {
		t.Fatalf("hint = %q, want %q", hint, content)
	}
}

func TestDetectNeedsInputPrompt_ExplicitMarker(t *testing.T) {
	content := "Plan complete\nDo you want me to proceed? (y/N)"
	ok, hint := detectNeedsInputPrompt(content)
	if !ok {
		t.Fatalf("detectNeedsInputPrompt() = false, want true")
	}
	if hint != "Do you want me to proceed? (y/N)" {
		t.Fatalf("hint = %q, want %q", hint, "Do you want me to proceed? (y/N)")
	}
}

func TestDetectNeedsInputPrompt_ExplicitMarkerIncludesTrailingOptions(t *testing.T) {
	content := "Choose one and reply with the number:\n1. Continue with codex\n2) Continue with claude\n3. Ask me later\n4. Cancel"
	ok, hint := detectNeedsInputPrompt(content)
	if !ok {
		t.Fatalf("detectNeedsInputPrompt() = false, want true")
	}
	want := "Choose one and reply with the number:\n1. Continue with codex\n2) Continue with claude\n3. Ask me later\n4. Cancel"
	if hint != want {
		t.Fatalf("hint = %q, want %q", hint, want)
	}
}

func TestDetectNeedsInputPrompt_ExplicitMarkerIncludesTrailingANSIOptions(t *testing.T) {
	content := "Choose one and reply with the number:\n\x1b[32m1.\x1b[0m Continue with codex\n\x1b[32m2)\x1b[0m Continue with claude"
	ok, hint := detectNeedsInputPrompt(content)
	if !ok {
		t.Fatalf("detectNeedsInputPrompt() = false, want true")
	}
	want := "Choose one and reply with the number:\n1. Continue with codex\n2) Continue with claude"
	if hint != want {
		t.Fatalf("hint = %q, want %q", hint, want)
	}
}

func TestDetectNeedsInputPrompt_ExplicitMarkerIncludesLetterOptions(t *testing.T) {
	content := "Reply with one letter:\nA. Keep current assistant\nB) Switch assistant"
	ok, hint := detectNeedsInputPrompt(content)
	if !ok {
		t.Fatalf("detectNeedsInputPrompt() = false, want true")
	}
	want := "Reply with one letter:\nA. Keep current assistant\nB) Switch assistant"
	if hint != want {
		t.Fatalf("hint = %q, want %q", hint, want)
	}
}

func TestDetectNeedsInputPrompt_ReplyWithNumericChoices(t *testing.T) {
	content := "Reply with 1 to continue, 2 to cancel."
	ok, hint := detectNeedsInputPrompt(content)
	if !ok {
		t.Fatalf("detectNeedsInputPrompt() = false, want true")
	}
	if hint != content {
		t.Fatalf("hint = %q, want %q", hint, content)
	}
}

func TestDetectNeedsInputPrompt_ReplyWithNumericChoicesMidSentence(t *testing.T) {
	content := "To continue, reply with 1 to approve or 2 to cancel."
	ok, hint := detectNeedsInputPrompt(content)
	if !ok {
		t.Fatalf("detectNeedsInputPrompt() = false, want true")
	}
	if hint != content {
		t.Fatalf("hint = %q, want %q", hint, content)
	}
}

func TestDetectNeedsInputPrompt_ReplyWithSlashLetterChoices(t *testing.T) {
	content := "Reply with A/B/C"
	ok, hint := detectNeedsInputPrompt(content)
	if !ok {
		t.Fatalf("detectNeedsInputPrompt() = false, want true")
	}
	if hint != content {
		t.Fatalf("hint = %q, want %q", hint, content)
	}
}

func TestDetectNeedsInputPrompt_ReplyWithYesConfirmation(t *testing.T) {
	content := "Reply with yes to continue."
	ok, hint := detectNeedsInputPrompt(content)
	if !ok {
		t.Fatalf("detectNeedsInputPrompt() = false, want true")
	}
	if hint != content {
		t.Fatalf("hint = %q, want %q", hint, content)
	}
}

func TestDetectNeedsInputPrompt_ReplyWithShortYesConfirmation(t *testing.T) {
	content := "Reply with y to continue."
	ok, hint := detectNeedsInputPrompt(content)
	if !ok {
		t.Fatalf("detectNeedsInputPrompt() = false, want true")
	}
	if hint != content {
		t.Fatalf("hint = %q, want %q", hint, content)
	}
}

func TestDetectNeedsInputPrompt_ExplicitMarkerIncludesExtendedNumericOptions(t *testing.T) {
	content := "Pick one:\n1. One\n2. Two\n3. Three\n4. Four\n5. Five"
	ok, hint := detectNeedsInputPrompt(content)
	if !ok {
		t.Fatalf("detectNeedsInputPrompt() = false, want true")
	}
	if hint != content {
		t.Fatalf("hint = %q, want %q", hint, content)
	}
}

func TestDetectNeedsInputPrompt_ExplicitMarkerIncludesExtendedLetterOptions(t *testing.T) {
	content := "Reply with one letter:\nA. First\nB. Second\nC. Third\nD. Fourth\nE. Fifth"
	ok, hint := detectNeedsInputPrompt(content)
	if !ok {
		t.Fatalf("detectNeedsInputPrompt() = false, want true")
	}
	if hint != content {
		t.Fatalf("hint = %q, want %q", hint, content)
	}
}

func TestDetectNeedsInputPrompt_DoesNotMatchQuestionFallbackOnly(t *testing.T) {
	content := "I can continue with either option A or B. Which do you prefer?"
	ok, _ := detectNeedsInputPrompt(content)
	if ok {
		t.Fatalf("detectNeedsInputPrompt() = true, want false")
	}
}

func TestDetectNeedsInputPrompt_DoesNotMatchGenericReplyWithText(t *testing.T) {
	content := "I'll reply with a patch summary once tests finish."
	ok, hint := detectNeedsInputPrompt(content)
	if ok {
		t.Fatalf("detectNeedsInputPrompt() = true, want false (hint=%q)", hint)
	}
}

func TestDetectNeedsInputPrompt_DoesNotMatchFirstPersonReplyWithNumbers(t *testing.T) {
	content := "I'll reply with 1 summary and 2 follow-ups after tests."
	ok, hint := detectNeedsInputPrompt(content)
	if ok {
		t.Fatalf("detectNeedsInputPrompt() = true, want false (hint=%q)", hint)
	}
}

func TestDetectNeedsInputPrompt_DoesNotMatchNonChoiceReplyWithNumbers(t *testing.T) {
	content := "For release notes, reply with 2025 roadmap and 2026 patch notes."
	ok, hint := detectNeedsInputPrompt(content)
	if ok {
		t.Fatalf("detectNeedsInputPrompt() = true, want false (hint=%q)", hint)
	}
}

func TestDetectNeedsInput_DoesNotMatchGenericReplyWithText(t *testing.T) {
	content := "I'll reply with a patch summary once tests finish."
	ok, hint := detectNeedsInput(content)
	if ok {
		t.Fatalf("detectNeedsInput() = true, want false (hint=%q)", hint)
	}
}

func TestDetectNeedsInputPrompt_CodexInlinePromptDoesNotTrigger(t *testing.T) {
	content := "Working (1m 40s • esc to interrupt)\n› Find and fix a bug in @filename\n? for shortcuts                                             30% context left"
	ok, hint := detectNeedsInputPrompt(content)
	if ok {
		t.Fatalf("detectNeedsInputPrompt() = true, want false (hint=%q)", hint)
	}
}

func TestDetectNeedsInput_CodexInlinePromptDoesNotTrigger(t *testing.T) {
	content := "Working (1m 40s • esc to interrupt)\n› Find and fix a bug in @filename\n? for shortcuts                                             30% context left"
	ok, hint := detectNeedsInput(content)
	if ok {
		t.Fatalf("detectNeedsInput() = true, want false (hint=%q)", hint)
	}
}

func TestDetectNeedsInputPrompt_NormalizesPermissionSelectorHint(t *testing.T) {
	content := "⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt"
	ok, hint := detectNeedsInputPrompt(content)
	if !ok {
		t.Fatalf("detectNeedsInputPrompt() = false, want true")
	}
	if hint != "Assistant is waiting for local permission-mode selection." {
		t.Fatalf("hint = %q", hint)
	}
}

func TestSummarizeWaitResponse_NeedsInputHint(t *testing.T) {
	got := summarizeWaitResponse(
		"needs_input",
		"Assistant is waiting for local permission-mode selection.",
		true,
		"Assistant is waiting for local permission-mode selection.",
	)
	want := "Needs input: Assistant is waiting for local permission-mode selection."
	if got != want {
		t.Fatalf("summarizeWaitResponse() = %q, want %q", got, want)
	}
}

func TestSummarizeWaitResponse_StatusFallbacks(t *testing.T) {
	if got := summarizeWaitResponse("timed_out", "", false, ""); got != "Timed out waiting for agent response." {
		t.Fatalf("timed_out summary = %q", got)
	}
	if got := summarizeWaitResponse("session_exited", "", false, ""); got != "Agent session exited while waiting." {
		t.Fatalf("session_exited summary = %q", got)
	}
}

func TestLastNonEmptyLine_PreservesQuotedLine(t *testing.T) {
	content := "summary line\n> quoted conclusion"
	if got := lastNonEmptyLine(content); got != "> quoted conclusion" {
		t.Fatalf("lastNonEmptyLine() = %q, want %q", got, "> quoted conclusion")
	}
}
