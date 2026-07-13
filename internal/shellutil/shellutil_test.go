package shellutil

import "testing"

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "''",
		},
		{
			name:     "plain word",
			input:    "hello",
			expected: "'hello'",
		},
		{
			name:     "embedded single quote",
			input:    "it's",
			expected: "'it'\\''s'",
		},
		{
			name:     "multiple embedded single quotes",
			input:    "'''",
			expected: "''\\'''\\'''\\'''",
		},
		{
			name:     "spaces and shell metacharacters stay literal",
			input:    "hello world $HOME `whoami` & | ; > <",
			expected: "'hello world $HOME `whoami` & | ; > <'",
		},
		{
			name:     "path-like value",
			input:    "/usr/local/bin/amux",
			expected: "'/usr/local/bin/amux'",
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
