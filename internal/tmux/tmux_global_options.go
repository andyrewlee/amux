package tmux

import "strings"

// GlobalOptionValues returns tmux global option values for the given keys in a
// single tmux command invocation.
func GlobalOptionValues(keys []string, opts Options) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	filtered := make([]string, 0, len(keys))
	for _, raw := range keys {
		key := strings.TrimSpace(raw)
		if key == "" {
			continue
		}
		filtered = append(filtered, key)
		values[key] = ""
	}
	if len(filtered) == 0 {
		return values, nil
	}
	if err := EnsureAvailable(); err != nil {
		return values, err
	}
	separator := "\x1f"
	formatParts := make([]string, 0, len(filtered))
	for _, key := range filtered {
		formatParts = append(formatParts, "#{"+key+"}")
	}
	format := strings.Join(formatParts, separator)
	cmd, cancel := tmuxCommand(opts, "display-message", "-p", format)
	defer cancel()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return values, err
	}
	trimmed := strings.TrimRight(string(output), "\r\n")
	parts := strings.Split(trimmed, separator)
	for i, key := range filtered {
		if i >= len(parts) {
			values[key] = ""
			continue
		}
		values[key] = strings.TrimSpace(parts[i])
	}
	return values, nil
}
