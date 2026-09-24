package workspacesvc

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/testutil"
)

// Shared test helpers mirroring internal/app's: test files are not importable
// across packages, so the svc tests keep their own copies.

func normalizePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// runGit delegates to the shared testutil fixture (git-env filtering + pinned
// author identity); the package-level name is kept so existing call sites are
// unchanged.
func runGit(t *testing.T, dir string, args ...string) {
	testutil.RunGit(t, dir, args...)
}
