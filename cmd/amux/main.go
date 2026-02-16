//go:build !windows

package main

import (
	"bytes"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/andyrewlee/amux/internal/app"
	"github.com/andyrewlee/amux/internal/cli"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/safego"
)

// Version info set by GoReleaser via ldflags
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// CLI subcommands that route to the headless CLI.
var cliCommands = map[string]bool{
	"status": true, "doctor": true, "logs": true,
	"workspace": true, "agent": true, "session": true, "project": true,
	"capabilities": true,
	"version":      true, "help": true,
}

func main() {
	// Handle --version flag
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("amux %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	sub, parseErr := classifyInvocation(os.Args[1:])
	if parseErr != nil {
		// Let the headless CLI render the canonical parse error response.
		code := cli.Run(os.Args[1:], version, commit, date)
		os.Exit(code)
	}

	// Route to CLI if a known subcommand is given (even with leading global flags).
	if sub != "" {
		if cliCommands[sub] {
			code := cli.Run(os.Args[1:], version, commit, date)
			os.Exit(code)
		}
		if sub == "tui" {
			// Launch TUI unconditionally.
			runTUI()
			return
		}
	}

	// No subcommand: TTY → TUI, non-TTY → delegate to headless CLI.
	if sub == "" {
		launchTUI := shouldLaunchTUI(
			term.IsTerminal(os.Stdin.Fd()),
			term.IsTerminal(os.Stdout.Fd()),
			term.IsTerminal(os.Stderr.Fd()),
		)
		if handled, code := handleNoSubcommand(os.Args[1:], launchTUI); handled {
			os.Exit(code)
		}
		runTUI()
		return
	}

	// Unknown argument: route through CLI for JSON-aware error handling
	code := cli.Run(os.Args[1:], version, commit, date)
	os.Exit(code)
}

func firstCLIArg(args []string) string {
	sub, _ := classifyInvocation(args)
	return sub
}

func classifyInvocation(args []string) (string, error) {
	_, rest, err := cli.ParseGlobalFlags(args)
	if err != nil {
		return "", err
	}
	if len(rest) == 0 {
		return "", nil
	}
	return rest[0], nil
}

func shouldLaunchTUI(stdinIsTTY, stdoutIsTTY, stderrIsTTY bool) bool {
	return stdinIsTTY && stdoutIsTTY && stderrIsTTY
}

func handleNoSubcommand(args []string, launchTUI bool) (bool, int) {
	if len(args) > 0 {
		return true, cli.Run(args, version, commit, date)
	}
	if launchTUI {
		return false, 0
	}
	return true, cli.Run(args, version, commit, date)
}

func runTUI() {
	// Initialize logging
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".amux", "logs")
	if err := logging.Initialize(logDir, logging.LevelDebug); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not initialize logging: %v\n", err)
	}
	defer logging.Close()

	logging.Info("Starting amux")

	startSignalDebug()

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
	a.SetMsgSender(p.Send)

	if _, err := p.Run(); err != nil {
		logging.Error("App exited with error: %v", err)
		fmt.Fprintf(os.Stderr, "Error running app: %v\n", err)
		a.CleanupTmuxOnExit()
		a.Shutdown()
		os.Exit(1)
	}
	a.CleanupTmuxOnExit()
	a.Shutdown()

	logging.Info("amux shutdown complete")
}

var (
	lastMouseMotionEvent   time.Time
	lastMouseWheelEvent    time.Time
	lastMouseX, lastMouseY int
)

func mouseEventFilter(m tea.Model, msg tea.Msg) tea.Msg {
	switch msg := msg.(type) {
	case tea.MouseMotionMsg:
		// Always allow if position changed
		if msg.X != lastMouseX || msg.Y != lastMouseY {
			lastMouseX = msg.X
			lastMouseY = msg.Y
			lastMouseMotionEvent = time.Now()
			return msg
		}
		// Same position - apply time throttle
		now := time.Now()
		if now.Sub(lastMouseMotionEvent) < 15*time.Millisecond {
			return nil
		}
		lastMouseMotionEvent = now
	case tea.MouseWheelMsg:
		now := time.Now()
		if now.Sub(lastMouseWheelEvent) < 15*time.Millisecond {
			return nil
		}
		lastMouseWheelEvent = now
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

	safego.Go("pprof", func() {
		logging.Info("pprof listening on %s", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			logging.Warn("pprof server stopped: %v", err)
		}
	})
}

// startSignalDebug registers a SIGUSR1 handler for debug goroutine dumps.
// The goroutine and signal handler intentionally live for the process lifetime
// since this is only active in dev builds or when AMUX_DEBUG_SIGNALS is set.
func startSignalDebug() {
	if version != "dev" && strings.TrimSpace(os.Getenv("AMUX_DEBUG_SIGNALS")) == "" {
		return
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	safego.Go("signal-debug", func() {
		for range ch {
			var buf bytes.Buffer
			if err := pprof.Lookup("goroutine").WriteTo(&buf, 2); err != nil {
				logging.Warn("Failed to write goroutine dump: %v", err)
				continue
			}
			logging.Warn("GOROUTINE DUMP\n%s", buf.String())
		}
	})
}
