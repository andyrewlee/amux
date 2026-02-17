package cli

import (
	"reflect"
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
		{
			name:     "session prune older-than preserved",
			args:     []string{"session", "prune", "--older-than", "1h", "--json"},
			wantGF:   GlobalFlags{JSON: true},
			wantRest: []string{"session", "prune", "--older-than", "1h"},
		},
		{
			name:     "session prune global between command and subcommand",
			args:     []string{"session", "--json", "prune", "--older-than", "30m"},
			wantGF:   GlobalFlags{JSON: true},
			wantRest: []string{"session", "prune", "--older-than", "30m"},
		},
		{
			name:     "terminal run local text value that looks global is preserved",
			args:     []string{"terminal", "run", "--workspace", "0123456789abcdef", "--text", "--json"},
			wantGF:   GlobalFlags{},
			wantRest: []string{"terminal", "run", "--workspace", "0123456789abcdef", "--text", "--json"},
		},
		{
			name:     "terminal run global between command and subcommand extracted",
			args:     []string{"terminal", "--json", "run", "--workspace", "0123456789abcdef", "--text", "npm run dev"},
			wantGF:   GlobalFlags{JSON: true},
			wantRest: []string{"terminal", "run", "--workspace", "0123456789abcdef", "--text", "npm run dev"},
		},
		{
			name:     "terminal run preserves unquoted command tail that looks global",
			args:     []string{"terminal", "run", "--workspace", "0123456789abcdef", "--text", "npm", "--quiet"},
			wantGF:   GlobalFlags{},
			wantRest: []string{"terminal", "run", "--workspace", "0123456789abcdef", "--text", "npm", "--quiet"},
		},
		{
			name:     "terminal run preserves command tail after text equals form",
			args:     []string{"terminal", "run", "--workspace", "0123456789abcdef", "--text=npm", "--cwd"},
			wantGF:   GlobalFlags{},
			wantRest: []string{"terminal", "run", "--workspace", "0123456789abcdef", "--text=npm", "--cwd"},
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
