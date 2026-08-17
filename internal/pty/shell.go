package pty

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/shellutil"
)

const defaultLoginShell = "/bin/bash"

// LoginShellCommandFromEnv builds a safe login-shell exec command from SHELL.
func LoginShellCommandFromEnv() (string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = defaultLoginShell
	}
	return LoginShellCommand(shell)
}

// LoginShellCommand validates and quotes shell for use in a sh -c command.
func LoginShellCommand(shell string) (string, error) {
	if strings.ContainsRune(shell, 0) {
		return "", errors.New("SHELL contains NUL")
	}
	if !filepath.IsAbs(shell) {
		return "", fmt.Errorf("SHELL must be an absolute path: %q", shell)
	}
	return "exec " + shellutil.ShellQuote(shell) + " -l", nil
}

// AugmentedPath returns the current PATH environment variable, augmented with
// common CLI tool directories (such as ~/.opencode/bin, ~/.local/bin, /opt/homebrew/bin)
// if they exist on disk and are not already present in PATH.
func AugmentedPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.Getenv("HOME")
	}
	return AugmentPath(os.Getenv("PATH"), home)
}

// AugmentPath augments the given path string with common tool binary locations
// that exist on disk, avoiding duplicates.
func AugmentPath(currentPath, homeDir string) string {
	sep := string(os.PathListSeparator)
	rawParts := filepath.SplitList(currentPath)
	parts := make([]string, 0, len(rawParts)+13)
	seen := make(map[string]struct{}, len(rawParts)+13)
	for _, p := range rawParts {
		if p != "" {
			clean := filepath.Clean(p)
			if _, exists := seen[clean]; !exists {
				seen[clean] = struct{}{}
				parts = append(parts, p)
			}
		}
	}

	var candidates []string
	if homeDir != "" {
		candidates = append(candidates,
			filepath.Join(homeDir, ".opencode", "bin"),
			filepath.Join(homeDir, ".local", "bin"),
			filepath.Join(homeDir, ".cargo", "bin"),
			filepath.Join(homeDir, ".npm-global", "bin"),
			filepath.Join(homeDir, "go", "bin"),
		)
	}
	candidates = append(candidates,
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	)

	for _, cand := range candidates {
		candClean := filepath.Clean(cand)
		if _, exists := seen[candClean]; exists {
			continue
		}
		if info, err := os.Stat(cand); err == nil && info.IsDir() {
			parts = append(parts, cand)
			seen[candClean] = struct{}{}
		}
	}

	return strings.Join(parts, sep)
}
