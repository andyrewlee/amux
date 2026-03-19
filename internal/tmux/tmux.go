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
	WorkspaceID  string
	TabID        string
	Type         string
	Assistant    string
	CreatedAt    int64 // Unix seconds for fresh create/restart; may be zero for reattach.
	InstanceID   string
	SessionOwner string
	LeaseAtMS    int64
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

// AllSessionStates returns the SessionState for every tmux session on the
// server in a single subprocess call.  It runs:
//
//	tmux list-panes -a -F "#{session_name}\t#{pane_dead}"
//
// Sessions that appear in output have Exists=true.  Any session with at
// least one pane where pane_dead is "0" gets HasLivePane=true.
// If there are no sessions at all (exit code 1), an empty map is returned.
func AllSessionStates(opts Options) (map[string]SessionState, error) {
	if err := EnsureAvailable(); err != nil {
		return nil, err
	}
	cmd, cancel := tmuxCommand(opts, "list-panes", "-a", "-F", "#{session_name}\t#{pane_dead}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		if isExitCode1(err) {
			return map[string]SessionState{}, nil
		}
		return nil, err
	}
	states := make(map[string]SessionState)
	for _, line := range parseOutputLines(output) {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		name := parts[0]
		dead := parts[1]
		st := states[name]
		st.Exists = true
		if dead == "0" {
			st.HasLivePane = true
		}
		states[name] = st
	}
	return states, nil
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
		if isExitCode1(err) {
			return false, nil
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
		if isExitCode1(err) {
			return false, nil
		}
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
	if pids, err := panePIDs(sessionName, opts); err == nil {
		for _, pid := range pids {
			_ = process.KillProcessGroup(pid, process.KillOptions{})
		}
	}
	cmd, cancel := tmuxCommand(opts, "kill-session", "-t", sessionTarget(sessionName))
	defer cancel()
	if err := cmd.Run(); err != nil {
		if isExitCode1(err) {
			return nil
		}
		return err
	}
	return nil
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
	cmd, cancel := tmuxCommand(opts, "list-panes", "-s", "-t", sessionTarget(sessionName), "-F", "#{pane_pid}")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		if isExitCode1(err) {
			return nil, nil
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
		if isExitCode1(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// GlobalOptionValue returns a tmux global option value for the given key.
// Missing options return an empty value with nil error, while connection
// failures (for example, no running server) are returned as errors.
// Unlike SetGlobalOptionValue, read paths do not suppress generic command
// errors because callers rely on these failures for ownership/coordination
// fallback decisions.
func GlobalOptionValue(key string, opts Options) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", nil
	}
	if err := EnsureAvailable(); err != nil {
		return "", err
	}
	cmd, cancel := tmuxCommand(opts, "show-options", "-g", "-v", key)
	defer cancel()
	output, err := cmd.CombinedOutput()
	if err != nil {
		if isExitCode1(err) {
			if tmuxShowOptionMissingError(string(output)) {
				return "", nil
			}
			stderr := strings.TrimSpace(string(output))
			return "", fmt.Errorf("show-options -g %s: %s", key, stderr)
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func tmuxShowOptionMissingError(stderr string) bool {
	message := strings.ToLower(strings.TrimSpace(stderr))
	return strings.Contains(message, "invalid option") || strings.Contains(message, "unknown option")
}

// OptionValue represents a tmux option key/value pair.
type OptionValue struct {
	Key   string
	Value string
}

// SetGlobalOptionValue sets a tmux global option value.
func SetGlobalOptionValue(key, value string, opts Options) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	if err := EnsureAvailable(); err != nil {
		return err
	}
	cmd, cancel := tmuxCommand(opts, "set-option", "-g", key, value)
	defer cancel()
	output, err := cmd.CombinedOutput()
	if err != nil {
		if isExitCode1(err) {
			stderr := strings.TrimSpace(string(output))
			if tmuxShowOptionMissingError(stderr) {
				return nil
			}
			return fmt.Errorf("set-option -g %s: %s", key, stderr)
		}
		return err
	}
	return nil
}

// buildMultiSetOptionArgs builds semicolon-separated tmux set-option arguments.
// scope provides the targeting flags (e.g. []string{"-g"} or []string{"-t", target}).
func buildMultiSetOptionArgs(scope []string, values []OptionValue) ([]string, int) {
	args := make([]string, 0, len(values)*6)
	added := 0
	for _, candidate := range values {
		key := strings.TrimSpace(candidate.Key)
		if key == "" {
			continue
		}
		if added > 0 {
			args = append(args, ";")
		}
		args = append(args, "set-option")
		args = append(args, scope...)
		args = append(args, key, candidate.Value)
		added++
	}
	return args, added
}

// SetGlobalOptionValues sets multiple tmux global options in a single tmux command.
func SetGlobalOptionValues(values []OptionValue, opts Options) error {
	if len(values) == 0 {
		return nil
	}
	if err := EnsureAvailable(); err != nil {
		return err
	}
	args, added := buildMultiSetOptionArgs([]string{"-g"}, values)
	if added == 0 {
		return nil
	}
	cmd, cancel := tmuxCommand(opts, args...)
	defer cancel()
	output, err := cmd.CombinedOutput()
	if err != nil {
		if isExitCode1(err) {
			stderr := strings.TrimSpace(string(output))
			if tmuxShowOptionMissingError(stderr) {
				return nil
			}
			return fmt.Errorf("set-option -g (multi): %s", stderr)
		}
		return err
	}
	return nil
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

// sessionTarget returns a tmux target for session-level commands.
// Uses "=" prefix for exact session matching, preventing tmux from
// prefix-matching "amux-ws-tab-1" to "amux-ws-tab-10".
func sessionTarget(name string) string { return "=" + name }

// exactSessionOptionTarget returns a tmux target for session-scoped options.
// Unlike has-session and send-keys, tmux set-option and show-options do not
// support the "=" exact-match prefix (tmux 3.6a returns "no such session").
// Bare names are safe here because amux session names include workspace ID +
// tab ID, making prefix collisions practically impossible.
func exactSessionOptionTarget(name string) string { return name }

// parseOutputLines splits tmux command output into non-empty trimmed lines.
func parseOutputLines(output []byte) []string {
	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
