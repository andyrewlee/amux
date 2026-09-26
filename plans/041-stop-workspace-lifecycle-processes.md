# Plan 041: Cancel and drain lifecycle scripts before workspace teardown

> **Executor instructions:** Follow each step and its verification. This is a self-contained implementation handoff; no audit-session context is assumed. Honor the STOP conditions. No commits or pushes are authorized.
>
> **Drift check (run first):**
> ~~~sh
> git diff --stat 7c530ee..HEAD -- internal/process/scripts.go internal/process/scripts_stop.go internal/process/script_lifecycle.go internal/process/lifecycle_coordinator.go internal/process/lifecycle_coordinator_test.go internal/process/scripts_teardown_test.go internal/process/scripts_release_test.go internal/process/run_session_test.go internal/process/on_done_test.go internal/app/app_workspace_scripts.go internal/app/app_input_workspace.go internal/app/app_operations.go internal/app/app_input_dialogs.go internal/app/app_input_workspace_shelve.go internal/app/workspacesvc/workspace_service_app_ops.go internal/app/workspacesvc/workspace_service_scripts.go internal/app/workspacesvc/workspace_service.go internal/app/workspacesvc/workspace_service_shelve.go internal/app/workspacesvc/workspace_service_remove_project.go internal/app/workspacesvc/workspace_service_prune.go internal/app/workspacesvc/workspace_service_scripts_test.go internal/app/workspacesvc/workspace_service_shelve_test.go internal/app/workspacesvc/workspace_service_archive_script_test.go internal/app/workspacesvc/workspace_service_lifecycle_teardown_test.go internal/app/app_workspace_scripts_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md
> git diff HEAD -- internal/process/scripts.go internal/process/scripts_stop.go internal/process/script_lifecycle.go internal/process/lifecycle_coordinator.go internal/process/lifecycle_coordinator_test.go internal/process/scripts_teardown_test.go internal/process/scripts_release_test.go internal/process/run_session_test.go internal/process/on_done_test.go internal/app/app_workspace_scripts.go internal/app/app_input_workspace.go internal/app/app_operations.go internal/app/app_input_dialogs.go internal/app/app_input_workspace_shelve.go internal/app/workspacesvc/workspace_service_app_ops.go internal/app/workspacesvc/workspace_service_scripts.go internal/app/workspacesvc/workspace_service.go internal/app/workspacesvc/workspace_service_shelve.go internal/app/workspacesvc/workspace_service_remove_project.go internal/app/workspacesvc/workspace_service_prune.go internal/app/workspacesvc/workspace_service_scripts_test.go internal/app/workspacesvc/workspace_service_shelve_test.go internal/app/workspacesvc/workspace_service_archive_script_test.go internal/app/workspacesvc/workspace_service_lifecycle_teardown_test.go internal/app/app_workspace_scripts_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md
> git status --short
> ~~~
> Compare BOTH commit changes and the current working tree against the Current state excerpts below. The audited baseline intentionally includes dirty user work on 2026-09-26. A known dirty file is not automatically a mismatch: preserve the audited behavior and the user's additional changes. On unexplained semantic drift, stop and report. New files will appear only in status, so inspect them before creating a same-named file.

## Status

- **Priority:** P1
- **Effort:** L
- **Risk:** MED
- **Depends on:** none
- **Category:** bug
- **Planned at:** commit 7c530ee, 2026-09-26, INCLUDING the known dirty working tree (run-session picker/transcript-browser changes).
- **Status:** TODO

## Why this matters

In the production hosted-run mode, Stop kills tmux run sessions and returns without stopping setup's local process. Deletion/shelving then removes the directory while setup is still writing. Repeated setup requests can overwrite the single tracking slot, losing the ability to stop an older process even on quit. Teardown needs an explicit lifecycle coordinator, not only an IsRunning check.

## Current state

- Production installs the host at internal/app/app_init.go:199.
- internal/process/scripts_stop.go:16–30:
~~~go
if r.RunHosted() {
    names, err := r.findRunSessions(ws)
    if err != nil {
        return err
    }
    for _, name := range names {
        if err := r.runHost.Kill(name); err != nil {
            return err
        }
    }
    return nil
}
~~~
- Setup spawns one local shell per command and tracks it at internal/process/scripts.go:249–256:
~~~go
running := &runningScript{cmd: cmd, done: make(chan struct{})}
key := scriptWorkspaceKey(ws)
r.setRunningEntry(key, running)
err := cmd.Wait()
~~~
setRunningEntry at :158–164 replaces an existing slot. handleRerunWorkspaceScript at app_workspace_scripts.go:34–48 schedules every request.
- Service teardown calls stopWorkspaceScriptsForDelete before archive and RemoveWorkspace (workspace_service_shelve.go:113–126); delete has the same order.
- RunOnDone intentionally avoids the long-lived run slot; it is still a local subprocess that can outlive the worktree. Keep that distinction rather than placing every hook into the same slot.
- Use the existing process-group helpers and archive timeout/reaping pattern; tests use isolated shell helpers and observable readiness. Preserve archive best-effort semantics and the UI lifecycle mutation guard. Long work must remain in tea.Cmd.

## Behavior decisions

1. Separate hosted run control from local lifecycle work. A run-toggle stops only the run backend. Destructive workspace teardown stops hosted runs AND every local lifecycle process for that workspace.
2. Admit only one setup sequence per workspace. A second manual rerun returns a typed busy outcome and informational toast; it never launches a second sequence or replaces tracking. Setup may coexist with an independently requested hosted run, but teardown cancels both.
3. Use a per-workspace runner generation and cancellation context spanning the entire setup sequence, including gaps between commands. Reserve a setup request when its command is created; execution checks its ticket/generation. Beginning teardown invalidates queued tickets, cancels active work, and prevents new starts until the service operation finishes.
4. Archive is the one authorized lifecycle operation during teardown: the teardown owner runs it after prior work has drained and before removal. Keep its timeout and warning-only outcome; do not accidentally reject it through the new gate.
5. On-done hooks have their own tracked set, not the single setup slot. Teardown cancels/drains them. Ordinary app shutdown cancels local lifecycle work but leaves hosted run/agent sessions persistent, as today.
6. A failed teardown releases its admission gate so the surviving workspace is usable. A successful teardown leaves older tickets permanently stale; a newly created/restored lifecycle obtains a fresh generation. Transcript recording and completion messages may still report the canceled run, but may not spawn another setup step or recreate workspace metadata.
7. Stop/drain failure aborts destructive worktree removal. Cancellation is bounded by the existing process-group escalation and explicit drain allowance; never release a live local process's port allocation.

## Steps

### Step 1: Add combined-mode regression fixtures

Read scripts_teardown_test.go, scripts_release_test.go, run_session_test.go, and the service archive tests. Add an observable helper setup that reports its PID/readiness then blocks. Install a fake RunSessionHost on the same runner and call teardown. Assert the local PID exits, hosted sessions are killed, and the second command in a multi-step setup never starts. Add rapid duplicate rerun and queued-before-teardown tests.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/process -run 'Test.*(Hosted|Setup|Stop|Lifecycle)' -count=1 → existing tests pass; new combined-mode cases expose the missing local teardown/overwritten tracking until Step 2.

### Step 2: Add local lifecycle admission, generation, and draining

Implement the coordinator in lifecycle_coordinator.go. Keep state under r.mu or one documented coordinator mutex; do not hold it across process Start/Wait, kill, transcript I/O, or callbacks. Each ticket has one cancellation owner and exactly-once completion. Snapshot Workspace input using its existing clone convention before asynchronous execution.

Route automatic setup, restore-time setup, manual rerun, and trust-retry setup through reservation/admission. The audited entry points are app_input_workspace.go:116 and app_input_workspace_shelve.go:186 via app_operations.go:222, app_workspace_scripts.go:38, and app_input_dialogs.go:240 via app_operations.go:230; all converge on workspace_service_scripts.go. Preserve these wrappers if no changes are needed. RunOnDoneScript in workspace_service_app_ops.go must retain the same coordinator protection provided by the process API. Direct RunSetup callers must receive the same protection. Check cancellation before each setup command and before Start; register a started process atomically with the ticket so teardown cannot miss the start/register gap. Track each on-done process separately. Preserve script trust hash checks and environment precedence.

Separate StopRun from teardown-capable StopWorkspace (names may follow repo conventions). Existing nonconcurrent RunScript and the run toggle call StopRun. Teardown receives an owned guard/ticket that allows exactly its archive invocation. StopAll must cancel queued/active local lifecycle work and must not kill hosted runs.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test -race ./internal/process -count=1 → all combined-mode, duplicate admission, start/stop-gap, multi-command, on-done, transcript, and trust tests pass without races.

### Step 3: Hold the runner teardown gate across service removal

In validated delete/shelve/project-removal commands, acquire the runner teardown guard before stopping scripts. Hold it through archive, worktree/session removal, and metadata outcome; release with success/failure semantics in a defer. Do not acquire it before validation that would otherwise leave a live workspace untouched. Preserve primary-checkout protection, durable delete tombstones, shelve intent rollback, and branch/metadata ordering.

Map busy manual setup to an informational toast, not an operation-failed modal. A canceled setup after explicit teardown must not reopen trust prompts or claim an unrelated new workspace finished. Preserve the dirty working-tree run-picker/transcript-browser code in app_workspace_scripts.go and workspace_service_scripts.go.

Use fake Git operations that refuse RemoveWorkspace while the test setup helper is live. Add deletion/shelving failures that release the gate and allow a later setup, archive execution under the guard, and a stale setup ticket delivered after delete/recreate.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/app/workspacesvc ./internal/app -run 'Test.*(Setup|Script|Lifecycle|Delete|Shelve|RemoveProject)' -count=1 → teardown ordering and recoverability assertions pass.

### Step 4: Check release/shutdown and document lifecycle semantics

Fix IsRunning/ReleaseWorkspace decisions so the hosted backend does not hide active local lifecycle work. Keep a separate query for the sidebar's hosted run badge. Add ordinary shutdown assertions: local setup/on-done drained, persistent hosted runs untouched, and no port release while an uncanceled lifecycle process remains.

Update README.md, docs/CONFIG.md, and docs/ORCHESTRATION.md with duplicate-setup rejection, teardown cancellation, and archive exception. No new key or config field is required.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test -race ./internal/process ./internal/app/workspacesvc ./internal/app → exit 0 and no races; run all final gates below.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Focused package verification | env -u AMUX_WORKSPACES_ROOT go test ./internal/process ./internal/app/workspacesvc ./internal/app | All selected tests pass; no new skips |
| Race verification | env -u AMUX_WORKSPACES_ROOT go test -race ./internal/process ./internal/app/workspacesvc ./internal/app | Exit 0, no race reports |
| Standard repository gate | env -u AMUX_WORKSPACES_ROOT make devcheck | Exit 0; investigate and record baseline exceptions as described below |
| Changed-code strict gate | make lint-strict-new | Exit 0, zero new issues and clean formatter diff |
| Real input path | env -u AMUX_WORKSPACES_ROOT make verify-loop | Both real-agent keystroke tests pass without skips |
| Real tmux lifecycle suite | env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e | Exit 0; report skips/baseline blockers |
| Full concurrency gate | env -u AMUX_WORKSPACES_ROOT make test-race | Exit 0, no race reports |
| Real tmux concurrency gate | env -u AMUX_WORKSPACES_ROOT make test-race-tmux | Exit 0, no race reports; report skips |
| Diff integrity | git diff --check | Exit 0 |

No dependency installation or module upgrade is needed. Format changed Go files with the repository's gofumpt-compatible tooling; make fmt is the repository formatting command, but do not accept unrelated formatting changes in this dirty checkout.

**Verification baseline:** Audit verification is not wholly green: the broad devcheck run failed in real e2e tests. An ambient AMUX_WORKSPACES_ROOT escaped the test HOME and caused a collision with the user's real workspace root; plan 059 owns that isolation defect. Until 059 lands, prefix every command below that can reach e2e with env -u AMUX_WORKSPACES_ROOT. The filtered-picker regression is separately owned by plan 036 and may affect e2e results. The real input verify-loop passed during the audit. Record exact remaining failures; do not fix unrelated failures, weaken checks, kill user sessions, or claim skipped real-tmux tests provide end-to-end validation.

## Scope

**In scope — only these paths may be changed for this implementation:**

- internal/process/scripts.go
- internal/process/scripts_stop.go
- internal/process/script_lifecycle.go
- internal/process/lifecycle_coordinator.go
- internal/process/lifecycle_coordinator_test.go
- internal/process/scripts_teardown_test.go
- internal/process/scripts_release_test.go
- internal/process/run_session_test.go
- internal/process/on_done_test.go
- internal/app/app_workspace_scripts.go
- internal/app/app_input_workspace.go
- internal/app/app_operations.go
- internal/app/app_input_dialogs.go
- internal/app/app_input_workspace_shelve.go
- internal/app/workspacesvc/workspace_service_app_ops.go
- internal/app/workspacesvc/workspace_service_scripts.go
- internal/app/workspacesvc/workspace_service.go
- internal/app/workspacesvc/workspace_service_shelve.go
- internal/app/workspacesvc/workspace_service_remove_project.go
- internal/app/workspacesvc/workspace_service_prune.go
- internal/app/workspacesvc/workspace_service_scripts_test.go
- internal/app/workspacesvc/workspace_service_shelve_test.go
- internal/app/workspacesvc/workspace_service_archive_script_test.go
- internal/app/workspacesvc/workspace_service_lifecycle_teardown_test.go
- internal/app/app_workspace_scripts_test.go
- README.md
- docs/CONFIG.md
- docs/ORCHESTRATION.md
- plans/README.md — only this plan's status row, unless the reviewer maintains it.

**Out of scope:** all other source files; unrelated refactoring; dependency/toolchain upgrades; changes to the trust model or automatic Git push/checkout behavior; other plans' implementation. Do not expand wildcard test scope into production edits.

## Git workflow

Work on advisor/041-stop-workspace-lifecycle-processes only if the operator has selected that branch; otherwise use their current checkout. Do not commit, push, open a PR, stash, clean, reset, or discard user edits without explicit instruction. Before editing, record git status --short and the existing diff. Existing dirty changes are input to this plan, not cleanup targets. Only this plan's implementation delta must fit Scope; pre-existing unrelated edits may remain. Update only this plan's row in plans/README.md at completion unless the reviewer maintains that index.

## Test plan

Use the regression cases and existing test exemplars in the steps. Tests must assert the observable outcome, not merely that a new helper was called. Use temporary repositories/metadata and isolated tmux servers, never the user's actual state. Channel/barrier-based scheduling is preferred over arbitrary pacing sleeps.

## Done criteria

- [ ] Every regression case in Steps passes with env -u AMUX_WORKSPACES_ROOT go test ./internal/process ./internal/app/workspacesvc ./internal/app.
- [ ] env -u AMUX_WORKSPACES_ROOT go test -race ./internal/process ./internal/app/workspacesvc ./internal/app exits 0 with no races.
- [ ] env -u AMUX_WORKSPACES_ROOT make devcheck and make lint-strict-new were run; both pass, or the exact independently established baseline blocker is recorded and this plan remains BLOCKED rather than DONE.
- [ ] env -u AMUX_WORKSPACES_ROOT make verify-loop: Both real-agent keystroke tests pass without skips.
- [ ] env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e: Exit 0; report skips/baseline blockers.
- [ ] env -u AMUX_WORKSPACES_ROOT make test-race: Exit 0, no race reports.
- [ ] env -u AMUX_WORKSPACES_ROOT make test-race-tmux: Exit 0, no race reports; report skips.
- [ ] git diff --check exits 0.
- [ ] git diff --name-only and git status --short, compared to the captured initial state, show no implementation edits outside Scope.
- [ ] User work is preserved; no commit/push occurred; this plan's status row is updated only when all required work is complete.

## STOP conditions

- The relevant live semantics differ from the excerpts and steps for reasons not explained by the declared dependencies or audited dirty baseline.
- A verification fails twice after a focused, reasonable fix attempt.
- The change requires production files outside Scope, alters a public behavior this plan explicitly preserves, or cannot be made without discarding user edits.
- Do not add a new App lifecycle phase that bypasses or replaces the existing create/mutate guard. The runner gate covers asynchronous script admission, not UI ownership.
- If archive cannot be admitted under an owned teardown token without allowing other scripts through, stop; never remove the tree before archive by accident.
- Do not make normal app quit destroy tmux-hosted run/agent sessions.
- If stopping a child fails, keep the teardown failed and the worktree present; do not convert it to a warning and proceed.
- If plan 039 or 038 has landed, preserve its field transactions/no-resurrection behavior when adapting service and transcript completion code.
- A broad baseline check fails outside scope: report the exact test/error and retain BLOCKED status; do not silently waive the gate or repair unrelated code.

## Maintenance notes

- Every new local lifecycle hook must register with the coordinator before teardown can safely rely on it.
- Process cancellation must span the sequence, not just its currently executing shell. Generation invalidation also protects queued commands.
- Keep busy, canceled, trust-skipped, and actual failed outcomes distinct in UI reporting.
- Plan 051 must consult local lifecycle ownership as well as hosted sessions if it later introduces reservation reclamation.

