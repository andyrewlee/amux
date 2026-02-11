package validation

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ValidationError represents a validation failure
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// workspaceNameRegex matches valid workspace/branch names
var workspaceNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// ValidateWorkspaceName validates a workspace name
func ValidateWorkspaceName(name string) error {
	name = strings.TrimSpace(name)

	if name == "" {
		return &ValidationError{Field: "name", Message: "name cannot be empty"}
	}

	if len(name) > 100 {
		return &ValidationError{Field: "name", Message: "name too long (max 100 characters)"}
	}

	if !workspaceNameRegex.MatchString(name) {
		return &ValidationError{Field: "name", Message: "name must start with letter/number and contain only letters, numbers, dots, dashes, or underscores"}
	}

	// Disallow consecutive dots (..)
	if strings.Contains(name, "..") {
		return &ValidationError{Field: "name", Message: "name cannot contain consecutive dots"}
	}

	// Disallow .lock suffix
	if strings.HasSuffix(name, ".lock") {
		return &ValidationError{Field: "name", Message: "name cannot end with .lock"}
	}

	// Disallow @{ sequence
	if strings.Contains(name, "@{") {
		return &ValidationError{Field: "name", Message: "name cannot contain '@{'"}
	}

	// Check for reserved names
	reserved := []string{".", "..", "HEAD", "FETCH_HEAD", "ORIG_HEAD", "MERGE_HEAD", "CHERRY_PICK_HEAD"}
	for _, r := range reserved {
		if strings.EqualFold(name, r) {
			return &ValidationError{Field: "name", Message: fmt.Sprintf("'%s' is a reserved name", name)}
		}
	}

	return nil
}

// ValidateProjectPath validates a project path
func ValidateProjectPath(path string) error {
	path = strings.TrimSpace(path)

	if path == "" {
		return &ValidationError{Field: "path", Message: "path cannot be empty"}
	}

	// Expand home directory
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return &ValidationError{Field: "path", Message: "cannot resolve home directory"}
		}
		switch {
		case path == "~":
			path = home
		case strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\"):
			path = filepath.Join(home, path[2:])
		}
	}

	// Check if path exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ValidationError{Field: "path", Message: "path does not exist"}
		}
		return &ValidationError{Field: "path", Message: fmt.Sprintf("cannot access path: %v", err)}
	}

	if !info.IsDir() {
		return &ValidationError{Field: "path", Message: "path is not a directory"}
	}

	// Check for .git
	gitPath := filepath.Join(path, ".git")
	if _, err := os.Stat(gitPath); os.IsNotExist(err) {
		return &ValidationError{Field: "path", Message: "path is not a git repository"}
	}

	return nil
}

// ValidateBaseRef validates a git base reference
func ValidateBaseRef(ref string) error {
	ref = strings.TrimSpace(ref)

	if ref == "" {
		return &ValidationError{Field: "base", Message: "base reference cannot be empty"}
	}

	if len(ref) > 200 {
		return &ValidationError{Field: "base", Message: "base reference too long"}
	}

	// Basic format validation - allow common patterns
	// refs/heads/*, refs/remotes/*, origin/*, HEAD, branch names
	if strings.Contains(ref, "..") {
		return &ValidationError{Field: "base", Message: "base reference cannot contain '..'"}
	}

	if strings.ContainsAny(ref, " \t\n\r") {
		return &ValidationError{Field: "base", Message: "base reference cannot contain whitespace"}
	}

	return nil
}

// ValidateAssistant validates an assistant name
func ValidateAssistant(assistant string) error {
	valid := map[string]bool{
		"claude":   true,
		"codex":    true,
		"gemini":   true,
		"amp":      true,
		"opencode": true,
		"droid":    true,
		"cline":    true,
		"cursor":   true,
		"pi":       true,
		"term":     true,
	}

	if !valid[assistant] {
		return &ValidationError{Field: "assistant", Message: fmt.Sprintf("unknown assistant '%s'", assistant)}
	}

	return nil
}

// SanitizeInput removes potentially dangerous characters from input
func SanitizeInput(input string) string {
	// Remove control characters
	input = strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, input)

	return strings.TrimSpace(input)
}
