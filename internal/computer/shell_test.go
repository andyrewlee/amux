package computer

import (
	"testing"
)

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple string",
			input:    "hello",
			expected: "'hello'",
		},
		{
			name:     "string with spaces",
			input:    "hello world",
			expected: "'hello world'",
		},
		{
			name:     "string with single quote",
			input:    "it's",
			expected: "'it'\\''s'",
		},
		{
			name:     "string with multiple single quotes",
			input:    "it's a 'test'",
			expected: "'it'\\''s a '\\''test'\\'''",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "''",
		},
		{
			name:     "string with special chars",
			input:    "hello; rm -rf /",
			expected: "'hello; rm -rf /'",
		},
		{
			name:     "string with newline",
			input:    "hello\nworld",
			expected: "'hello\nworld'",
		},
		{
			name:     "string with dollar sign",
			input:    "$HOME",
			expected: "'$HOME'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShellQuote(tt.input)
			if result != tt.expected {
				t.Errorf("ShellQuote(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestShellQuoteAll(t *testing.T) {
	input := []string{"hello", "world", "it's"}
	expected := []string{"'hello'", "'world'", "'it'\\''s'"}

	result := ShellQuoteAll(input)
	if len(result) != len(expected) {
		t.Errorf("ShellQuoteAll length = %d, want %d", len(result), len(expected))
		return
	}

	for i, v := range result {
		if v != expected[i] {
			t.Errorf("ShellQuoteAll[%d] = %q, want %q", i, v, expected[i])
		}
	}
}

func TestShellJoin(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{
			name:     "simple args",
			input:    []string{"echo", "hello", "world"},
			expected: "'echo' 'hello' 'world'",
		},
		{
			name:     "args with spaces",
			input:    []string{"echo", "hello world"},
			expected: "'echo' 'hello world'",
		},
		{
			name:     "empty args",
			input:    []string{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShellJoin(tt.input)
			if result != tt.expected {
				t.Errorf("ShellJoin(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsValidEnvKey(t *testing.T) {
	tests := []struct {
		key   string
		valid bool
	}{
		{"HOME", true},
		{"PATH", true},
		{"MY_VAR", true},
		{"_PRIVATE", true},
		{"VAR123", true},
		{"123VAR", false},
		{"VAR-NAME", false},
		{"VAR.NAME", false},
		{"", false},
		{"$VAR", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := IsValidEnvKey(tt.key)
			if result != tt.valid {
				t.Errorf("IsValidEnvKey(%q) = %v, want %v", tt.key, result, tt.valid)
			}
		})
	}
}

func TestBuildEnvExport(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		expected string
	}{
		{
			name:     "simple value",
			key:      "HOME",
			value:    "/home/user",
			expected: "export HOME='/home/user'",
		},
		{
			name:     "value with spaces",
			key:      "MY_VAR",
			value:    "hello world",
			expected: "export MY_VAR='hello world'",
		},
		{
			name:     "invalid key",
			key:      "123INVALID",
			value:    "test",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildEnvExport(tt.key, tt.value)
			if result != tt.expected {
				t.Errorf("BuildEnvExport(%q, %q) = %q, want %q", tt.key, tt.value, result, tt.expected)
			}
		})
	}
}

func TestSafeCommands(t *testing.T) {
	tests := []struct {
		name     string
		fn       func() string
		contains string
	}{
		{
			name:     "MkdirP",
			fn:       func() string { return SafeCommands.MkdirP("/test/path") },
			contains: "mkdir -p",
		},
		{
			name:     "RmRf",
			fn:       func() string { return SafeCommands.RmRf("/test/path") },
			contains: "rm -rf",
		},
		{
			name:     "RmF",
			fn:       func() string { return SafeCommands.RmF("/test/file") },
			contains: "rm -f",
		},
		{
			name:     "Touch",
			fn:       func() string { return SafeCommands.Touch("/test/file") },
			contains: "touch",
		},
		{
			name:     "Cat",
			fn:       func() string { return SafeCommands.Cat("/test/file") },
			contains: "cat",
		},
		{
			name:     "TarCreate",
			fn:       func() string { return SafeCommands.TarCreate("/archive.tgz", "/source") },
			contains: "tar -czf",
		},
		{
			name:     "TarExtract",
			fn:       func() string { return SafeCommands.TarExtract("/archive.tgz", "/dest") },
			contains: "tar -xzf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fn()
			if result == "" {
				t.Error("Expected non-empty command")
			}
			if !contains(result, tt.contains) {
				t.Errorf("Command %q should contain %q", result, tt.contains)
			}
		})
	}
}

func TestSafeCommandsInjectionPrevention(t *testing.T) {
	// Test that shell metacharacters are properly quoted
	malicious := "/test; rm -rf /"

	cmd := SafeCommands.Cat(malicious)
	// The semicolon should be inside quotes, not a command separator
	if contains(cmd, "rm -rf") && !contains(cmd, "'") {
		t.Errorf("SafeCommands.Cat did not properly quote malicious input: %s", cmd)
	}
}

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		shouldMatch bool // check if the key is redacted, not exact match
	}{
		{
			name:        "API key export is redacted",
			input:       "export ANTHROPIC_API_KEY='sk-ant-12345'",
			shouldMatch: true,
		},
		{
			name:        "no secrets unchanged",
			input:       "export HOME='/home/user'",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactSecrets(tt.input)
			hasRedacted := contains(result, "<redacted>")
			if tt.shouldMatch && !hasRedacted {
				t.Errorf("RedactSecrets(%q) = %q, expected to contain <redacted>", tt.input, result)
			}
			if !tt.shouldMatch && hasRedacted {
				t.Errorf("RedactSecrets(%q) = %q, did not expect redaction", tt.input, result)
			}
		})
	}
}

func TestValidatePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid simple path",
			path:    "/home/user/file.txt",
			wantErr: false,
		},
		{
			name:    "valid path with spaces",
			path:    "/home/user/my file.txt",
			wantErr: false,
		},
		{
			name:    "null byte",
			path:    "/home/user/\x00file",
			wantErr: true,
		},
		{
			name:    "semicolon",
			path:    "/home/user;rm -rf /",
			wantErr: true,
		},
		{
			name:    "pipe",
			path:    "/home/user|cat /etc/passwd",
			wantErr: true,
		},
		{
			name:    "backtick",
			path:    "/home/`whoami`",
			wantErr: true,
		},
		{
			name:    "dollar sign",
			path:    "/home/$USER",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

// contains checks if s contains substr
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
