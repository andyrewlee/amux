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

// stubOps snapshots the seam struct for a test: override fields on ops freely
// after calling it — Cleanup restores the whole surface, so no field's fake
// can leak into a following test. It composes with helpers that stub
// individual fields (each snapshots independently). Overriding tests must not
// use t.Parallel: ops is shared mutable state.
func stubOps(t *testing.T) seamOps {
	t.Helper()
	saved := ops
	t.Cleanup(func() { ops = saved })
	return saved
}

// stubFilterDriverGate stands down the mutation-site filter-driver refusal —
// tests that fake ops.runGitCtx must call it, since the gate execs git directly
// for its enumeration and would otherwise escape the fake.
func stubFilterDriverGate(t *testing.T) {
	t.Helper()
	stubOps(t)
	ops.filterDriverGate = func(context.Context, string, string, ...string) error { return nil }
}
