package cli

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunNoCommandJSONReturnsUsageErrorEnvelope(t *testing.T) {
	code, stdout, stderr := runWithCapturedStdIO(t, []string{"--json"})
	if code != ExitUsage {
		t.Fatalf("Run() code = %d, want %d", code, ExitUsage)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected empty stderr in --json mode, got %q", stderr)
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
	if env.Error == nil || !strings.Contains(env.Error.Message, "Usage: amux <command> [flags]") {
		t.Fatalf("unexpected error message: %#v", env.Error)
	}
}

func TestRunParseErrorUsesJSONWhenFlagAppearsAfterMalformedGlobal(t *testing.T) {
	code, stdout, stderr := runWithCapturedStdIO(t, []string{"--timeout=abc", "--json", "status"})
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

func TestRunVersionJSONReturnsEnvelope(t *testing.T) {
	code, stdout, stderr := runWithCapturedStdIO(t, []string{"--json", "version"})
	if code != ExitOK {
		t.Fatalf("Run() code = %d, want %d", code, ExitOK)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected empty stderr in --json mode, got %q", stderr)
	}

	var env Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, stdout)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error=%#v", env.Error)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env.Data)
	}
	if got, _ := data["version"].(string); got != "test-v1" {
		t.Fatalf("version = %q, want %q", got, "test-v1")
	}
	if got, _ := data["commit"].(string); got != "test-commit" {
		t.Fatalf("commit = %q, want %q", got, "test-commit")
	}
	if got, _ := data["date"].(string); got != "test-date" {
		t.Fatalf("date = %q, want %q", got, "test-date")
	}
}

func TestRunHelpJSONReturnsEnvelope(t *testing.T) {
	code, stdout, stderr := runWithCapturedStdIO(t, []string{"--json", "help"})
	if code != ExitOK {
		t.Fatalf("Run() code = %d, want %d", code, ExitOK)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected empty stderr in --json mode, got %q", stderr)
	}

	var env Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, stdout)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error=%#v", env.Error)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env.Data)
	}
	usage, _ := data["usage"].(string)
	if !strings.Contains(usage, "Usage: amux <command> [flags]") {
		t.Fatalf("usage data missing expected header: %q", usage)
	}
}

func runWithCapturedStdIO(t *testing.T, args []string) (int, string, string) {
	t.Helper()

	origStdout := os.Stdout
	origStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stdout) error = %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stderr) error = %v", err)
	}
	os.Stdout = stdoutW
	os.Stderr = stderrW

	restore := func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}
	defer restore()

	code := Run(args, "test-v1", "test-commit", "test-date")

	_ = stdoutW.Close()
	_ = stderrW.Close()

	stdoutBytes, readStdoutErr := io.ReadAll(stdoutR)
	if readStdoutErr != nil {
		t.Fatalf("io.ReadAll(stdout) error = %v", readStdoutErr)
	}
	stderrBytes, readStderrErr := io.ReadAll(stderrR)
	if readStderrErr != nil {
		t.Fatalf("io.ReadAll(stderr) error = %v", readStderrErr)
	}

	_ = stdoutR.Close()
	_ = stderrR.Close()

	return code, string(stdoutBytes), string(stderrBytes)
}
