package sandbox

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ShellQuote safely quotes a string for use in shell commands.
// It uses single quotes and escapes any embedded single quotes.
// This is the safest method for POSIX shells.
func ShellQuote(s string) string {
	// Replace single quotes with '\'' (end quote, escaped quote, start quote)
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return "'" + escaped + "'"
}

// ShellQuoteAll quotes multiple strings for shell usage.
func ShellQuoteAll(args []string) []string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = ShellQuote(arg)
	}
	return quoted
}

// ShellJoin joins arguments into a single shell-safe command string.
func ShellJoin(args []string) string {
	return strings.Join(ShellQuoteAll(args), " ")
}

// ShellCommand builds a shell command safely.
type ShellCommand struct {
	parts []string
}

// NewShellCommand creates a new ShellCommand builder.
func NewShellCommand(cmd string) *ShellCommand {
	return &ShellCommand{parts: []string{cmd}}
}

// Arg adds a single argument, properly quoted.
func (c *ShellCommand) Arg(arg string) *ShellCommand {
	c.parts = append(c.parts, ShellQuote(arg))
	return c
}

// Args adds multiple arguments, properly quoted.
func (c *ShellCommand) Args(args ...string) *ShellCommand {
	for _, arg := range args {
		c.parts = append(c.parts, ShellQuote(arg))
	}
	return c
}

// RawArg adds an argument without quoting (use with caution).
func (c *ShellCommand) RawArg(arg string) *ShellCommand {
	c.parts = append(c.parts, arg)
	return c
}

// Flag adds a flag (e.g., "-v", "--verbose").
func (c *ShellCommand) Flag(flag string) *ShellCommand {
	if isValidFlag(flag) {
		c.parts = append(c.parts, flag)
	}
	return c
}

// FlagWithValue adds a flag with a value (e.g., "-o", "output.txt").
func (c *ShellCommand) FlagWithValue(flag, value string) *ShellCommand {
	if isValidFlag(flag) {
		c.parts = append(c.parts, flag, ShellQuote(value))
	}
	return c
}

// Pipe adds a pipe to another command.
func (c *ShellCommand) Pipe(cmd string) *ShellCommand {
	c.parts = append(c.parts, "|", cmd)
	return c
}

// And adds && to chain commands.
func (c *ShellCommand) And(cmd string) *ShellCommand {
	c.parts = append(c.parts, "&&", cmd)
	return c
}

// Or adds || to chain commands.
func (c *ShellCommand) Or(cmd string) *ShellCommand {
	c.parts = append(c.parts, "||", cmd)
	return c
}

// String returns the complete command string.
func (c *ShellCommand) String() string {
	return strings.Join(c.parts, " ")
}

// isValidFlag checks if a string looks like a valid flag.
var flagPattern = regexp.MustCompile(`^-{1,2}[a-zA-Z][a-zA-Z0-9_-]*$`)

func isValidFlag(s string) bool {
	return flagPattern.MatchString(s)
}

// EnvVar represents a shell environment variable assignment.
type EnvVar struct {
	Key   string
	Value string
}

// EnvVarPattern validates environment variable names.
var envVarPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// IsValidEnvKey checks if a key is a valid environment variable name.
func IsValidEnvKey(key string) bool {
	return envVarPattern.MatchString(key)
}

// BuildEnvExport builds a safe export statement.
func BuildEnvExport(key, value string) string {
	if !IsValidEnvKey(key) {
		return ""
	}
	return fmt.Sprintf("export %s=%s", key, ShellQuote(value))
}

// BuildEnvExports builds multiple export statements.
func BuildEnvExports(env map[string]string) []string {
	exports := make([]string, 0, len(env))
	for key, value := range env {
		if export := BuildEnvExport(key, value); export != "" {
			exports = append(exports, export)
		}
	}
	return exports
}

// BuildEnvAssignment builds an inline environment assignment (VAR=value cmd).
func BuildEnvAssignment(key, value string) string {
	if !IsValidEnvKey(key) {
		return ""
	}
	return fmt.Sprintf("%s=%s", key, ShellQuote(value))
}

// BuildEnvAssignments builds inline environment assignments.
func BuildEnvAssignments(env map[string]string) string {
	assignments := make([]string, 0, len(env))
	for key, value := range env {
		if assignment := BuildEnvAssignment(key, value); assignment != "" {
			assignments = append(assignments, assignment)
		}
	}
	return strings.Join(assignments, " ")
}

// SafeCommands provides pre-built safe command templates.
var SafeCommands = struct {
	// File operations
	MkdirP      func(path string) string
	RmRf        func(path string) string
	RmF         func(path string) string
	Ln          func(target, link string) string
	LnForce     func(target, link string) string
	Touch       func(path string) string
	Cat         func(path string) string
	Test        func(flag, path string) string
	Chmod       func(mode, path string) string
	Chown       func(owner, path string) string
	Cp          func(src, dst string) string
	Mv          func(src, dst string) string
	Stat        func(path string) string
	MkdirParent func(path string) string

	// Archive operations
	TarCreate  func(archive, dir string) string
	TarExtract func(archive, dir string) string

	// Command lookup
	CommandV func(cmd string) string
	Which    func(cmd string) string
}{
	MkdirP: func(path string) string {
		return "mkdir -p " + ShellQuote(path)
	},
	RmRf: func(path string) string {
		return "rm -rf " + ShellQuote(path)
	},
	RmF: func(path string) string {
		return "rm -f " + ShellQuote(path)
	},
	Ln: func(target, link string) string {
		return fmt.Sprintf("ln -s %s %s", ShellQuote(target), ShellQuote(link))
	},
	LnForce: func(target, link string) string {
		return fmt.Sprintf("ln -sfn %s %s", ShellQuote(target), ShellQuote(link))
	},
	Touch: func(path string) string {
		return "touch " + ShellQuote(path)
	},
	Cat: func(path string) string {
		return "cat " + ShellQuote(path)
	},
	Test: func(flag, path string) string {
		if !strings.HasPrefix(flag, "-") || len(flag) != 2 {
			flag = "-e"
		}
		return fmt.Sprintf("test %s %s", flag, ShellQuote(path))
	},
	Chmod: func(mode, path string) string {
		return fmt.Sprintf("chmod %s %s", mode, ShellQuote(path))
	},
	Chown: func(owner, path string) string {
		return fmt.Sprintf("chown %s %s", owner, ShellQuote(path))
	},
	Cp: func(src, dst string) string {
		return fmt.Sprintf("cp %s %s", ShellQuote(src), ShellQuote(dst))
	},
	Mv: func(src, dst string) string {
		return fmt.Sprintf("mv %s %s", ShellQuote(src), ShellQuote(dst))
	},
	Stat: func(path string) string {
		// Use portable stat flags (works on both Linux and macOS)
		return fmt.Sprintf("stat -c %%Y %s 2>/dev/null || stat -f %%m %s 2>/dev/null", ShellQuote(path), ShellQuote(path))
	},
	MkdirParent: func(path string) string {
		return fmt.Sprintf("mkdir -p $(dirname %s)", ShellQuote(path))
	},
	TarCreate: func(archive, dir string) string {
		return fmt.Sprintf("tar -czf %s -C %s .", ShellQuote(archive), ShellQuote(dir))
	},
	TarExtract: func(archive, dir string) string {
		return fmt.Sprintf("tar -xzf %s -C %s", ShellQuote(archive), ShellQuote(dir))
	},
	CommandV: func(cmd string) string {
		return "command -v " + ShellQuote(cmd)
	},
	Which: func(cmd string) string {
		return "which " + ShellQuote(cmd)
	},
}

// RedactSecrets redacts sensitive values from a string for logging.
func RedactSecrets(s string) string {
	// Redact export statements
	exportRe := regexp.MustCompile(`export [A-Za-z_][A-Za-z0-9_]*=('[^']*'|"[^"]*"|[^\s]+)`)
	s = exportRe.ReplaceAllStringFunc(s, func(match string) string {
		parts := strings.SplitN(match, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimPrefix(parts[0], "export ")
			// Check if this looks like a secret
			lowerKey := strings.ToLower(key)
			if strings.Contains(lowerKey, "key") ||
				strings.Contains(lowerKey, "token") ||
				strings.Contains(lowerKey, "secret") ||
				strings.Contains(lowerKey, "password") ||
				strings.Contains(lowerKey, "credential") {
				return fmt.Sprintf("export %s=<redacted>", key)
			}
		}
		return match
	})

	// Redact API keys that look like they're inline
	apiKeyRe := regexp.MustCompile(`(sk-[a-zA-Z0-9]{20,}|ghp_[a-zA-Z0-9]{36}|gho_[a-zA-Z0-9]{36}|ANTHROPIC_[a-zA-Z0-9_]+=)`)
	s = apiKeyRe.ReplaceAllString(s, "<redacted>")

	return s
}

// ValidatePath checks if a path is safe for use in commands.
// Returns an error if the path contains potentially dangerous characters.
func ValidatePath(path string) error {
	// Check for null bytes
	if strings.Contains(path, "\x00") {
		return errors.New("path contains null byte")
	}

	// Check for path traversal attempts that are suspicious
	if strings.Contains(path, "..") && strings.Contains(path, "/") {
		// Allow .. in the middle of paths, but warn about potential traversal
		LogDebug("path contains potential traversal", "path", path)
	}

	// Check for shell metacharacters that shouldn't be in paths
	dangerous := []string{";", "|", "&", "$", "`", "(", ")", "{", "}", "<", ">", "\n", "\r"}
	for _, char := range dangerous {
		if strings.Contains(path, char) {
			return fmt.Errorf("path contains dangerous character: %q", char)
		}
	}

	return nil
}
