package git

import (
	"os/exec"
	"testing"

	"github.com/andyrewlee/amux/internal/testutil"
)

// skipIfNoGit skips the test if git is not installed
func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// runGit and initRepo delegate to the shared testutil fixtures so the
// git-env filtering, pinned author identity, and single-commit repo setup live
// in one place (testutil.RunGit / testutil.InitRepo).
func runGit(t *testing.T, dir string, args ...string) string {
	return testutil.RunGit(t, dir, args...)
}

func initRepo(t *testing.T) string {
	return testutil.InitRepo(t)
}
