package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app"
	"github.com/andyrewlee/amux/internal/logging"
)

// Version info set by GoReleaser via ldflags
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Handle --version flag
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("amux %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
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

	a, err := app.New(version, commit, date)
	if err != nil {
		logging.Error("Failed to initialize app: %v", err)
		fmt.Fprintf(os.Stderr, "Error initializing app: %v\n", err)
		os.Exit(1)
	}
	startPprof()

	p := tea.NewProgram(
		a,
		tea.WithFilter(mouseEventFilter),
	)

	if _, err := p.Run(); err != nil {
		logging.Error("App exited with error: %v", err)
		fmt.Fprintf(os.Stderr, "Error running app: %v\n", err)
		os.Exit(1)
	}

	logging.Info("amux shutdown complete")
}

var lastMouseEvent time.Time

func mouseEventFilter(m tea.Model, msg tea.Msg) tea.Msg {
	switch msg.(type) {
	case tea.MouseWheelMsg, tea.MouseMotionMsg:
		now := time.Now()
		if now.Sub(lastMouseEvent) < 15*time.Millisecond {
			return nil
		}
		lastMouseEvent = now
	}
	return msg
}

func startPprof() {
	raw := strings.TrimSpace(os.Getenv("AMUX_PPROF"))
	if raw == "" {
		return
	}
	switch strings.ToLower(raw) {
	case "0", "false", "no":
		return
	}

	addr := raw
	if raw == "1" || strings.ToLower(raw) == "true" {
		addr = "127.0.0.1:6060"
	} else if _, err := strconv.Atoi(raw); err == nil {
		addr = "127.0.0.1:" + raw
	}

	go func() {
		logging.Info("pprof listening on %s", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			logging.Warn("pprof server stopped: %v", err)
		}
	}()
}
