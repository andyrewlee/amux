package tmux

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

type sessionTagRow struct {
	Name string
	Tags map[string]string
}

// SessionTagValues stores tag values for a tmux session.
type SessionTagValues struct {
	Name string
	Tags map[string]string
}

const tagFieldSeparator = "|"

// SessionsWithTags returns sessions matching the provided tags, plus values for requested tag keys.
func SessionsWithTags(match map[string]string, keys []string, opts Options) ([]SessionTagValues, error) {
	if len(match) == 0 && len(keys) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(match)+len(keys))
	for key, value := range match {
		tags[key] = value
	}
	for _, key := range keys {
		if _, exists := tags[key]; !exists {
			tags[key] = ""
		}
	}
	rows, _, err := listSessionsWithTags(tags, opts)
	if err != nil {
		return nil, err
	}
	matchKeys := make([]string, 0, len(match))
	for key := range match {
		matchKeys = append(matchKeys, key)
	}
	sort.Strings(matchKeys)
	var out []SessionTagValues
	for _, row := range rows {
		if len(matchKeys) > 0 && !matchesTags(row, match, matchKeys) {
			continue
		}
		out = append(out, SessionTagValues(row))
	}
	return out, nil
}

func listSessionsWithTags(tags map[string]string, opts Options) ([]sessionTagRow, []string, error) {
	if err := EnsureAvailable(); err != nil {
		return nil, nil, err
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	format := "#{session_name}"
	for _, key := range keys {
		format = fmt.Sprintf("%s%s#{%s}", format, tagFieldSeparator, key)
	}
	cmd, cancel := tmuxCommand(opts, "list-sessions", "-F", format)
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return nil, keys, nil
			}
		}
		return nil, keys, err
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var rows []sessionTagRow
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, tagFieldSeparator)
		if len(parts) == 0 {
			continue
		}
		row := sessionTagRow{
			Name: strings.TrimSpace(parts[0]),
			Tags: make(map[string]string, len(keys)),
		}
		for i, key := range keys {
			if i+1 >= len(parts) {
				row.Tags[key] = ""
				continue
			}
			row.Tags[key] = strings.TrimSpace(parts[i+1])
		}
		rows = append(rows, row)
	}
	return rows, keys, nil
}

func matchesTags(row sessionTagRow, tags map[string]string, orderedKeys []string) bool {
	for _, key := range orderedKeys {
		want := tags[key]
		value := strings.TrimSpace(row.Tags[key])
		// Empty means "tag must be present" vs. equal to empty.
		if want == "" {
			if value == "" {
				return false
			}
		} else if value != want {
			return false
		}
	}
	return true
}
