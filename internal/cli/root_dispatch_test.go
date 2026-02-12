package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

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
