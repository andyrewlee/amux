package pty

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	return "exec " + shellQuote(shell) + " -l", nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
