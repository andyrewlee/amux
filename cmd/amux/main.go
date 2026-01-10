package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/andyrewlee/amux/internal/app"
	"github.com/andyrewlee/amux/internal/cli"
	"github.com/andyrewlee/amux/internal/logging"
)

func main() {
	if len(os.Args) > 1 {
		os.Exit(cli.Run(os.Args[1:]))
	}

	// Initialize logging
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".amux", "logs")
	if err := logging.Initialize(logDir, logging.LevelDebug); err != nil {
		// Logging is optional, continue without it
		fmt.Fprintf(os.Stderr, "Warning: could not initialize logging: %v\n", err)
	}
	defer logging.Close()

	logging.Info("Starting amux")

	a, err := app.New()
	if err != nil {
		logging.Error("Failed to initialize app: %v", err)
		fmt.Fprintf(os.Stderr, "Error initializing app: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(
		a,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		// Note: Bracketed paste is enabled by default in bubbletea v1.x
		// Pasted text arrives as tea.KeyMsg with Paste=true and the full content in Runes
	)

	if _, err := p.Run(); err != nil {
		logging.Error("App exited with error: %v", err)
		fmt.Fprintf(os.Stderr, "Error running app: %v\n", err)
		os.Exit(1)
	}

	logging.Info("amux shutdown complete")
}
