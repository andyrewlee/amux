// Package ide provides utilities for detecting and opening folders in IDEs.
package ide

import (
	"os/exec"
)

// SupportedIDEs lists CLI commands for supported IDEs in priority order.
// The first available IDE will be used if no preference is configured.
var SupportedIDEs = []string{
	"cursor",  // Cursor (VS Code fork)
	"code",    // VS Code
	"zed",     // Zed
	"pycharm", // PyCharm
	"idea",    // IntelliJ IDEA
	"webstorm", // WebStorm
	"goland",  // GoLand
	"subl",    // Sublime Text
	"atom",    // Atom
	"nvim",    // Neovim
	"vim",     // Vim
}

// Detect returns the CLI command for the first available IDE found in PATH.
// Returns empty string if no supported IDE is found.
func Detect() string {
	for _, ide := range SupportedIDEs {
		if _, err := exec.LookPath(ide); err == nil {
			return ide
		}
	}
	return ""
}

// IsAvailable checks if a specific IDE CLI command is available in PATH.
func IsAvailable(cli string) bool {
	if cli == "" {
		return false
	}
	_, err := exec.LookPath(cli)
	return err == nil
}

// Open opens the specified folder in the given IDE.
// Returns an error if the IDE command fails to start.
func Open(cli, folderPath string) error {
	if cli == "" {
		return nil
	}
	cmd := exec.Command(cli, folderPath)
	return cmd.Start()
}

// GetOrDetect returns the configured IDE if available, otherwise auto-detects.
func GetOrDetect(configured string) string {
	if configured != "" && IsAvailable(configured) {
		return configured
	}
	return Detect()
}
