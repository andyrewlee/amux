# Plan 035: Recheck the approved merge destination at execution

> **Executor instructions:** Follow each step and its verification. This is a self-contained implementation handoff; no audit-session context is assumed. Honor the STOP conditions. No commits or pushes are authorized.
>
> **Drift check (run first):**
> ~~~sh
> git diff --stat 7c530ee..HEAD -- internal/app/app_workspace_merge.go internal/app/app_workspace_merge_test.go internal/app/workspace_merge_integration_test.go internal/git/merge.go internal/git/merge_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md
> git diff HEAD -- internal/app/app_workspace_merge.go internal/app/app_workspace_merge_test.go internal/app/workspace_merge_integration_test.go internal/git/merge.go internal/git/merge_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md
> git status --short
> ~~~
> Compare BOTH commit changes and the current working tree against the Current state excerpts below. The audited baseline intentionally includes dirty user work on 2026-09-26. A known dirty file is not automatically a mismatch: preserve the audited behavior and the user's additional changes. On unexplained semantic drift, stop and report. New files will appear only in status, so inspect them before creating a same-named file.

## Status

- **Priority:** P1
- **Effort:** S
- **Risk:** MED
- **Depends on:** none
- **Category:** bug
- **Planned at:** commit 7c530ee, 2026-09-26, INCLUDING the known dirty working tree (run-session picker/transcript-browser changes).
- **Status:** TODO

## Why this matters

The merge dialog proves the primary checkout is on the requested base before asking for confirmation. The user or another agent can change that checkout while the dialog is open. Today confirmation merges into its new HEAD and reports the old destination. The command must refuse that stale approval without changing branches.

## Current state

- internal/app/app_workspace_merge.go:81–95 performs the asynchronous preflight:
~~~go
head, err := resolveHead(ws.Repo)
// ... error handling ...
if head != base {
    return refusal(ws, fmt.Sprintf(
        "Cannot merge: %s is on '%s', not the base '%s'. Check out '%s' there and retry.",
        ws.Repo, head, base, base))
}
return messages.ShowMergeWorkspaceDialog{Workspace: ws, Base: base}
~~~
- The execution closure at internal/app/app_workspace_merge.go:182–187 does not recheck:
~~~go
return messages.WorkspaceMerged{
    Workspace: ws,
    Base:      base,
    Err:       merge(ctx, repo, branch),
}
~~~
- internal/git/merge.go:126 executes git merge --no-ff -- branch. Its API intentionally merges the current checkout and never checks out the base for the user.
- The success toast at internal/app/app_workspace_merge.go:212 uses msg.Base.
- Follow the existing refusal(ws, reason) / messages.MergeWorkspaceRefused path and error wrapping in handleMergeWorkspaceRefused. Existing exemplars are TestMergeConfirmDialog_RunsMergeWithVerifiedBase and TestMergeWorkspace_EndToEndRefusesWrongBranch.
- ARCHITECTURE.md says long-running operations belong in tea.Cmd. Keep both revalidation and merge off Update. Product constraints: no checkout, fetch, rebase, autostash, or push.

## Behavior decisions

Compare the exact local branch name approved by the dialog against CheckedOutBranch immediately before calling the mutation. Empty approved base, detached HEAD, a failed HEAD query, or a different branch must prevent merge. Do not silently recompute a different base and proceed. This closes the human confirmation interval; it does not promise atomic exclusion of arbitrary external Git writers after the final check.

## Steps

### Step 1: Establish the current seam and baseline

Read the two existing merge test files and follow how confirmation dispatches mergeWorkspaceAsync. Run drift checks. Keep the approved base as immutable command input; do not read mutable dialog state from the command.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/app ./internal/git -run 'Test.*Merge' -count=1 → existing merge tests pass, or record a baseline failure and stop before changing behavior.

### Step 2: Revalidate inside the execution command

In mergeWorkspaceAsync, capture the HEAD resolver on the Update goroutine along with repo/source/base. In its command, reject an empty base, resolve current HEAD, and return the existing refusal/error message without invoking merge on any mismatch. On success invoke the existing merge seam with unchanged semantics. Preserve the source branch/name snapshot and avoid adding synchronous Git work to Update. Keep Git's lower-level merge API unchanged unless a narrow helper in merge.go materially simplifies shared validation.

Extend the seam-based test to let preflight return the approved branch and execution return a different branch; count merge calls and require zero. Add detached/query-error/blank-approved-base variants, successful unchanged branch, and cancellation with zero execution checks.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/app -run 'Test.*Merge' -count=1 → all variants pass; mismatches emit refusal and never invoke the merge seam.

### Step 3: Pin real Git behavior and document the boundary

Extend workspace_merge_integration_test.go using its existing temporary-repository helpers: open confirmation on main, switch the primary checkout to another local branch, then confirm. Record both refs before and after; neither may move. Also exercise the successful unchanged-branch flow and existing conflict/abort flow. Document the execution-time refusal in README.md and cross-reference the same contract in docs/CONFIG.md and docs/ORCHESTRATION.md without adding a new config key or overstating external-writer safety.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/app ./internal/git -run 'Test.*Merge' -count=1 → all real Git and seam cases pass. Then run all Commands you will need.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Focused package verification | env -u AMUX_WORKSPACES_ROOT go test ./internal/app ./internal/git | All selected tests pass; no new skips |
| Race verification | env -u AMUX_WORKSPACES_ROOT go test -race ./internal/app ./internal/git | Exit 0, no race reports |
| Standard repository gate | env -u AMUX_WORKSPACES_ROOT make devcheck | Exit 0; investigate and record baseline exceptions as described below |
| Changed-code strict gate | make lint-strict-new | Exit 0, zero new issues and clean formatter diff |
| Real input path | env -u AMUX_WORKSPACES_ROOT make verify-loop | Both real-agent keystroke tests pass without skips |
| Real tmux lifecycle suite | env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e | Exit 0; report skips/baseline blockers |
| Diff integrity | git diff --check | Exit 0 |

No dependency installation or module upgrade is needed. Format changed Go files with the repository's gofumpt-compatible tooling; make fmt is the repository formatting command, but do not accept unrelated formatting changes in this dirty checkout.

**Verification baseline:** Audit verification is not wholly green: the broad devcheck run failed in real e2e tests. An ambient AMUX_WORKSPACES_ROOT escaped the test HOME and caused a collision with the user's real workspace root; plan 059 owns that isolation defect. Until 059 lands, prefix every command below that can reach e2e with env -u AMUX_WORKSPACES_ROOT. The filtered-picker regression is separately owned by plan 036 and may affect e2e results. The real input verify-loop passed during the audit. Record exact remaining failures; do not fix unrelated failures, weaken checks, kill user sessions, or claim skipped real-tmux tests provide end-to-end validation.

## Scope

**In scope — only these paths may be changed for this implementation:**

- internal/app/app_workspace_merge.go
- internal/app/app_workspace_merge_test.go
- internal/app/workspace_merge_integration_test.go
- internal/git/merge.go
- internal/git/merge_test.go
- README.md
- docs/CONFIG.md
- docs/ORCHESTRATION.md
- plans/README.md — only this plan's status row, unless the reviewer maintains it.

**Out of scope:** all other source files; unrelated refactoring; dependency/toolchain upgrades; changes to the trust model or automatic Git push/checkout behavior; other plans' implementation. Do not expand wildcard test scope into production edits.

## Git workflow

Work on advisor/035-recheck-merge-destination only if the operator has selected that branch; otherwise use their current checkout. Do not commit, push, open a PR, stash, clean, reset, or discard user edits without explicit instruction. Before editing, record git status --short and the existing diff. Existing dirty changes are input to this plan, not cleanup targets. Only this plan's implementation delta must fit Scope; pre-existing unrelated edits may remain. Update only this plan's row in plans/README.md at completion unless the reviewer maintains that index.

## Test plan

Use the regression cases and existing test exemplars in the steps. Tests must assert the observable outcome, not merely that a new helper was called. Use temporary repositories/metadata and isolated tmux servers, never the user's actual state. Channel/barrier-based scheduling is preferred over arbitrary pacing sleeps.

## Done criteria

- [ ] Every regression case in Steps passes with env -u AMUX_WORKSPACES_ROOT go test ./internal/app ./internal/git.
- [ ] env -u AMUX_WORKSPACES_ROOT go test -race ./internal/app ./internal/git exits 0 with no races.
- [ ] env -u AMUX_WORKSPACES_ROOT make devcheck and make lint-strict-new were run; both pass, or the exact independently established baseline blocker is recorded and this plan remains BLOCKED rather than DONE.

- [ ] env -u AMUX_WORKSPACES_ROOT make verify-loop: Both real-agent keystroke tests pass without skips.
- [ ] env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e: Exit 0; report skips/baseline blockers.
- [ ] git diff --check exits 0.
- [ ] git diff --name-only and git status --short, compared to the captured initial state, show no implementation edits outside Scope.
- [ ] User work is preserved; no commit/push occurred; this plan's status row is updated only when all required work is complete.

## STOP conditions

- The relevant live semantics differ from the excerpts and steps for reasons not explained by the declared dependencies or audited dirty baseline.
- A verification fails twice after a focused, reasonable fix attempt.
- The change requires production files outside Scope, alters a public behavior this plan explicitly preserves, or cannot be made without discarding user edits.
- Do not add an automatic checkout or attempt to lock arbitrary external Git clients. If a different requested destination must be supported, obtain a revised plan.
- If confirmation already revalidates HEAD in a changed caller, prove whether the defect remains before duplicating checks.
- A broad baseline check fails outside scope: report the exact test/error and retain BLOCKED status; do not silently waive the gate or repair unrelated code.

## Maintenance notes

- Future merge destinations must flow as immutable approved inputs into this execution check.
- Do not treat the dialog's preflight as authorization for a branch discovered later. Preserve its warning and cancellation behavior.

