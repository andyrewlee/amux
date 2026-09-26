# Plan 048: Bound Git process cancellation and output draining

> **Executor instructions:** Follow each step and its verification. This is a self-contained implementation handoff; no audit-session context is assumed. Honor the STOP conditions. No commits or pushes are authorized.
>
> **Drift check (run first):**
> ~~~sh
> git diff --stat 7c530ee..HEAD -- internal/git/operations.go internal/git/operations_coverage_test.go internal/git/operations_race_test.go internal/git/operations_process_test.go internal/git/operations_process_unix_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md
> git diff HEAD -- internal/git/operations.go internal/git/operations_coverage_test.go internal/git/operations_race_test.go internal/git/operations_process_test.go internal/git/operations_process_unix_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md
> git status --short
> ~~~
> Compare BOTH commit changes and the current working tree against the Current state excerpts below. The audited baseline intentionally includes dirty user work on 2026-09-26. A known dirty file is not automatically a mismatch: preserve the audited behavior and the user's additional changes. On unexplained semantic drift, stop and report. New files will appear only in status, so inspect them before creating a same-named file.

## Status

- **Priority:** P2
- **Effort:** M
- **Risk:** MED
- **Depends on:** none
- **Category:** bug
- **Planned at:** commit 7c530ee, 2026-09-26, INCLUDING the known dirty working tree (run-session picker/transcript-browser changes).
- **Status:** TODO

## Why this matters

The Git wrapper applies deadlines, but after cancellation kills only the Git leader and waits indefinitely for Cmd.Wait. An inherited stdout/stderr pipe held by a child can keep worktree/merge operations and their locks pending after the advertised timeout. Termination must bound both the process tree and the output copier lifetime.

## Current state

- internal/git/operations.go:299–316:
~~~go
var killedByContext atomic.Bool
waitCh := make(chan error, 1)
go func() {
    waitCh <- cmd.Wait()
}()
// ...
case <-ctx.Done():
    if cmd.Process != nil {
        if err := cmd.Process.Kill(); err == nil {
            killedByContext.Store(true)
        }
    }
    err := <-waitCh
~~~
- RunGitCtx, RunGitRawCtx, and RunGitAllowFailureCtx construct exec.Command with bytes.Buffer output writers. None sets WaitDelay or an isolated group.
- internal/process/treekill_unix.go:68 has the established process-group helper:
~~~go
func SetProcessGroup(cmd *exec.Cmd) {
    if cmd.SysProcAttr == nil {
        cmd.SysProcAttr = &syscall.SysProcAttr{}
    }
    cmd.SysProcAttr.Setpgid = true
}
~~~
KillProcessGroup sends TERM then KILL with a 200ms default grace. On Windows its documented fallback kills only the leader; Darwin/Linux are release targets.
- Existing git error classification deliberately distinguishes cancellation-induced termination from a completed exit racing cancellation. Preserve gitCommandContextErrorWithKill and the Windows allow-failure classification tests.
- Existing TestRunGitAllowFailureCtxTimeout runs a one-second child under a 50ms deadline but asserts only error classification. Audit execution passed in 1.30s.
- Local authoritative API reference: go doc os/exec.Cmd.WaitDelay. Its zero default can wait for orphaned descendants to close pipes.

## Behavior decisions

Keep the existing structured Git error and raw/trimmed output contracts. Add a 500ms bounded output-drain allowance and use the existing 200ms process-group termination grace. A canceled invocation should return within its deadline plus those cleanup allowances and scheduling slack; do not promise an exact millisecond latency. On Darwin/Linux, launch Git in its own process group before Start and terminate that group even if the leader has already exited. On Windows retain compile-compatible leader termination plus bounded pipes, without claiming Unix descendant guarantees.

Use exec.CommandContext where needed to let Cmd.WaitDelay begin on cancellation; override Cancel with the group-kill behavior and record whether cancellation actually performed termination. Keep output buffers inaccessible until Wait has joined their copier. Do not implement a timeout by abandoning a goroutine that still writes into returned buffers.

## Steps

### Step 1: Characterize completion-versus-cancel semantics

Read operations_race_test.go and operations_coverage_test.go, including the runGitCommandAfterWaitHook seam. Run the existing error/cancellation tests. Prepare helper-subprocess tests using the current Go test binary rather than a user's repo hook. Helpers should report readiness and child PID through private temporary files or pipes, inherit output descriptors, and have reliable cleanup.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/git -run 'Test.*(RunGit|Context|AllowFailure)' -count=1 → existing error classifications pass before implementation; record the current timeout test duration.

### Step 2: Give commands a cancellation-aware group and bounded drain

Route all three Git constructors through one private command setup helper. Apply process.SetProcessGroup before Start, configure the chosen WaitDelay, and integrate group cancellation with the caller context. Preserve the killed-by-context signal and after-wait test hook without introducing global mutable production options. Avoid double Wait and do not kill any caller process group. If the leader exits successfully but a descendant keeps pipes open, WaitDelay must still terminate the drain; return the appropriate wrapped error instead of silently claiming complete output.

Add tests for a running leader with a pipe-holding child, an exited leader with a lingering child, already-canceled context, start failure, ordinary success, and normal exit racing cancellation. Unix tests must prove the ordinary descendant no longer runs after cancellation; an intentionally detached descendant is outside group control, but its pipe must still stop blocking the call.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/git -run 'Test.*(RunGit|Context|AllowFailure|Process|Descendant)' -count=1 -v → deadlines return within the documented cleanup budget plus a generous test margin, child cleanup holds, and every pre-existing classification case passes.

### Step 3: Verify wrappers, races, and contract documentation

Exercise all three public wrapper variants, not only a helper. Include stdout returned by the allow-failure path on its existing supported nonzero result and NUL/raw output preservation. Document cancellation cleanup limits in README.md and both configuration/orchestration docs; preserve AMUX_ALLOW_GIT_HOOKS semantics and the documented attribute-filter residual.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test -race ./internal/git ./internal/process → exit 0 with no races. Then run all Commands you will need.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Focused package verification | env -u AMUX_WORKSPACES_ROOT go test ./internal/git ./internal/process ./internal/app/workspacesvc | All selected tests pass; no new skips |
| Race verification | env -u AMUX_WORKSPACES_ROOT go test -race ./internal/git ./internal/process | Exit 0, no race reports |
| Standard repository gate | env -u AMUX_WORKSPACES_ROOT make devcheck | Exit 0; investigate and record baseline exceptions as described below |
| Changed-code strict gate | make lint-strict-new | Exit 0, zero new issues and clean formatter diff |
| Real input path | env -u AMUX_WORKSPACES_ROOT make verify-loop | Both real-agent keystroke tests pass without skips |
| Real tmux lifecycle suite | env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e | Exit 0; report any skips/baseline blockers |
| Full concurrency gate | env -u AMUX_WORKSPACES_ROOT make test-race | Exit 0, no race reports |
| Real tmux concurrency gate | env -u AMUX_WORKSPACES_ROOT make test-race-tmux | Exit 0, no race reports; report skips |
| Portability | make windows-build | Exit 0 |
| Diff integrity | git diff --check | Exit 0 |

No dependency installation or module upgrade is needed. Format changed Go files with the repository's gofumpt-compatible tooling; make fmt is the repository formatting command, but do not accept unrelated formatting changes in this dirty checkout.

**Verification baseline:** Audit verification is not wholly green: the broad devcheck run failed in real e2e tests. An ambient AMUX_WORKSPACES_ROOT escaped the test HOME and caused a collision with the user's real workspace root; plan 059 owns that isolation defect. Until 059 lands, prefix every command below that can reach e2e with env -u AMUX_WORKSPACES_ROOT. The filtered-picker regression is separately owned by plan 036 and may affect e2e results. The real input verify-loop passed during the audit. Record exact remaining failures; do not fix unrelated failures, weaken checks, kill user sessions, or claim skipped real-tmux tests provide end-to-end validation.

## Scope

**In scope — only these paths may be changed for this implementation:**

- internal/git/operations.go
- internal/git/operations_coverage_test.go
- internal/git/operations_race_test.go
- internal/git/operations_process_test.go
- internal/git/operations_process_unix_test.go
- README.md
- docs/CONFIG.md
- docs/ORCHESTRATION.md
- plans/README.md — only this plan's status row, unless the reviewer maintains it.

**Out of scope:** all other source files; unrelated refactoring; dependency/toolchain upgrades; changes to the trust model or automatic Git push/checkout behavior; other plans' implementation. Do not expand wildcard test scope into production edits.

## Git workflow

Work on advisor/048-bound-git-cancellation only if the operator has selected that branch; otherwise use their current checkout. Do not commit, push, open a PR, stash, clean, reset, or discard user edits without explicit instruction. Before editing, record git status --short and the existing diff. Existing dirty changes are input to this plan, not cleanup targets. Only this plan's implementation delta must fit Scope; pre-existing unrelated edits may remain. Update only this plan's row in plans/README.md at completion unless the reviewer maintains that index.

## Test plan

Use the regression cases and existing test exemplars in the steps. Tests must assert the observable outcome, not merely that a new helper was called. Use temporary repositories/metadata and isolated tmux servers, never the user's actual state. Channel/barrier-based scheduling is preferred over arbitrary pacing sleeps.

## Done criteria

- [ ] Every regression case in Steps passes with env -u AMUX_WORKSPACES_ROOT go test ./internal/git ./internal/process ./internal/app/workspacesvc.
- [ ] env -u AMUX_WORKSPACES_ROOT go test -race ./internal/git ./internal/process exits 0 with no races.
- [ ] env -u AMUX_WORKSPACES_ROOT make devcheck and make lint-strict-new were run; both pass, or the exact independently established baseline blocker is recorded and this plan remains BLOCKED rather than DONE.
- [ ] env -u AMUX_WORKSPACES_ROOT make verify-loop: Both real-agent keystroke tests pass without skips.
- [ ] env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e: Exit 0; report any skips/baseline blockers.
- [ ] env -u AMUX_WORKSPACES_ROOT make test-race: Exit 0, no race reports.
- [ ] env -u AMUX_WORKSPACES_ROOT make test-race-tmux: Exit 0, no race reports; report skips.
- [ ] make windows-build: Exit 0.
- [ ] git diff --check exits 0.
- [ ] git diff --name-only and git status --short, compared to the captured initial state, show no implementation edits outside Scope.
- [ ] User work is preserved; no commit/push occurred; this plan's status row is updated only when all required work is complete.

## STOP conditions

- The relevant live semantics differ from the excerpts and steps for reasons not explained by the declared dependencies or audited dirty baseline.
- A verification fails twice after a focused, reasonable fix attempt.
- The change requires production files outside Scope, alters a public behavior this plan explicitly preserves, or cannot be made without discarding user edits.
- If importing internal/process into git creates a cycle in the live dependency graph, stop; do not duplicate process-group code or weaken the boundary silently.
- Do not change shared process helpers' global termination semantics as a side effect of the Git fix; that requires a revised scope.
- If preserving completed-success-versus-cancel classification requires changing an existing regression's expected result, stop and explain the behavioral tradeoff.
- Do not count zombie PID existence alone as a live-child failure; tests must distinguish a running process from an exited child awaiting reaping.
- A broad baseline check fails outside scope: report the exact test/error and retain BLOCKED status; do not silently waive the gate or repair unrelated code.

## Maintenance notes

- WaitDelay bounds pipes, process groups bound ordinary descendants; neither alone provides both guarantees.
- All future Git constructors must use the common setup path. Review successful-leader/lingering-child behavior and output completeness.
- External descendants can deliberately escape process groups. Keep that limitation explicit and do not claim arbitrary process-tree containment.

