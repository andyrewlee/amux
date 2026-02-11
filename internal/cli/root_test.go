package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseGlobalFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantGF   GlobalFlags
		wantRest []string
		wantErr  bool
	}{
		{
			name:     "prefix extraction",
			args:     []string{"--json", "--quiet", "status"},
			wantGF:   GlobalFlags{JSON: true, Quiet: true},
			wantRest: []string{"status"},
		},
		{
			name:     "global after command extracted",
			args:     []string{"--json", "status", "--quiet"},
			wantGF:   GlobalFlags{JSON: true, Quiet: true},
			wantRest: []string{"status"},
		},
		{
			name:     "subcommand value preserved",
			args:     []string{"agent", "send", "s", "--text", "--json"},
			wantGF:   GlobalFlags{},
			wantRest: []string{"agent", "send", "s", "--text", "--json"},
		},
		{
			name:     "global parsed after local value flag",
			args:     []string{"agent", "send", "s", "--text", "hello", "--json"},
			wantGF:   GlobalFlags{JSON: true},
			wantRest: []string{"agent", "send", "s", "--text", "hello"},
		},
		{
			name:     "global after nested subcommand extracted",
			args:     []string{"workspace", "list", "--cwd", "/tmp"},
			wantGF:   GlobalFlags{Cwd: "/tmp"},
			wantRest: []string{"workspace", "list"},
		},
		{
			name:     "global between command and subcommand extracted",
			args:     []string{"workspace", "--cwd", "/tmp", "list"},
			wantGF:   GlobalFlags{Cwd: "/tmp"},
			wantRest: []string{"workspace", "list"},
		},
		{
			name:     "global timeout after command extracted",
			args:     []string{"status", "--timeout", "2s"},
			wantGF:   GlobalFlags{Timeout: 2 * time.Second},
			wantRest: []string{"status"},
		},
		{
			name:     "local timeout on agent job wait is preserved",
			args:     []string{"agent", "job", "wait", "job-1", "--timeout", "2s", "--json"},
			wantGF:   GlobalFlags{JSON: true},
			wantRest: []string{"agent", "job", "wait", "job-1", "--timeout", "2s"},
		},
		{
			name:     "interleaved global still infers nested command path",
			args:     []string{"agent", "--json", "job", "wait", "job-1", "--timeout", "2s"},
			wantGF:   GlobalFlags{JSON: true},
			wantRest: []string{"agent", "job", "wait", "job-1", "--timeout", "2s"},
		},
		{
			name:     "cwd= form",
			args:     []string{"--cwd=/tmp", "status"},
			wantGF:   GlobalFlags{Cwd: "/tmp"},
			wantRest: []string{"status"},
		},
		{
			name:     "request-id flag",
			args:     []string{"--request-id", "req-123", "status"},
			wantGF:   GlobalFlags{RequestID: "req-123"},
			wantRest: []string{"status"},
		},
		{
			name:     "only globals",
			args:     []string{"--json", "--no-color"},
			wantGF:   GlobalFlags{JSON: true, NoColor: true},
			wantRest: nil,
		},
		{
			name:     "empty args",
			args:     nil,
			wantGF:   GlobalFlags{},
			wantRest: nil,
		},
		{
			name:     "unknown flag stops extraction",
			args:     []string{"--json", "--unknown", "status"},
			wantGF:   GlobalFlags{JSON: true},
			wantRest: []string{"--unknown", "status"},
		},
		{
			name:    "malformed timeout equals form",
			args:    []string{"--timeout=1sec", "status"},
			wantGF:  GlobalFlags{},
			wantErr: true,
		},
		{
			name:    "malformed timeout space form",
			args:    []string{"--timeout", "abc", "status"},
			wantGF:  GlobalFlags{},
			wantErr: true,
		},
		{
			name:    "bare --cwd missing value",
			args:    []string{"--cwd"},
			wantErr: true,
		},
		{
			name:    "bare --timeout missing value",
			args:    []string{"--timeout"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotGF, gotRest, gotErr := ParseGlobalFlags(tt.args)
			if tt.wantErr {
				if gotErr == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if gotErr != nil {
				t.Fatalf("unexpected error: %v", gotErr)
			}
			if !reflect.DeepEqual(gotGF, tt.wantGF) {
				t.Errorf("GlobalFlags = %+v, want %+v", gotGF, tt.wantGF)
			}
			if !reflect.DeepEqual(gotRest, tt.wantRest) {
				t.Errorf("rest = %v, want %v", gotRest, tt.wantRest)
			}
		})
	}
}

func TestApplyRunGlobalsAppliesAndRestores(t *testing.T) {
	prevTimeout := setCLITmuxTimeoutOverride(0)
	defer setCLITmuxTimeoutOverride(prevTimeout)

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}

	targetWD := t.TempDir()
	restore, err := applyRunGlobals(GlobalFlags{
		Cwd:     targetWD,
		Timeout: 250 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("applyRunGlobals() error = %v", err)
	}

	gotWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() after apply error = %v", err)
	}
	if gotWD != targetWD {
		gotCanonical, err := filepath.EvalSymlinks(gotWD)
		if err != nil {
			t.Fatalf("EvalSymlinks(got cwd) error = %v", err)
		}
		wantCanonical, err := filepath.EvalSymlinks(targetWD)
		if err != nil {
			t.Fatalf("EvalSymlinks(target cwd) error = %v", err)
		}
		if gotCanonical != wantCanonical {
			t.Fatalf("cwd after apply = %q, want %q", gotWD, targetWD)
		}
	}
	if got := currentCLITmuxTimeoutOverride(); got != 250*time.Millisecond {
		t.Fatalf("timeout override after apply = %v, want %v", got, 250*time.Millisecond)
	}

	restore()

	gotWD, err = os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() after restore error = %v", err)
	}
	if gotWD != originalWD {
		gotCanonical, err := filepath.EvalSymlinks(gotWD)
		if err != nil {
			t.Fatalf("EvalSymlinks(got restored cwd) error = %v", err)
		}
		wantCanonical, err := filepath.EvalSymlinks(originalWD)
		if err != nil {
			t.Fatalf("EvalSymlinks(original cwd) error = %v", err)
		}
		if gotCanonical != wantCanonical {
			t.Fatalf("cwd after restore = %q, want %q", gotWD, originalWD)
		}
	}
	if got := currentCLITmuxTimeoutOverride(); got != 0 {
		t.Fatalf("timeout override after restore = %v, want 0", got)
	}
}

func TestApplyRunGlobalsInvalidCwdRestoresTimeout(t *testing.T) {
	prevTimeout := setCLITmuxTimeoutOverride(0)
	defer setCLITmuxTimeoutOverride(prevTimeout)

	_, err := applyRunGlobals(GlobalFlags{
		Cwd:     filepath.Join(t.TempDir(), "missing"),
		Timeout: time.Second,
	})
	if err == nil {
		t.Fatalf("expected error for invalid cwd")
	}

	if got := currentCLITmuxTimeoutOverride(); got != 0 {
		t.Fatalf("timeout override after invalid cwd = %v, want 0", got)
	}
}

func TestRouteWorkspaceJSON(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode string
		wantMsg  string
	}{
		{
			name:     "empty args",
			args:     nil,
			wantCode: "usage_error",
			wantMsg:  "Usage: amux workspace",
		},
		{
			name:     "unknown subcommand",
			args:     []string{"bogus"},
			wantCode: "unknown_command",
			wantMsg:  "Unknown workspace subcommand: bogus",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w bytes.Buffer
			var wErr bytes.Buffer
			gf := GlobalFlags{JSON: true}
			code := routeWorkspace(&w, &wErr, gf, tt.args, "test")
			if code != ExitUsage {
				t.Fatalf("exit code = %d, want %d", code, ExitUsage)
			}
			var env Envelope
			if err := json.Unmarshal(w.Bytes(), &env); err != nil {
				t.Fatalf("failed to parse JSON output: %v\nraw: %s", err, w.String())
			}
			if env.OK {
				t.Fatalf("expected ok=false")
			}
			if env.Error.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q", env.Error.Code, tt.wantCode)
			}
			if !strings.Contains(env.Error.Message, tt.wantMsg) {
				t.Errorf("error message = %q, want to contain %q", env.Error.Message, tt.wantMsg)
			}
		})
	}
}

func TestRouteAgentJSON(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode string
		wantMsg  string
	}{
		{
			name:     "empty args",
			args:     nil,
			wantCode: "usage_error",
			wantMsg:  "Usage: amux agent",
		},
		{
			name:     "unknown subcommand",
			args:     []string{"bogus"},
			wantCode: "unknown_command",
			wantMsg:  "Unknown agent subcommand: bogus",
		},
		{
			name:     "agent job missing subcommand",
			args:     []string{"job"},
			wantCode: "usage_error",
			wantMsg:  "Usage: amux agent job",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w bytes.Buffer
			var wErr bytes.Buffer
			gf := GlobalFlags{JSON: true}
			code := routeAgent(&w, &wErr, gf, tt.args, "test")
			if code != ExitUsage {
				t.Fatalf("exit code = %d, want %d", code, ExitUsage)
			}
			var env Envelope
			if err := json.Unmarshal(w.Bytes(), &env); err != nil {
				t.Fatalf("failed to parse JSON output: %v\nraw: %s", err, w.String())
			}
			if env.OK {
				t.Fatalf("expected ok=false")
			}
			if env.Error.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q", env.Error.Code, tt.wantCode)
			}
			if !strings.Contains(env.Error.Message, tt.wantMsg) {
				t.Errorf("error message = %q, want to contain %q", env.Error.Message, tt.wantMsg)
			}
		})
	}
}

func TestCommandFromArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "empty", args: nil, want: ""},
		{name: "single command", args: []string{"status"}, want: "status"},
		{name: "agent send", args: []string{"agent", "send", "s"}, want: "agent send"},
		{name: "agent job status", args: []string{"agent", "job", "status", "id"}, want: "agent job status"},
		{name: "agent job wait", args: []string{"agent", "job", "wait", "id"}, want: "agent job wait"},
		{name: "workspace list", args: []string{"workspace", "list"}, want: "workspace list"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commandFromArgs(tt.args); got != tt.want {
				t.Fatalf("commandFromArgs(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

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
