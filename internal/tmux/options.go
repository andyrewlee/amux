package tmux

import (
	"fmt"
	"strings"
)

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
	output, err := runTmuxCmd(cmd)
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
	output, err := runTmuxCmdCombined(cmd)
	if err != nil {
		if isExitCode1(err) {
			if isOptionMissingStderr(string(output)) {
				return "", nil
			}
			stderr := strings.TrimSpace(string(output))
			return "", fmt.Errorf("show-options -g %s: %s: %w", key, stderr, err)
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
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
	output, err := runTmuxCmdCombined(cmd)
	if err != nil {
		if isExitCode1(err) {
			stderr := strings.TrimSpace(string(output))
			if isOptionMissingStderr(stderr) {
				return nil
			}
			return fmt.Errorf("set-option -g %s: %s: %w", key, stderr, err)
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
	output, err := runTmuxCmdCombined(cmd)
	if err != nil {
		if isExitCode1(err) {
			stderr := strings.TrimSpace(string(output))
			if isOptionMissingStderr(stderr) {
				return nil
			}
			return fmt.Errorf("set-option -g (multi): %s: %w", stderr, err)
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
