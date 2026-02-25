package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSandboxRules_Count(t *testing.T) {
	rules := DefaultSandboxRules()
	if len(rules.Rules) != 8 {
		t.Errorf("expected 8 default rules, got %d", len(rules.Rules))
	}
}

func TestDefaultSandboxRules_NoLockedRules(t *testing.T) {
	rules := DefaultSandboxRules()
	for _, r := range rules.Rules {
		if r.Locked {
			t.Errorf("default rules should have no locked rules, found locked: %s", r.Path)
		}
	}
}

func TestLoadSaveSandboxRules_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sandbox_rules.json")

	original := DefaultSandboxRules()
	if err := SaveSandboxRules(path, original); err != nil {
		t.Fatalf("SaveSandboxRules: %v", err)
	}

	loaded, err := LoadSandboxRules(path)
	if err != nil {
		t.Fatalf("LoadSandboxRules: %v", err)
	}

	if len(loaded.Rules) != len(original.Rules) {
		t.Fatalf("rule count mismatch: got %d, want %d", len(loaded.Rules), len(original.Rules))
	}
	for i, r := range loaded.Rules {
		o := original.Rules[i]
		if r.Path != o.Path || r.Action != o.Action || r.PathType != o.PathType || r.Locked != o.Locked || r.Comment != o.Comment {
			t.Errorf("rule %d mismatch:\n  got:  %+v\n  want: %+v", i, r, o)
		}
	}
}

func TestLoadSandboxRules_MissingFileWritesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sandbox_rules.json")

	rules, err := LoadSandboxRules(path)
	if err != nil {
		t.Fatalf("LoadSandboxRules: %v", err)
	}

	// Should return defaults
	if len(rules.Rules) != 8 {
		t.Errorf("expected 8 default rules, got %d", len(rules.Rules))
	}

	// File should now exist
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to be created: %v", err)
	}
}

func TestExpandSandboxPath_Tilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot get home dir")
	}

	tests := []struct {
		input string
		want  string
	}{
		{"~/.ssh", home + "/.ssh"},
		{"~/.ssh/known_hosts", home + "/.ssh/known_hosts"},
		{"~", home},
		{"/private/tmp", "/private/tmp"},
		{`^/dev/`, `^/dev/`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ExpandSandboxPath(tt.input)
			if got != tt.want {
				t.Errorf("ExpandSandboxPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDefaultSandboxRules_ActionTypes(t *testing.T) {
	rules := DefaultSandboxRules()

	denyRead := 0
	allowRead := 0
	allowWrite := 0
	for _, r := range rules.Rules {
		switch r.Action {
		case SandboxDenyRead:
			denyRead++
		case SandboxAllowRead:
			allowRead++
		case SandboxAllowWrite:
			allowWrite++
		}
	}

	if denyRead != 5 {
		t.Errorf("expected 5 deny-read rules, got %d", denyRead)
	}
	if allowRead != 1 {
		t.Errorf("expected 1 allow-read rule, got %d", allowRead)
	}
	if allowWrite != 2 {
		t.Errorf("expected 2 allow-write rules, got %d", allowWrite)
	}
}

func TestDefaultSandboxRules_PathTypes(t *testing.T) {
	rules := DefaultSandboxRules()

	subpath := 0
	literal := 0
	regex := 0
	for _, r := range rules.Rules {
		switch r.PathType {
		case SandboxSubpath:
			subpath++
		case SandboxLiteral:
			literal++
		case SandboxRegex:
			regex++
		}
	}

	if subpath != 7 {
		t.Errorf("expected 7 subpath rules, got %d", subpath)
	}
	if literal != 1 {
		t.Errorf("expected 1 literal rule, got %d", literal)
	}
	if regex != 0 {
		t.Errorf("expected 0 regex rules, got %d", regex)
	}
}
