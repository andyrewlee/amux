package git

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/andyrewlee/amux/internal/fsatomic"
)

// seamOps bundles the package's mutable test seams — timeouts, runner
// indirections, filesystem hooks, and the hooks opt-out flag — behind one
// struct so a test replaces the whole surface through stubOps instead of
// hand-managing N save/restore pairs. Production resolves seams through the
// package-level ops value, read fresh at each call site (like
// ptyio.DefaultBootstrap's rebuild-per-call contract), so an override takes
// effect for the next git operation without touching init order.
//
// Tests that override any field must not use t.Parallel within this
// package: ops is shared mutable state. Keeping the seams as fields rather
// than bare vars is what makes a future per-test ops instance (and with it
// t.Parallel) possible.
type seamOps struct {
	worktreeTimeout         time.Duration
	worktreeAddTimeout      time.Duration
	worktreeRecoveryReserve time.Duration

	runGitCtx               func(context.Context, string, ...string) (string, error)
	unregisterWorktreeCtx   func(context.Context, string, string) error
	removeWorkspacePathCtx  func(context.Context, string) error
	removeWorkspacePathGOOS string
	removeRetryMetadataPath func(string) error
	writeRetryMarkerFile    func(string, []byte, os.FileMode) error
	writeRetryMetadataFile  func(string, []byte, os.FileMode) error

	// filterDriverGate stands down the mutation-site filter-driver refusal
	// in tests that fake runGitCtx — the gate execs git directly for its
	// enumeration and would otherwise escape the fake.
	filterDriverGate func(context.Context, string, string, ...string) error
	// runGitCommandAfterWaitHook fires inside runGitCommand after its wait,
	// before output handling — the seam cleanup-race tests use to land a
	// concurrent mutation in the result-handling window.
	runGitCommandAfterWaitHook          func()
	workspaceCleanupRetryFingerprintCtx func(context.Context, string) (string, error)

	// allowRepoGitHooks, when true, disables the hook/fsmonitor/GPG-signing
	// neutralization so users can opt repo-configured drivers back in
	// (AMUX_ALLOW_GIT_HOOKS=1). Read once at package init.
	allowRepoGitHooks bool
}

// ops is the package's live seam set. It initializes in init() rather than
// at the var: defaultOps names functions (RunGitCtx, refuseRepoFilterDrivers)
// whose bodies resolve fields on ops, which would make the assignment a
// package-init cycle.
var ops seamOps

func init() { ops = defaultOps() }

func defaultOps() seamOps {
	return seamOps{
		worktreeTimeout:         30 * time.Second,
		worktreeAddTimeout:      5 * time.Minute,
		worktreeRecoveryReserve: 2 * time.Minute,

		runGitCtx:               RunGitCtx,
		unregisterWorktreeCtx:   unregisterWorktreeAdminDirWithContext,
		removeWorkspacePathCtx:  removeWorkspacePathWithContext,
		removeWorkspacePathGOOS: runtime.GOOS,
		removeRetryMetadataPath: os.RemoveAll,
		writeRetryMarkerFile:    fsatomic.WriteFile,
		writeRetryMetadataFile:  fsatomic.WriteFile,

		filterDriverGate:                    refuseRepoFilterDrivers,
		workspaceCleanupRetryFingerprintCtx: workspaceCleanupRetryFingerprintWithContext,
		allowRepoGitHooks:                   os.Getenv("AMUX_ALLOW_GIT_HOOKS") == "1",
	}
}
