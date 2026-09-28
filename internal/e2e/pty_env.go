package e2e

import (
	"path/filepath"

	"github.com/andyrewlee/amux/internal/config"
)

// childEnv assembles the environment for a PTY-launched amux. Order matters:
// exec applies last-entry-wins, so the layered list is
//
//  1. the parent environment minus Git checkout overrides (stripGitEnv),
//  2. fixture defaults — HOME, TERM, profiling off,
//  3. the workspaces root pinned under the fixture HOME, and finally
//  4. explicit PTYOptions.Env entries.
//
// Layer 3 exists because a parent that itself runs inside amux exports
// AMUX_WORKSPACES_ROOT (app_init re-exports the resolved root); inheriting it
// would create fixture worktrees in the real user's workspace directory and
// make repeated runs collide. An explicit override in opts.Env still wins —
// tests that deliberately relocate the root must point it at a test-owned
// temporary directory.
func childEnv(parent []string, home string, optEnv []string) []string {
	env := append(stripGitEnv(parent),
		"HOME="+home,
		"TERM=xterm-256color",
		"AMUX_PROFILE=0",
		"AMUX_PROFILE_INTERVAL_MS=0",
		config.WorkspacesRootEnvVar+"="+filepath.Join(home, ".amux", "workspaces"),
	)
	return append(env, optEnv...)
}
