package process

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeRepoFile creates a file (and any parent dirs) under root, so tests can
// assert that only existing in-repo files are reported.
func writeRepoFile(t *testing.T, root, relPath string) {
	t.Helper()
	full := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %q: %v", relPath, err)
	}
	if err := os.WriteFile(full, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write %q: %v", relPath, err)
	}
}

func TestReferencesInRepoFiles(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		seed    []string // repo-relative files to create before the scan
		want    []string // expected repo-relative results (OS-separator form)
		wantNil bool     // expect an empty/nil result
	}{
		{
			name: "resolves a relative script reference",
			cmd:  "bash ./scripts/dev.sh",
			seed: []string{"scripts/dev.sh"},
			want: []string{filepath.Join("scripts", "dev.sh")},
		},
		{
			name:    "no path-like token yields nothing",
			cmd:     "npm run build",
			wantNil: true,
		},
		{
			name:    "missing file is not reported",
			cmd:     "bash ./scripts/dev.sh",
			wantNil: true, // nothing seeded, so the file does not exist
		},
		{
			name:    "token escaping the repo is rejected",
			cmd:     "cat ../../etc/passwd",
			wantNil: true,
		},
		{
			name:    "variable expansion is a blind spot",
			cmd:     "bash $SCRIPT",
			seed:    []string{"SCRIPT"}, // even a literal match must not be reported
			wantNil: true,
		},
		{
			name: "single-quoted path is resolved",
			cmd:  "bash './scripts/dev.sh'",
			seed: []string{"scripts/dev.sh"},
			want: []string{filepath.Join("scripts", "dev.sh")},
		},
		{
			name: "bare script extension without a slash resolves",
			cmd:  "sh dev.sh",
			seed: []string{"dev.sh"},
			want: []string{"dev.sh"},
		},
		{
			name: "directory is not reported as a file",
			cmd:  "ls ./scripts",
			seed: []string{"scripts/keep"}, // creates the scripts/ dir
			// "./scripts" resolves to a directory, not a regular file.
			wantNil: true,
		},
		{
			name: "multiple references are de-duplicated",
			cmd:  "bash ./scripts/dev.sh && bash ./scripts/dev.sh",
			seed: []string{"scripts/dev.sh"},
			want: []string{filepath.Join("scripts", "dev.sh")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			for _, f := range tt.seed {
				writeRepoFile(t, repo, f)
			}

			got := ReferencesInRepoFiles(tt.cmd, repo)

			if tt.wantNil {
				if len(got) != 0 {
					t.Fatalf("ReferencesInRepoFiles(%q) = %v, want empty", tt.cmd, got)
				}
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ReferencesInRepoFiles(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestReferencesInRepoFilesEmptyRoot(t *testing.T) {
	if got := ReferencesInRepoFiles("bash ./scripts/dev.sh", ""); len(got) != 0 {
		t.Fatalf("expected empty result for empty repoRoot, got %v", got)
	}
}

func TestCommandIsUnresolvable(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{name: "plain command is resolvable", cmd: "bash ./scripts/dev.sh", want: false},
		{name: "variable expansion", cmd: "bash $SCRIPT", want: true},
		{name: "command substitution dollar-paren", cmd: "bash $(which dev)", want: true},
		{name: "command substitution backtick", cmd: "bash `which dev`", want: true},
		{name: "glob star", cmd: "bash ./scripts/*.sh", want: true},
		{name: "glob question mark", cmd: "bash ./scripts/dev.s?", want: true},
		{name: "empty command", cmd: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CommandIsUnresolvable(tt.cmd); got != tt.want {
				t.Fatalf("CommandIsUnresolvable(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}
