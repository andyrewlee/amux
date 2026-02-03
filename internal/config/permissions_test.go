package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizePermission(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Legacy format should be converted
		{"Bash(ls:*)", "Bash(ls *)"},
		{"Bash(npm run:*)", "Bash(npm run *)"},
		{"Read(~/.ssh:*)", "Read(~/.ssh *)"},
		{"Edit(/src:*)", "Edit(/src *)"},
		{"WebFetch(domain:*)", "WebFetch(domain *)"},

		// New format should remain unchanged
		{"Bash(ls *)", "Bash(ls *)"},
		{"Bash(npm run *)", "Bash(npm run *)"},
		{"Read(~/.ssh/*)", "Read(~/.ssh/*)"},
		{"Read(~/.ssh/**)", "Read(~/.ssh/**)"},

		// Simple tool names should remain unchanged
		{"Bash", "Bash"},
		{"Read", "Read"},
		{"Edit", "Edit"},

		// Exact commands should remain unchanged
		{"Bash(npm run build)", "Bash(npm run build)"},
		{"Read(./.env)", "Read(./.env)"},

		// Edge cases
		{"  Bash(ls:*)  ", "Bash(ls *)"},  // whitespace trimmed
		{"Bash(git * main)", "Bash(git * main)"}, // middle wildcard unchanged
		{"Bash(*)", "Bash(*)"},            // just wildcard unchanged
	}

	for _, tc := range tests {
		result := NormalizePermission(tc.input)
		if result != tc.expected {
			t.Errorf("NormalizePermission(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestDedupe(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: []string{},
		},
		{
			name:     "empty input",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "no duplicates",
			input:    []string{"Bash(ls *)", "Bash(npm *)"},
			expected: []string{"Bash(ls *)", "Bash(npm *)"},
		},
		{
			name:     "exact duplicates",
			input:    []string{"Bash(ls *)", "Bash(ls *)", "Bash(npm *)"},
			expected: []string{"Bash(ls *)", "Bash(npm *)"},
		},
		{
			name:     "legacy and new format duplicates",
			input:    []string{"Bash(ls:*)", "Bash(ls *)"},
			expected: []string{"Bash(ls *)"},
		},
		{
			name:     "normalizes legacy format",
			input:    []string{"Bash(npm:*)", "Read(~/.ssh:*)"},
			expected: []string{"Bash(npm *)", "Read(~/.ssh *)"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := dedupe(tc.input)
			if len(result) != len(tc.expected) {
				t.Errorf("dedupe(%v) = %v, want %v", tc.input, result, tc.expected)
				return
			}
			for i := range result {
				if result[i] != tc.expected[i] {
					t.Errorf("dedupe(%v) = %v, want %v", tc.input, result, tc.expected)
					return
				}
			}
		})
	}
}

func TestDiffPermissions(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		incoming []string
		expected []string
	}{
		{
			name:     "new permission",
			existing: []string{"Bash(ls *)"},
			incoming: []string{"Bash(ls *)", "Bash(npm *)"},
			expected: []string{"Bash(npm *)"},
		},
		{
			name:     "legacy format treated as same",
			existing: []string{"Bash(ls *)"},
			incoming: []string{"Bash(ls:*)", "Bash(npm:*)"},
			expected: []string{"Bash(npm *)"},
		},
		{
			name:     "no new permissions",
			existing: []string{"Bash(ls *)", "Bash(npm *)"},
			incoming: []string{"Bash(ls:*)", "Bash(npm:*)"},
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DiffPermissions(tc.existing, tc.incoming)
			if len(result) != len(tc.expected) {
				t.Errorf("DiffPermissions(%v, %v) = %v, want %v", tc.existing, tc.incoming, result, tc.expected)
				return
			}
			for i := range result {
				if result[i] != tc.expected[i] {
					t.Errorf("DiffPermissions(%v, %v) = %v, want %v", tc.existing, tc.incoming, result, tc.expected)
					return
				}
			}
		})
	}
}

func TestGlobalPermissions_AddAllow(t *testing.T) {
	p := &GlobalPermissions{}

	// Add a permission
	if !p.AddAllow("Bash(ls:*)") {
		t.Error("AddAllow should return true for new permission")
	}
	if len(p.Allow) != 1 || p.Allow[0] != "Bash(ls *)" {
		t.Errorf("Allow list = %v, want [Bash(ls *)]", p.Allow)
	}

	// Try to add same permission in new format (should be rejected as duplicate)
	if p.AddAllow("Bash(ls *)") {
		t.Error("AddAllow should return false for duplicate permission")
	}
	if len(p.Allow) != 1 {
		t.Errorf("Allow list should still have 1 entry, got %d", len(p.Allow))
	}
}

func TestGlobalPermissions_RemoveAllow(t *testing.T) {
	p := &GlobalPermissions{
		Allow: []string{"Bash(ls *)"},
	}

	// Remove using legacy format
	if !p.RemoveAllow("Bash(ls:*)") {
		t.Error("RemoveAllow should return true when removing via legacy format")
	}
	if len(p.Allow) != 0 {
		t.Errorf("Allow list should be empty, got %v", p.Allow)
	}
}

func TestLoadGlobalPermissions_NormalizesOnLoad(t *testing.T) {
	// Create a temp file with legacy format and duplicates
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "global_permissions.json")

	// Write file with legacy formats and duplicates
	data := `{
  "allow": ["Bash(ls:*)", "Bash(ls *)", "Bash(npm:*)"],
  "deny": ["Read(~/.ssh:*)", "Read(~/.ssh *)"]
}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Load and verify normalization + deduplication
	perms, err := LoadGlobalPermissions(path)
	if err != nil {
		t.Fatalf("LoadGlobalPermissions failed: %v", err)
	}

	// Should have deduplicated: "Bash(ls:*)" and "Bash(ls *)" are the same
	expectedAllow := []string{"Bash(ls *)", "Bash(npm *)"}
	if len(perms.Allow) != len(expectedAllow) {
		t.Errorf("Allow = %v, want %v", perms.Allow, expectedAllow)
	}
	for i, v := range perms.Allow {
		if v != expectedAllow[i] {
			t.Errorf("Allow[%d] = %q, want %q", i, v, expectedAllow[i])
		}
	}

	// Same for deny
	expectedDeny := []string{"Read(~/.ssh *)"}
	if len(perms.Deny) != len(expectedDeny) {
		t.Errorf("Deny = %v, want %v", perms.Deny, expectedDeny)
	}
	for i, v := range perms.Deny {
		if v != expectedDeny[i] {
			t.Errorf("Deny[%d] = %q, want %q", i, v, expectedDeny[i])
		}
	}
}
