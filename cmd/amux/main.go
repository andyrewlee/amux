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

// legacyCLICommands route to the JSON-capable headless CLI.
var legacyCLICommands = map[string]bool{
	"workspace": true, "agent": true, "session": true, "project": true,
	"terminal":     true,
	"logs":         true,
	"capabilities": true,
	"version":      true, "help": true,
}

// cobraCLICommands route to the sandbox-oriented Cobra command tree.
var cobraCLICommands = map[string]bool{
	"auth":       true,
	"sandbox":    true,
	"setup":      true,
	"snapshot":   true,
	"settings":   true,
	"ssh":        true,
	"exec":       true,
	"status":     true,
	"doctor":     true,
	"completion": true,
	"explain":    true,
	"claude":     true,
	"codex":      true,
	"opencode":   true,
	"amp":        true,
	"gemini":     true,
	"droid":      true,
	"shell":      true,
	"ls":         true,
	"rm":         true,
}

type dispatchTarget int

const (
	dispatchTargetLegacy dispatchTarget = iota
	dispatchTargetCobra
	dispatchTargetTUI
	dispatchTargetUnknown
)

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

	// Route to the appropriate command surface if a known subcommand is given
	// (even with leading global flags).
	if sub != "" {
		switch classifyDispatch(sub) {
		case dispatchTargetLegacy:
			code := cli.Run(os.Args[1:], version, commit, date)
			os.Exit(code)
		case dispatchTargetCobra:
			if shouldRouteLegacyJSONContract(sub, os.Args[1:]) {
				code := cli.Run(os.Args[1:], version, commit, date)
				os.Exit(code)
			}
			gf, cobraArgs, err := prepareCobraDispatchArgs(os.Args[1:], sub)
			if err != nil {
				code := cli.Run(os.Args[1:], version, commit, date)
				os.Exit(code)
			}
			if err := applyCobraGlobals(gf, sub); err != nil {
				code := cli.Run(os.Args[1:], version, commit, date)
				os.Exit(code)
			}
			code := cli.RunCobraWithGlobals(cobraArgs, gf)
			os.Exit(code)
		case dispatchTargetTUI:
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

func classifyDispatch(sub string) dispatchTarget {
	if legacyCLICommands[sub] {
		return dispatchTargetLegacy
	}
	if cobraCLICommands[sub] {
		return dispatchTargetCobra
	}
	if sub == "tui" {
		return dispatchTargetTUI
	}
	return dispatchTargetUnknown
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

func shouldRouteLegacyJSONContract(sub string, args []string) bool {
	if sub != "status" && sub != "doctor" {
		return false
	}
	gf, _, err := cli.ParseGlobalFlags(args)
	if err != nil {
		return false
	}
	return gf.JSON
}

func prepareCobraDispatchArgs(args []string, sub string) (cli.GlobalFlags, []string, error) {
	// Preserve passthrough semantics for all regular Cobra commands.
	// Only status/doctor retain compatibility for legacy leading globals.
	if !requiresCobraCompatPreprocess(sub) {
		return cli.GlobalFlags{}, append([]string(nil), args...), nil
	}

	gf, rest, err := cli.ParseGlobalFlags(args)
	if err != nil {
		return gf, nil, err
	}
	if len(rest) == 0 {
		return gf, nil, nil
	}

	cobraArgs := append([]string(nil), rest...)
	// Preserve legacy automation form: `amux --json status`.
	// For doctor, remap consumed --json back into argv so Cobra surfaces
	// a clear unsupported-flag error instead of silently dropping it.
	if (sub == "status" || sub == "doctor") && gf.JSON && !containsArg(cobraArgs[1:], "--json") {
		cobraArgs = insertArgAfterCommand(cobraArgs, "--json")
	}
	return gf, cobraArgs, nil
}

func requiresCobraCompatPreprocess(sub string) bool {
	return sub == "status" || sub == "doctor"
}

func containsArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

func insertArgAfterCommand(args []string, arg string) []string {
	if len(args) == 0 {
		return []string{arg}
	}
	withArg := make([]string, 0, len(args)+1)
	withArg = append(withArg, args[0], arg)
	withArg = append(withArg, args[1:]...)
	return withArg
}

func applyCobraGlobals(gf cli.GlobalFlags, sub string) error {
	if !requiresCobraCompatPreprocess(sub) {
		return nil
	}
	if gf.Cwd == "" {
		return nil
	}
	return os.Chdir(gf.Cwd)
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
	if err := logging.Initialize(logDir, logging.LevelInfo); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not initialize logging: %v\n", err)
	}
	defer logging.Close()

	cleanupStaleTestTmuxSockets()

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
