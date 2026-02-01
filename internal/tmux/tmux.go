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

func ClientCommand(sessionName, workDir, command string) string {
	return ClientCommandWithOptions(sessionName, workDir, command, DefaultOptions())
}

func ClientCommandWithOptions(sessionName, workDir, command string, opts Options) string {
	return clientCommand(sessionName, workDir, command, opts, SessionTags{})
}

func ClientCommandWithTags(sessionName, workDir, command string, opts Options, tags SessionTags) string {
	return clientCommand(sessionName, workDir, command, opts, tags)
}

func clientCommand(sessionName, workDir, command string, opts Options, tags SessionTags) string {
	base := tmuxBase(opts)
	session := shellQuote(sessionName)
	dir := shellQuote(workDir)
	cmd := shellQuote(command)

	// Use atomic new-session -Ad: creates if missing, attaches if exists (detaching other clients)
	create := fmt.Sprintf("%s new-session -Ads %s -c %s sh -lc %s",
		base, session, dir, cmd)

	var settings strings.Builder
	// Disable tmux prefix for this session only (not global) to make it transparent
	settings.WriteString(fmt.Sprintf("%s set-option -t %s prefix None 2>/dev/null; ", base, session))
	settings.WriteString(fmt.Sprintf("%s set-option -t %s prefix2 None 2>/dev/null; ", base, session))
	if opts.HideStatus {
		settings.WriteString(fmt.Sprintf("%s set-option -t %s status off 2>/dev/null; ", base, session))
	}
	if opts.DisableMouse {
		settings.WriteString(fmt.Sprintf("%s set-option -t %s mouse off 2>/dev/null; ", base, session))
	}
	if opts.DefaultTerminal != "" {
		settings.WriteString(fmt.Sprintf("%s set-option -t %s default-terminal %s 2>/dev/null; ", base, session, shellQuote(opts.DefaultTerminal)))
	}
	// Ensure activity timestamps update for window_activity-based tracking.
	settings.WriteString(fmt.Sprintf("%s set-option -t %s -w monitor-activity on 2>/dev/null; ", base, session))
	appendSessionTags(&settings, base, session, tags)

	// Use attach -d to detach other clients (handles multi-instance gracefully)
	attach := fmt.Sprintf("%s attach -dt %s", base, session)

	return fmt.Sprintf("%s && %s%s", create, settings.String(), attach)
}

func appendSessionTags(settings *strings.Builder, base, session string, tags SessionTags) {
	if tags.WorkspaceID == "" && tags.TabID == "" && tags.Type == "" && tags.Assistant == "" && tags.CreatedAt == 0 {
		return
	}
	settings.WriteString(fmt.Sprintf("%s set-option -t %s @amux 1 2>/dev/null; ", base, session))
	if tags.WorkspaceID != "" {
		settings.WriteString(fmt.Sprintf("%s set-option -t %s @amux_workspace %s 2>/dev/null; ", base, session, shellQuote(tags.WorkspaceID)))
	}
	if tags.TabID != "" {
		settings.WriteString(fmt.Sprintf("%s set-option -t %s @amux_tab %s 2>/dev/null; ", base, session, shellQuote(tags.TabID)))
	}
	if tags.Type != "" {
		settings.WriteString(fmt.Sprintf("%s set-option -t %s @amux_type %s 2>/dev/null; ", base, session, shellQuote(tags.Type)))
	}
	if tags.Assistant != "" {
		settings.WriteString(fmt.Sprintf("%s set-option -t %s @amux_assistant %s 2>/dev/null; ", base, session, shellQuote(tags.Assistant)))
	}
	if tags.CreatedAt != 0 {
		settings.WriteString(fmt.Sprintf("%s set-option -t %s @amux_created_at %s 2>/dev/null; ", base, session, shellQuote(fmt.Sprintf("%d", tags.CreatedAt))))
	}
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
	cmd, cancel := tmuxCommand(opts, "has-session", "-t", sessionName)
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
	cmd, cancel := tmuxCommand(opts, "list-panes", "-t", sessionName, "-F", "#{pane_dead}")
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
	cmd, cancel := tmuxCommand(opts, "kill-session", "-t", sessionName)
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
func PanePIDs(sessionName string, opts Options) ([]int, error) {
	cmd, cancel := tmuxCommand(opts, "list-panes", "-t", sessionName, "-F", "#{pane_pid}")
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
	if err := EnsureAvailable(); err != nil {
		return "", err
	}
	cmd, cancel := tmuxCommand(opts, "show-options", "-t", sessionName, "-v", key)
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

// ListSessionsMatchingTags returns sessions matching all provided tags.
func ListSessionsMatchingTags(tags map[string]string, opts Options) ([]string, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	rows, orderedKeys, err := listSessionsWithTags(tags, opts)
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, row := range rows {
		if matchesTags(row, tags, orderedKeys) {
			matches = append(matches, row.Name)
		}
	}
	return matches, nil
}

// KillSessionsMatchingTags kills sessions that match all provided tags.
func KillSessionsMatchingTags(tags map[string]string, opts Options) (bool, error) {
	sessions, err := ListSessionsMatchingTags(tags, opts)
	if err != nil {
		return false, err
	}
	if len(sessions) == 0 {
		return false, nil
	}
	var firstErr error
	for _, name := range sessions {
		if err := KillSession(name, opts); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return true, firstErr
}

// ListSessions returns all tmux session names for the configured server.
func ListSessions(opts Options) ([]string, error) {
	if err := EnsureAvailable(); err != nil {
		return nil, err
	}
	cmd, cancel := tmuxCommand(opts, "list-sessions", "-F", "#{session_name}")
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
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var sessions []string
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		sessions = append(sessions, name)
	}
	return sessions, nil
}

// KillSessionsWithPrefix kills all sessions with a matching name prefix.
func KillSessionsWithPrefix(prefix string, opts Options) error {
	if prefix == "" {
		return nil
	}
	sessions, err := ListSessions(opts)
	if err != nil {
		return err
	}
	var firstErr error
	for _, name := range sessions {
		if strings.HasPrefix(name, prefix) {
			if err := KillSession(name, opts); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// KillWorkspaceSessions kills all sessions for a workspace ID.
func KillWorkspaceSessions(wsID string, opts Options) error {
	if wsID == "" {
		return nil
	}
	prefix := SessionName("amux", wsID) + "-"
	return KillSessionsWithPrefix(prefix, opts)
}

// CapturePane captures the scrollback history of a tmux pane (excluding the
// visible screen area) with ANSI escape codes preserved. Returns nil if the
// session has no scrollback or does not exist.
func CapturePane(sessionName string, opts Options) ([]byte, error) {
	if sessionName == "" {
		return nil, nil
	}
	// -p: output to stdout
	// -e: include escape sequences (ANSI styling)
	// -S -: start from beginning of history
	// -E -1: end at last scrollback line (excludes visible screen)
	// -t: target session
	cmd, cancel := tmuxCommand(opts, "capture-pane", "-p", "-e", "-S", "-", "-E", "-1", "-t", sessionName)
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if len(output) == 0 {
		return nil, nil
	}
	return output, nil
}

// AmuxSessionsByWorkspace returns all @amux=1 sessions grouped by their
// @amux_workspace value. Sessions without a workspace tag are omitted.
func AmuxSessionsByWorkspace(opts Options) (map[string][]string, error) {
	rows, err := SessionsWithTags(
		map[string]string{"@amux": "1"},
		[]string{"@amux_workspace"},
		opts,
	)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string)
	for _, row := range rows {
		wsID := row.Tags["@amux_workspace"]
		if wsID == "" {
			continue
		}
		out[wsID] = append(out[wsID], row.Name)
	}
	return out, nil
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

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
