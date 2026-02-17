package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/process"
)

type Options struct {
	ServerName      string
	ConfigPath      string
	HideStatus      bool
	DisableMouse    bool
	DefaultTerminal string
	CommandTimeout  time.Duration
}

type SessionState struct {
	Exists      bool
	HasLivePane bool
}

type SessionTags struct {
	WorkspaceID string
	TabID       string
	Type        string
	Assistant   string
	CreatedAt   int64 // Unix seconds for fresh create/restart; may be zero for reattach.
	InstanceID  string
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

func EnsureAvailable() error {
	if _, err := exec.LookPath("tmux"); err == nil {
		return nil
	}
	return fmt.Errorf("tmux is not installed.\n\n%s", InstallHint())
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
		base = fmt.Sprintf("%s -L %s", base, shellQuote(opts.ServerName))
	}
	if opts.ConfigPath != "" {
		base = fmt.Sprintf("%s -f %s", base, shellQuote(opts.ConfigPath))
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
	cmd := exec.CommandContext(ctx, "tmux", tmuxArgs(opts, args...)...)
	return cmd, cancel
}

func hasSession(sessionName string, opts Options) (bool, error) {
	cmd, cancel := tmuxCommand(opts, "has-session", "-t", sessionTarget(sessionName))
	defer cancel()
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return false, nil
			}
		}
		return false, err
	}
	return true, nil
}

func hasLivePane(sessionName string, opts Options) (bool, error) {
	exists, err := hasSession(sessionName, opts)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	cmd, cancel := tmuxCommand(opts, "list-panes", "-t", sessionTarget(sessionName), "-F", "#{pane_dead}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		// Treat exit code 1 as "no live pane" (session may have died between checks)
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return false, nil
			}
		}
		// Return actual error for unexpected failures (callers can decide tolerance)
		return false, err
	}
	lines := strings.Fields(string(output))
	for _, line := range lines {
		if strings.TrimSpace(line) == "0" {
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
	// Kill process trees in each pane before killing the session.
	// This prevents orphaned processes (e.g. node/turbo/pnpm trees)
	// that survive SIGHUP from tmux kill-session.
	if pids, err := PanePIDs(sessionName, opts); err == nil {
		for _, pid := range pids {
			_ = process.KillProcessGroup(pid, process.KillOptions{})
		}
	}
	cmd, cancel := tmuxCommand(opts, "kill-session", "-t", sessionTarget(sessionName))
	defer cancel()
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return nil
			}
		}
		return err
	}
	return nil
}

// PanePIDs returns the PID of each pane's initial process in the given session.
// The -s flag lists panes across all windows in the session, not just the active one.
func PanePIDs(sessionName string, opts Options) ([]int, error) {
	exists, err := hasSession(sessionName, opts)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	cmd, cancel := tmuxCommand(opts, "list-panes", "-s", "-t", sessionTarget(sessionName), "-F", "#{pane_pid}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return nil, nil
			}
		}
		return nil, err
	}
	var pids []int
	for _, field := range strings.Fields(string(output)) {
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

// SessionTagValue returns a session option value for the given tag key.
func SessionTagValue(sessionName, key string, opts Options) (string, error) {
	if sessionName == "" || key == "" {
		return "", nil
	}
	exists, err := hasSession(sessionName, opts)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}
	cmd, cancel := tmuxCommand(opts, "show-options", "-t", exactSessionOptionTarget(sessionName), "-v", key)
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return "", nil
			}
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func sanitize(value string) string {
	// Normalize to lowercase to keep session naming deterministic across inputs.
	value = strings.ToLower(value)
	var b strings.Builder
	b.Grow(len(value))
	for i := 0; i < len(value); i++ {
		ch := value[i]
		switch {
		case ch >= 'a' && ch <= 'z':
			b.WriteByte(ch)
		case ch >= '0' && ch <= '9':
			b.WriteByte(ch)
		case ch == '-' || ch == '_':
			b.WriteByte(ch)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// exactTarget returns a tmux target string that forces exact session-name
// matching.  Without the "=" prefix tmux falls back to prefix matching,
// which can cause commands aimed at "amux-ws-tab-1" to hit "amux-ws-tab-10".
func exactTarget(name string) string { return "=" + name }

// sessionTarget returns a tmux target for session-level commands.
// Uses "=" prefix for exact session matching.
func sessionTarget(name string) string { return "=" + name }

// exactSessionOptionTarget returns a tmux target for session-scoped options.
// Uses "=" prefix for exact session matching in set-option/show-options.
func exactSessionOptionTarget(name string) string { return "=" + name }

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
