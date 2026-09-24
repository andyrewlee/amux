// Package tmux is a thin wrapper over the tmux CLI used to host agent
// sessions: it creates and kills sessions, captures pane contents and
// scrollback, resizes windows, and reads/writes the @amux_* activity tags
// that coordinate detection and reattach across amux instances. tmux exit
// code 1 ("not found") is treated as an empty/absent result, not an error.
package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/shellutil"
)

type Options struct {
	ServerName      string
	ConfigPath      string
	HideStatus      bool
	DisableMouse    bool
	DefaultTerminal string
	CommandTimeout  time.Duration
}

type SessionTags struct {
	WorkspaceID  string
	TabID        string
	Type         string
	Assistant    string
	CreatedAt    int64 // Unix seconds for fresh create/restart; may be zero for reattach.
	InstanceID   string
	SessionOwner string
	LeaseAtMS    int64
	// WorkspaceName/ProjectName are display-only labels for external
	// orchestrators (`@amux_workspace_name`, `@amux_project`) — never read
	// back by amux and never identity keys. They snapshot the names as of
	// the last session attach: a workspace rename does not re-tag a live
	// session (docs/ORCHESTRATION.md documents the staleness contract).
	WorkspaceName string
	ProjectName   string
}

const tmuxCommandTimeout = 5 * time.Second

func DefaultOptions() Options {
	server := strings.TrimSpace(os.Getenv("AMUX_TMUX_SERVER"))
	if server == "" {
		server = "amux"
	}
	config := strings.TrimSpace(os.Getenv("AMUX_TMUX_CONFIG"))
	if config == "" {
		config = "/dev/null"
	}
	return Options{
		ServerName:      server,
		ConfigPath:      config,
		HideStatus:      true,
		DisableMouse:    true,
		DefaultTerminal: "xterm-256color",
	}
}

var ensureAvailability struct {
	once sync.Once
	err  error
}

// EnsureAvailable reports whether a usable tmux is installed: present on
// PATH and at least version 3.2 (amux's `=` session targets and pane_dead
// formats depend on it). The check runs a `tmux -V` subprocess, so the
// result is cached with sync.Once — this sits on the hot path of every
// tmux operation. An unparseable or failing `tmux -V` fails closed.
func EnsureAvailable() error {
	ensureAvailability.once.Do(func() {
		ensureAvailability.err = checkAvailability()
	})
	return ensureAvailability.err
}

func checkAvailability() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux is not installed.\n\n%s", InstallHint())
	}
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		return fmt.Errorf("tmux is installed but `tmux -V` failed: %w\n\n%s", err, InstallHint())
	}
	raw := strings.TrimSpace(string(out))
	major, minor, ok := parseVersion(raw)
	if !ok {
		return fmt.Errorf("unrecognized tmux version %q; amux requires tmux >= 3.2.\n\n%s", raw, InstallHint())
	}
	if major < 3 || (major == 3 && minor < 2) {
		return fmt.Errorf("found %s, but amux requires tmux >= 3.2.\n\n%s", raw, InstallHint())
	}
	return nil
}

// parseVersion extracts the numeric version from `tmux -V` output such as
// "tmux 3.4", "tmux 3.2a" (letter suffix = patch level, ignored), or
// "tmux next-3.6" (git builds).
func parseVersion(s string) (major, minor int, ok bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "tmux"))
	s = strings.TrimPrefix(s, "next-")
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, 0, false
	}
	if i < len(s) && s[i] == '.' {
		j := i + 1
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j > i+1 {
			minor, _ = strconv.Atoi(s[i+1 : j])
		}
	}
	return major, minor, true
}

func InstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS: brew install tmux"
	case "linux":
		return "Linux: sudo apt install tmux  (or dnf/pacman/etc.)"
	default:
		return "Install tmux and ensure it is on your PATH."
	}
}

func SessionName(parts ...string) string {
	var cleaned []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		part = sanitize(part)
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	if len(cleaned) == 0 {
		return "amux"
	}
	return strings.Join(cleaned, "-")
}

func SessionStateFor(sessionName string, opts Options) (SessionState, error) {
	if sessionName == "" {
		return SessionState{}, nil
	}
	if err := EnsureAvailable(); err != nil {
		return SessionState{}, err
	}
	exists, err := hasSession(sessionName, opts)
	if err != nil || !exists {
		return SessionState{Exists: exists}, err
	}
	live, err := hasLivePane(sessionName, opts)
	return SessionState{Exists: true, HasLivePane: live}, err
}

func tmuxBase(opts Options) string {
	base := "tmux"
	if opts.ServerName != "" {
		base = fmt.Sprintf("%s -L %s", base, shellutil.ShellQuote(opts.ServerName))
	}
	if opts.ConfigPath != "" {
		base = fmt.Sprintf("%s -f %s", base, shellutil.ShellQuote(opts.ConfigPath))
	}
	return base
}

func tmuxArgs(opts Options, args ...string) []string {
	out := []string{}
	if opts.ServerName != "" {
		out = append(out, "-L", opts.ServerName)
	}
	if opts.ConfigPath != "" {
		out = append(out, "-f", opts.ConfigPath)
	}
	out = append(out, args...)
	return out
}

func tmuxCommand(opts Options, args ...string) (*exec.Cmd, context.CancelFunc) {
	timeout := tmuxCommandTimeout
	if opts.CommandTimeout > 0 {
		timeout = opts.CommandTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	// #nosec G204 -- tmux is the fixed executable; dynamic values are passed as argv, not shell text.
	cmd := exec.CommandContext(ctx, "tmux", tmuxArgs(opts, args...)...)
	return cmd, cancel
}

// runTmux runs a fire-and-forget tmux command, treating tmux's exit code 1
// ("not found": no session/server) as success. Use for mutations where a
// missing target should not surface as an error.
func runTmux(opts Options, args ...string) error {
	cmd, cancel := tmuxCommand(opts, args...)
	defer cancel()
	if _, err := runTmuxCmd(cmd); err != nil {
		if isExitCode1(err) {
			return nil
		}
		return err
	}
	return nil
}

// listTmux runs a tmux read command and returns its non-empty output lines.
// tmux's exit code 1 ("not found": no session/server) yields an empty result
// with no error, encoding the exit-1-means-empty convention (see errors.go)
// once instead of at every read site. Callers keep their own availability
// gating (EnsureAvailable / hasSession) and any per-line parsing.
func listTmux(opts Options, args ...string) ([]string, error) {
	cmd, cancel := tmuxCommand(opts, args...)
	defer cancel()
	output, err := runTmuxCmd(cmd)
	if err != nil {
		if isExitCode1(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseOutputLines(output), nil
}

func hasSession(sessionName string, opts Options) (bool, error) {
	cmd, cancel := tmuxCommand(opts, "has-session", "-t", sessionTarget(sessionName))
	defer cancel()
	if _, err := runTmuxCmd(cmd); err != nil {
		if isExitCode1(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// hasLivePane reports whether any pane in the session is not dead. Its only
// caller (SessionStateFor) has already established that the session exists, and
// list-panes exits 1 for a missing session anyway, so it does no has-session
// pre-check of its own.
func hasLivePane(sessionName string, opts Options) (bool, error) {
	lines, err := listTmux(opts, "list-panes", "-t", sessionTarget(sessionName), "-F", "#{pane_dead}")
	if err != nil {
		return false, err
	}
	for _, line := range lines {
		if line == "0" {
			return true, nil
		}
	}
	return false, nil
}

func KillSession(sessionName string, opts Options) error {
	if sessionName == "" {
		return nil
	}
	if err := EnsureAvailable(); err != nil {
		return err
	}
	// Kill each pane's process tree first: node/turbo/pnpm trees survive the SIGHUP from kill-session.
	pids, err := panePIDs(sessionName, opts)
	if err != nil { // retry once — a transient list-panes failure may clear
		if pids, err = panePIDs(sessionName, opts); err != nil {
			logging.Warn("KillSession %q: pane-PID lookup failed after retry; skipping process-tree reap: %v", sessionName, err)
		}
	}
	for _, pid := range pids {
		_ = process.KillProcessGroup(pid, process.KillOptions{})
	}
	return runTmux(opts, "kill-session", "-t", sessionTarget(sessionName))
}

// panePIDs returns the PID of each pane's initial process in the given session.
// The -s flag lists panes across all windows in the session, not just the active one.
func panePIDs(sessionName string, opts Options) ([]int, error) {
	exists, err := hasSession(sessionName, opts)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	lines, err := listTmux(opts, "list-panes", "-s", "-t", sessionTarget(sessionName), "-F", "#{pane_pid}")
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, field := range lines {
		if pid, err := strconv.Atoi(field); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

type SessionActivity struct {
	Name        string
	WorkspaceID string
	TabID       string
	Type        string
	Tagged      bool
}
