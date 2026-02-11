package cli

import (
	"strings"
	"testing"
)

func TestRunParseErrorDoesNotForceJSONForCommandValueJSONToken(t *testing.T) {
	code, stdout, stderr := runWithCapturedStdIO(
		t,
		[]string{"--timeout=abc", "agent", "send", "s", "--text", "--json"},
	)
	if code != ExitUsage {
		t.Fatalf("Run() code = %d, want %d", code, ExitUsage)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("expected empty stdout in human parse-error mode, got %q", stdout)
	}
	if !strings.Contains(stderr, "invalid --timeout value") {
		t.Fatalf("expected parse error on stderr, got %q", stderr)
	}
}
