package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func stringsContains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

// stripTestEnv removes environment that must never flow into a spawned
// child: git checkout overrides (a test repo is not the caller's
// checkout), every AMUX_* knob (a developer's exported AMUX_PPROF or
// AMUX_ALLOW_GIT_HOOKS silently changes child behavior — tests set their
// own via opts.Env/sessionEnv, which are appended after this strip and
// therefore survive), and TMUX/TMUX_* (a child must never believe it
// lives inside a tmux pane or socket dir it doesn't own). New AMUX_*
// vars default to stripped — an allowlist belongs in explicit test env,
// not here.
func stripTestEnv(env []string) []string {
	if len(env) == 0 {
		return env
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_DIR=") ||
			strings.HasPrefix(kv, "GIT_WORK_TREE=") ||
			strings.HasPrefix(kv, "GIT_INDEX_FILE=") ||
			strings.HasPrefix(kv, "GIT_COMMON_DIR=") ||
			strings.HasPrefix(kv, "GIT_OBJECT_DIRECTORY=") ||
			strings.HasPrefix(kv, "GIT_ALTERNATE_OBJECT_DIRECTORIES=") ||
			strings.HasPrefix(kv, "GIT_CEILING_DIRECTORIES=") ||
			strings.HasPrefix(kv, "GIT_DISCOVERY_ACROSS_FILESYSTEM=") ||
			strings.HasPrefix(kv, "AMUX_") ||
			strings.HasPrefix(kv, "TMUX=") ||
			strings.HasPrefix(kv, "TMUX_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}
