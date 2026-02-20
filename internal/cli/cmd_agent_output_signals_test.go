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

func TestDetectNeedsInputPrompt_DoesNotMatchQuestionFallbackOnly(t *testing.T) {
	content := "I can continue with either option A or B. Which do you prefer?"
	ok, _ := detectNeedsInputPrompt(content)
	if ok {
		t.Fatalf("detectNeedsInputPrompt() = true, want false")
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
