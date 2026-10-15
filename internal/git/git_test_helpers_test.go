package git

import (
	"context"
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

// initRepoWithBranch is initRepo with a caller-chosen initial branch name, for
// tests that need a default branch whose name is not a plain identifier.
func initRepoWithBranch(t *testing.T, branch string) string {
	return testutil.InitRepoWithBranch(t, branch)
}

// stubFilterDriverGate stands down the mutation-site filter-driver refusal —
// tests that fake runGitCtx must call it, since the gate execs git directly
// for its enumeration and would otherwise escape the fake.
func stubFilterDriverGate(t *testing.T) {
	t.Helper()
	prev := filterDriverGate
	filterDriverGate = func(context.Context, string, string) error { return nil }
	t.Cleanup(func() { filterDriverGate = prev })
}
