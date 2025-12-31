package validation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateWorktreeName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple", "feature", false},
		{"valid with number", "feature1", false},
		{"valid with dash", "feature-1", false},
		{"valid with underscore", "feature_1", false},
		{"valid with dot", "feature.1", false},
		{"starts with number", "1feature", false},
		{"empty", "", true},
		{"only spaces", "   ", true},
		{"starts with dash", "-feature", true},
		{"starts with dot", ".feature", true},
		{"contains space", "feature branch", true},
		{"reserved HEAD", "HEAD", true},
		{"reserved lowercase head", "head", true},
		{"too long", string(make([]byte, 101)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorktreeName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWorktreeName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateProjectPath(t *testing.T) {
	// Create a temp git repo
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	_ = os.Mkdir(gitDir, 0755)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"valid git repo", tmpDir, false},
		{"empty path", "", true},
		{"non-existent", "/path/that/does/not/exist", true},
		{"not a git repo", t.TempDir(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProjectPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProjectPath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestValidateBaseRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantErr bool
	}{
		{"valid HEAD", "HEAD", false},
		{"valid branch", "main", false},
		{"valid remote branch", "origin/main", false},
		{"valid refs format", "refs/heads/main", false},
		{"empty", "", true},
		{"contains ..", "main..HEAD", true},
		{"contains space", "main branch", true},
		{"contains tab", "main\tbranch", true},
		{"too long", string(make([]byte, 201)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBaseRef(tt.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBaseRef(%q) error = %v, wantErr %v", tt.ref, err, tt.wantErr)
			}
		})
	}
}

func TestValidateAssistant(t *testing.T) {
	tests := []struct {
		name      string
		assistant string
		wantErr   bool
	}{
		{"claude", "claude", false},
		{"codex", "codex", false},
		{"gemini", "gemini", false},
		{"term", "term", false},
		{"unknown", "gpt4", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAssistant(tt.assistant)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAssistant(%q) error = %v, wantErr %v", tt.assistant, err, tt.wantErr)
			}
		})
	}
}

func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"normal text", "hello world", "hello world"},
		{"with newline", "hello\nworld", "hello\nworld"},
		{"with tab", "hello\tworld", "hello\tworld"},
		{"with leading space", "  hello", "hello"},
		{"with trailing space", "hello  ", "hello"},
		{"with control chars", "hello\x00\x01world", "helloworld"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeInput(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeInput(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidationError(t *testing.T) {
	err := &ValidationError{Field: "name", Message: "cannot be empty"}
	want := "name: cannot be empty"
	if err.Error() != want {
		t.Errorf("Error() = %v, want %v", err.Error(), want)
	}
}
