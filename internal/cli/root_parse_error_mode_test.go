package cli

import (
	"encoding/json"
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

func TestRunParseErrorUsesJSONWhenTrailingGlobalJSONFollowsMalformedGlobal(t *testing.T) {
	code, stdout, stderr := runWithCapturedStdIO(
		t,
		[]string{"status", "--timeout=abc", "--json"},
	)
	if code != ExitUsage {
		t.Fatalf("Run() code = %d, want %d", code, ExitUsage)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected empty stderr in JSON mode, got %q", stderr)
	}

	var env Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, stdout)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "invalid --timeout value") {
		t.Fatalf("unexpected parse error message: %#v", env.Error)
	}
}
