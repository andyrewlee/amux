//go:build !windows

package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func resetMouseFilterState() {
	lastMouseMotionEvent = time.Time{}
	lastMouseWheelEvent = time.Time{}
	lastMouseX = 0
	lastMouseY = 0
}

func TestMouseWheelNotThrottledByMotion(t *testing.T) {
	resetMouseFilterState()

	motion := tea.MouseMotionMsg{X: 10, Y: 10, Button: tea.MouseLeft}
	if mouseEventFilter(nil, motion) == nil {
		t.Fatalf("expected motion event to pass through")
	}

	wheel := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown}
	if mouseEventFilter(nil, wheel) == nil {
		t.Fatalf("expected wheel event to pass through after motion")
	}
}

func TestMouseWheelThrottleIndependent(t *testing.T) {
	resetMouseFilterState()

	wheel := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown}
	if mouseEventFilter(nil, wheel) == nil {
		t.Fatalf("expected first wheel event to pass through")
	}
	if mouseEventFilter(nil, wheel) != nil {
		t.Fatalf("expected second wheel event to be throttled")
	}
}

func TestFirstCLIArgSkipsLeadingGlobalFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "json status",
			args: []string{"--json", "status"},
			want: "status",
		},
		{
			name: "quiet doctor",
			args: []string{"-q", "doctor"},
			want: "doctor",
		},
		{
			name: "cwd workspace list",
			args: []string{"--cwd", "/tmp/repo", "workspace", "list"},
			want: "workspace",
		},
		{
			name: "timeout logs tail",
			args: []string{"--timeout=5s", "logs", "tail"},
			want: "logs",
		},
		{
			name: "request-id capabilities",
			args: []string{"--request-id", "req-1", "capabilities"},
			want: "capabilities",
		},
		{
			name: "only globals",
			args: []string{"--json"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstCLIArg(tt.args); got != tt.want {
				t.Fatalf("firstCLIArg() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyInvocation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantSub string
		wantErr bool
	}{
		{
			name:    "global-only",
			args:    []string{"--json"},
			wantSub: "",
		},
		{
			name:    "global-prefix-with-subcommand",
			args:    []string{"--json", "status"},
			wantSub: "status",
		},
		{
			name:    "malformed-timeout",
			args:    []string{"--timeout=abc"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSub, err := classifyInvocation(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("classifyInvocation() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("classifyInvocation() unexpected error: %v", err)
			}
			if gotSub != tt.wantSub {
				t.Fatalf("classifyInvocation() = %q, want %q", gotSub, tt.wantSub)
			}
		})
	}
}

func TestClassifyDispatch(t *testing.T) {
	tests := []struct {
		name string
		sub  string
		want dispatchTarget
	}{
		{name: "cobra status", sub: "status", want: dispatchTargetCobra},
		{name: "cobra doctor", sub: "doctor", want: dispatchTargetCobra},
		{name: "legacy logs", sub: "logs", want: dispatchTargetLegacy},
		{name: "legacy workspace", sub: "workspace", want: dispatchTargetLegacy},
		{name: "cobra sandbox", sub: "sandbox", want: dispatchTargetCobra},
		{name: "cobra auth", sub: "auth", want: dispatchTargetCobra},
		{name: "cobra alias", sub: "claude", want: dispatchTargetCobra},
		{name: "tui command", sub: "tui", want: dispatchTargetTUI},
		{name: "unknown", sub: "does-not-exist", want: dispatchTargetUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyDispatch(tt.sub); got != tt.want {
				t.Fatalf("classifyDispatch(%q) = %v, want %v", tt.sub, got, tt.want)
			}
		})
	}
}

func TestPrepareCobraDispatchArgs(t *testing.T) {
	tmp := t.TempDir()

	tests := []struct {
		name         string
		args         []string
		sub          string
		wantCwd      string
		wantReqID    string
		wantTimeout  time.Duration
		wantCobraArg []string
	}{
		{
			name:         "strips leading globals and remaps json for status",
			args:         []string{"--json", "--cwd", tmp, "status"},
			sub:          "status",
			wantCwd:      tmp,
			wantCobraArg: []string{"status", "--json"},
		},
		{
			name:         "status local json preserved via remap",
			args:         []string{"status", "--json"},
			sub:          "status",
			wantCwd:      "",
			wantCobraArg: []string{"status", "--json"},
		},
		{
			name:         "doctor remaps leading json so unsupported flag is explicit",
			args:         []string{"--json", "doctor", "--deep"},
			sub:          "doctor",
			wantCwd:      "",
			wantCobraArg: []string{"doctor", "--json", "--deep"},
		},
		{
			name:         "doctor json remap does not satisfy missing flag value",
			args:         []string{"--json", "doctor", "--agent"},
			sub:          "doctor",
			wantCwd:      "",
			wantCobraArg: []string{"doctor", "--json", "--agent"},
		},
		{
			name:         "status strips post-command cwd global",
			args:         []string{"status", "--cwd", tmp},
			sub:          "status",
			wantCwd:      tmp,
			wantCobraArg: []string{"status"},
		},
		{
			name:         "doctor strips post-command timeout global",
			args:         []string{"doctor", "--timeout", "1s"},
			sub:          "doctor",
			wantTimeout:  time.Second,
			wantCobraArg: []string{"doctor"},
		},
		{
			name:         "doctor strips post-command request-id global",
			args:         []string{"doctor", "--request-id", "req-123"},
			sub:          "doctor",
			wantCwd:      "",
			wantReqID:    "req-123",
			wantCobraArg: []string{"doctor"},
		},
		{
			name:         "exec passthrough after double-dash is preserved",
			args:         []string{"exec", "--", "rg", "--json"},
			sub:          "exec",
			wantCwd:      "",
			wantCobraArg: []string{"exec", "--", "rg", "--json"},
		},
		{
			name:         "alias passthrough is preserved",
			args:         []string{"claude", "--", "--quiet"},
			sub:          "claude",
			wantCwd:      "",
			wantCobraArg: []string{"claude", "--", "--quiet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotGF, gotArgs, err := prepareCobraDispatchArgs(tt.args, tt.sub)
			if err != nil {
				t.Fatalf("prepareCobraDispatchArgs() error = %v", err)
			}
			if gotGF.Cwd != tt.wantCwd {
				t.Fatalf("cwd = %q, want %q", gotGF.Cwd, tt.wantCwd)
			}
			if gotGF.RequestID != tt.wantReqID {
				t.Fatalf("request-id = %q, want %q", gotGF.RequestID, tt.wantReqID)
			}
			if gotGF.Timeout != tt.wantTimeout {
				t.Fatalf("timeout = %v, want %v", gotGF.Timeout, tt.wantTimeout)
			}
			if strings.Join(gotArgs, "\x00") != strings.Join(tt.wantCobraArg, "\x00") {
				t.Fatalf("cobra args = %v, want %v", gotArgs, tt.wantCobraArg)
			}
		})
	}
}

func TestShouldRouteLegacyJSONContract(t *testing.T) {
	tests := []struct {
		name string
		sub  string
		args []string
		want bool
	}{
		{
			name: "status with leading json",
			sub:  "status",
			args: []string{"--json", "status"},
			want: true,
		},
		{
			name: "status with trailing json",
			sub:  "status",
			args: []string{"status", "--json"},
			want: true,
		},
		{
			name: "doctor with request-id and json",
			sub:  "doctor",
			args: []string{"doctor", "--request-id", "req-1", "--json"},
			want: true,
		},
		{
			name: "doctor without json",
			sub:  "doctor",
			args: []string{"doctor", "--deep"},
			want: false,
		},
		{
			name: "non-compat command with json",
			sub:  "exec",
			args: []string{"--json", "exec", "--", "echo", "hi"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRouteLegacyJSONContract(tt.sub, tt.args); got != tt.want {
				t.Fatalf("shouldRouteLegacyJSONContract(%q, %v) = %v, want %v", tt.sub, tt.args, got, tt.want)
			}
		})
	}
}

func TestHandleNoSubcommandNonTTYRoutesThroughCLIJSON(t *testing.T) {
	code, stdout, stderr := runHandleNoSubcommandCaptured(t, []string{"--json"}, false)
	if code != 2 {
		t.Fatalf("handleNoSubcommand() code = %d, want 2", code)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected empty stderr in --json mode, got %q", stderr)
	}

	var env struct {
		OK    bool `json:"ok"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, stdout)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
}

func TestHandleNoSubcommandTTYSignalsTUIFlow(t *testing.T) {
	handled, code := handleNoSubcommand(nil, true)
	if handled {
		t.Fatalf("expected handled=false when stdin is a TTY")
	}
	if code != 0 {
		t.Fatalf("expected code=0 for TTY path, got %d", code)
	}
}

func TestHandleNoSubcommandTTYWithJSONRoutesThroughCLI(t *testing.T) {
	code, stdout, stderr := runHandleNoSubcommandCaptured(t, []string{"--json"}, true)
	if code != 2 {
		t.Fatalf("handleNoSubcommand() code = %d, want 2", code)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected empty stderr in --json mode, got %q", stderr)
	}

	var env struct {
		OK    bool `json:"ok"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, stdout)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
}

func TestShouldLaunchTUIRequiresAllTTYStreams(t *testing.T) {
	tests := []struct {
		name      string
		stdinTTY  bool
		stdoutTTY bool
		stderrTTY bool
		want      bool
	}{
		{
			name:      "all tty",
			stdinTTY:  true,
			stdoutTTY: true,
			stderrTTY: true,
			want:      true,
		},
		{
			name:      "stdout redirected",
			stdinTTY:  true,
			stdoutTTY: false,
			stderrTTY: true,
			want:      false,
		},
		{
			name:      "stdin non tty",
			stdinTTY:  false,
			stdoutTTY: true,
			stderrTTY: true,
			want:      false,
		},
		{
			name:      "stderr non tty",
			stdinTTY:  true,
			stdoutTTY: true,
			stderrTTY: false,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldLaunchTUI(tt.stdinTTY, tt.stdoutTTY, tt.stderrTTY); got != tt.want {
				t.Fatalf("shouldLaunchTUI() = %v, want %v", got, tt.want)
			}
		})
	}
}

func runHandleNoSubcommandCaptured(t *testing.T, args []string, stdinIsTTY bool) (int, string, string) {
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
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	handled, code := handleNoSubcommand(args, stdinIsTTY)
	if !handled {
		t.Fatalf("expected handled=true for non-TTY path")
	}

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
