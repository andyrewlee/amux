package pty

import "testing"

func TestMaybeApplyAgentNice_DefaultNoop(t *testing.T) {
	t.Setenv("AMUX_AGENT_NICE", "")
	cmd := "codex --json"
	if got := maybeApplyAgentNice(cmd); got != cmd {
		t.Fatalf("expected unchanged command, got %q", got)
	}
}

func TestMaybeApplyAgentNice_InvalidNoop(t *testing.T) {
	t.Setenv("AMUX_AGENT_NICE", "not-a-number")
	cmd := "codex --json"
	if got := maybeApplyAgentNice(cmd); got != cmd {
		t.Fatalf("expected unchanged command for invalid niceness, got %q", got)
	}
}

func TestMaybeApplyAgentNice_AppliesAndQuotes(t *testing.T) {
	t.Setenv("AMUX_AGENT_NICE", "10")
	cmd := "echo 'hello'; codex --json"
	got := maybeApplyAgentNice(cmd)
	want := "nice -n 10 sh -lc 'echo '\"'\"'hello'\"'\"'; codex --json'"
	if got != want {
		t.Fatalf("unexpected niceness wrapper:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestMaybeApplyAgentNice_ClampsRange(t *testing.T) {
	t.Setenv("AMUX_AGENT_NICE", "99")
	if got := maybeApplyAgentNice("cmd"); got != "nice -n 19 sh -lc 'cmd'" {
		t.Fatalf("expected clamp to 19, got %q", got)
	}

	t.Setenv("AMUX_AGENT_NICE", "-99")
	if got := maybeApplyAgentNice("cmd"); got != "nice -n -20 sh -lc 'cmd'" {
		t.Fatalf("expected clamp to -20, got %q", got)
	}
}
