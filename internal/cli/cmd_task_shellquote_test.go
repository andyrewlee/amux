package cli

import (
	"strings"
	"testing"
)

func TestShellQuoteCommandValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "''"},
		{name: "plain", in: "codex", want: "'codex'"},
		{name: "metacharacters", in: "$HOME && $(uname) `id`", want: "'$HOME && $(uname) `id`'"},
		{name: "single quote", in: "O'Reilly", want: `'O'"'"'Reilly'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shellQuoteCommandValue(tt.in); got != tt.want {
				t.Fatalf("shellQuoteCommandValue(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildTaskStartResult_SuggestedRestartCommandShellEscapesPrompt(t *testing.T) {
	t.Parallel()

	prompt := `Investigate $HOME and $(uname) with O'Reilly notes`
	result := buildTaskStartResult("ws-123", "codex", prompt, agentRunResult{
		AgentID: "ws-123:t1",
		Response: &waitResponseResult{
			Status: "session_exited",
		},
	})

	quotedPrompt := shellQuoteCommandValue(prompt)
	if !strings.Contains(result.SuggestedCommand, "--prompt "+quotedPrompt) {
		t.Fatalf("suggested_command = %q, want prompt argument %q", result.SuggestedCommand, quotedPrompt)
	}
}
