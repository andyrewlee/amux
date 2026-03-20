package cli

import "testing"

func TestAssistantFormatCapture_StripsANSIAndCarriageReturns(t *testing.T) {
	input := "\x1b[32mhello\x1b[0m\r\n\x1b]0;title\x07world"
	got := assistantFormatCapture(input, assistantFormatCaptureOptions{StripANSI: true})
	want := "hello\nworld"
	if got != want {
		t.Fatalf("assistantFormatCapture() = %q, want %q", got, want)
	}
}

func TestAssistantFormatCapture_ExtractsLastAnswerAfterPrompt(t *testing.T) {
	input := "$ ls\nfile1\n> summarize this\nanswer line 1\nanswer line 2"
	got := assistantFormatCapture(input, assistantFormatCaptureOptions{LastAnswer: true})
	want := "answer line 1\nanswer line 2"
	if got != want {
		t.Fatalf("assistantFormatCapture() = %q, want %q", got, want)
	}
}

func TestAssistantFormatCapture_TrimPreservesWhitespaceOnlyLines(t *testing.T) {
	input := "\n\nfirst\n \nsecond\n\n"
	got := assistantFormatCapture(input, assistantFormatCaptureOptions{Trim: true})
	want := "first\n \nsecond"
	if got != want {
		t.Fatalf("assistantFormatCapture() = %q, want %q", got, want)
	}
}

func TestAssistantFormatCapture_CombinedOptions(t *testing.T) {
	input := "\n> review this\r\n\x1b[36mfinal answer\x1b[0m\n\n"
	got := assistantFormatCapture(input, assistantFormatCaptureOptions{
		StripANSI:  true,
		LastAnswer: true,
		Trim:       true,
	})
	want := "final answer"
	if got != want {
		t.Fatalf("assistantFormatCapture() = %q, want %q", got, want)
	}
}
