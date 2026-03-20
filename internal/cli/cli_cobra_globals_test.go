package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCobraWithGlobalsInvalidCwdPreservesJSONEnvelope(t *testing.T) {
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
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	code := RunCobraWithGlobals([]string{"--json", "sandbox", "ls"}, GlobalFlags{
		Cwd: filepath.Join(t.TempDir(), "missing"),
	}, "test-v1")

	_ = stdoutW.Close()
	_ = stderrW.Close()
	stdoutBytes, _ := io.ReadAll(stdoutR)
	stderrBytes, _ := io.ReadAll(stderrR)

	if code != ExitUsage {
		t.Fatalf("RunCobraWithGlobals() code = %d, want %d", code, ExitUsage)
	}
	if strings.TrimSpace(string(stderrBytes)) != "" {
		t.Fatalf("expected empty stderr in JSON mode, got %q", string(stderrBytes))
	}

	var env Envelope
	if err := json.Unmarshal(stdoutBytes, &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, string(stdoutBytes))
	}
	if env.OK {
		t.Fatal("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "invalid_cwd" {
		t.Fatalf("expected invalid_cwd error, got %#v", env.Error)
	}
	if env.Command != "sandbox" {
		t.Fatalf("command = %q, want %q", env.Command, "sandbox")
	}
}

func TestRunCobraWithGlobalsInvalidCwdPreservesRequestIDFromArgs(t *testing.T) {
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
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	code := RunCobraWithGlobals([]string{"--request-id", "req-1", "--json", "sandbox", "ls"}, GlobalFlags{
		Cwd: filepath.Join(t.TempDir(), "missing"),
	}, "test-v1")

	_ = stdoutW.Close()
	_ = stderrW.Close()
	stdoutBytes, _ := io.ReadAll(stdoutR)
	stderrBytes, _ := io.ReadAll(stderrR)

	if code != ExitUsage {
		t.Fatalf("RunCobraWithGlobals() code = %d, want %d", code, ExitUsage)
	}
	if strings.TrimSpace(string(stderrBytes)) != "" {
		t.Fatalf("expected empty stderr in JSON mode, got %q", string(stderrBytes))
	}

	var env Envelope
	if err := json.Unmarshal(stdoutBytes, &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, string(stdoutBytes))
	}
	if env.RequestID != "req-1" {
		t.Fatalf("request_id = %q, want %q", env.RequestID, "req-1")
	}
}

func TestRunCobraWithGlobalsInvalidCwdIgnoresPassthroughJSONAfterDoubleDash(t *testing.T) {
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
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	code := RunCobraWithGlobals([]string{"exec", "--", "rg", "--json"}, GlobalFlags{
		Cwd: filepath.Join(t.TempDir(), "missing"),
	}, "test-v1")

	_ = stdoutW.Close()
	_ = stderrW.Close()
	stdoutBytes, _ := io.ReadAll(stdoutR)
	stderrBytes, _ := io.ReadAll(stderrR)

	if code != ExitUsage {
		t.Fatalf("RunCobraWithGlobals() code = %d, want %d", code, ExitUsage)
	}
	if strings.TrimSpace(string(stdoutBytes)) != "" {
		t.Fatalf("expected empty stdout without amux JSON mode, got %q", string(stdoutBytes))
	}
	if !strings.Contains(string(stderrBytes), "invalid --cwd") {
		t.Fatalf("expected human stderr invalid --cwd message, got %q", string(stderrBytes))
	}
}

func TestRunCobraWithGlobalsInvalidCwdHonorsPostCommandJSONAndRequestID(t *testing.T) {
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
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	code := RunCobraWithGlobals([]string{"sandbox", "ls", "--json", "--request-id", "req-post"}, GlobalFlags{
		Cwd: filepath.Join(t.TempDir(), "missing"),
	}, "test-v1")

	_ = stdoutW.Close()
	_ = stderrW.Close()
	stdoutBytes, _ := io.ReadAll(stdoutR)
	stderrBytes, _ := io.ReadAll(stderrR)

	if code != ExitUsage {
		t.Fatalf("RunCobraWithGlobals() code = %d, want %d", code, ExitUsage)
	}
	if strings.TrimSpace(string(stderrBytes)) != "" {
		t.Fatalf("expected empty stderr in JSON mode, got %q", string(stderrBytes))
	}

	var env Envelope
	if err := json.Unmarshal(stdoutBytes, &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, string(stdoutBytes))
	}
	if env.Error == nil || env.Error.Code != "invalid_cwd" {
		t.Fatalf("expected invalid_cwd error, got %#v", env.Error)
	}
	if env.RequestID != "req-post" {
		t.Fatalf("request_id = %q, want %q", env.RequestID, "req-post")
	}
	if env.Command != "sandbox" {
		t.Fatalf("command = %q, want %q", env.Command, "sandbox")
	}
}
