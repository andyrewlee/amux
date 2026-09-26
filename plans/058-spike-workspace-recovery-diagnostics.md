# Plan 058: Specify workspace recovery diagnostics and safe retry boundaries

> **Executor instructions:** Complete a plans-only design spike. Do not add recovery actions, change deletion, or expose new CLI commands. Record a GO / DEFER / REJECT verdict and update this row in `plans/README.md`; no commit or push.
>
> **Drift check:** Run `git diff --stat 7c530ee..HEAD -- internal/app/app_workspace_status.go internal/app/workspacesvc/workspace_delete_tombstone.go internal/app/workspacesvc/workspace_service_delete.go internal/git/workspace_errors.go internal/data README.md docs/CONFIG.md docs/ORCHESTRATION.md` and `git diff HEAD --` with the same paths. Compare the excerpts and reconcile the intended changes from the dependency plans before specifying new behavior.

## Status

- **Priority:** P3
- **Effort:** S for spike; M estimated for a follow-up
- **Risk:** LOW for spike; MED for any later retry action
- **Depends on:** plans 039 (workspace transactions), 041 (lifecycle process teardown), and 048 (bounded Git cancellation) before final build handoff
- **Category:** direction
- **Planned at:** commit `7c530ee` plus existing working-tree changes, 2026-09-26

## Why this matters

A failed deletion can leave a tombstone, surviving metadata, or a branch needing cleanup. The status dialog currently explains ports, scripts, trust, and sessions but not why cleanup remains pending. A small diagnostic surface could reduce guesswork; first distinguish facts the app actually knows from states that would require new durable records, and constrain any retry to existing authorized lifecycle operations.

## Current state

`internal/app/app_workspace_status.go:48` opens status through an async run-session read and rejects stale completions with a dialog token. Its snapshot explicitly avoids env values:

```go
// envKeys are the merged custom-env key NAMES (repo + project +
// workspace layers), sorted — names only, never values —
// values can hold secrets).
```

Keep this privacy rule and the one-shot-at-open behavior unless the design justifies a change. Slow file/Git/tmux probes must not move onto Bubble Tea's Update goroutine. `README.md` calls `i` a read-only snapshot; an action would change that contract.

`internal/app/workspacesvc/workspace_delete_tombstone.go:100` keeps a surviving worktree usable:

```go
if DirExists(ws.Root) {
	// A surviving worktree means an earlier delete failed before removing it;
	// do not finish the delete — the workspace must stay usable.
	return false
}
```

The same function retries session cleanup, branch deletion, and metadata deletion once the root is gone. On branch deletion failure it logs, preserves the tombstone, and surfaces the row. A tombstone is evidence of an attempted deletion, not a durable exact-stage or latest-error journal. `internal/git/workspace_errors.go:9` provides existing typed errors:

```go
ErrUnregisteredWorkspacePath = errors.New("workspace is not a registered worktree but still exists on disk")
ErrWorkspaceCleanupPending   = errors.New("workspace cleanup is still pending")
```

Match the fixture convention in `internal/app/workspacesvc/workspace_delete_tombstone_test.go:19`:

```go
store := data.NewWorkspaceStore(t.TempDir())
var deletedBranch string
svc := New(nil, store, nil, "")
svc.gitOps = &testutil.FakeGitOps{DeleteBranchFunc: func(repoPath, branch string) error {
	deletedBranch = branch
	return nil
}}
```

The related test `TestFinishInterruptedDelete_KeepsTombstonedWithLiveWorktree` pins the safety boundary. `docs/ORCHESTRATION.md` rejects a new lifecycle CLI without a concrete unmet orchestration requirement; this feature does not establish one.

## Commands you will need

| Purpose | Command | Expected result |
|---|---|---|
| Recovery characterization | `go test ./internal/app/workspacesvc -run '^TestFinishInterruptedDelete_' -count=1` | PASS |
| Status characterization | `go test ./internal/app -run '^TestBuildWorkspaceStatus_' -count=1` | PASS |
| Inventory recovery surface | `rg -n 'MarkDeleting|IsDeleting|finishInterruptedDelete|ErrWorkspaceCleanupPending|ClearDeleting' internal` | every state/action source mapped |
| Artifact validation | Python command in Step 3 | exit 0 |
| Whitespace | `git diff --check` | exit 0 |

No production gate is needed for plans-only changes. The eventual implementation must specify `env -u AMUX_WORKSPACES_ROOT make devcheck` (until 059 lands), `make lint-strict-new`, `env -u AMUX_WORKSPACES_ROOT make verify-loop`, relevant service/data/app race tests, and real tmux tests if retry controls sessions. A render change also requires `make harness-presets` and `PERF_STRICT=1 make perf-check`.

## Scope

**Writes:** `plans/spikes/058/decision.md`, `plans/spikes/058/recovery-cases.json`, `plans/spikes/058/implementation.md`, this plan and its index row.

**Read-only inputs:** source/docs listed above, status tests, service deletion/tombstone tests, data tombstone implementation, and the final dependency-plan diffs.

**Out of scope:** new destructive authority, cleanup of actual user worktrees, a CLI, background retry redesign, new broad event/log storage, raw env values, automatic force-delete, changing Git branch ownership checks, implementing any chosen feature during the spike.

## Git workflow

Suggested branch: `advisor/058-recovery-diagnostics-spike`. Preserve the existing dirty tree; no stash/reset/commit/push. All output stays under `plans/`.

## Steps

### Step 1: Build an evidence-backed recovery state table

In `decision.md`, write `## Evidence` and `## State model`. List each combination the current code distinguishes: no tombstone, tombstone with root present, root missing with session cleanup pending, branch cleanup failure, metadata deletion failure, and completed cleanup. For each specify the observable source, whether it survives restart, whether the row is visible, and what the app cannot know. Include alias metadata IDs and the no-longer-present workspace case.

Do not label an exact stage or error from a boolean marker alone. A diagnostic read must have no cleanup side effects. If new structured persistence is proposed, identify its schema, ownership, lifetime, bounded/sanitized fields, unknown-version policy, and how plan 039 transactions avoid overwriting it. Compare that cost with a read-only initial feature that says only "cleanup pending" and links to existing logs.

**Verify:** both characterization commands above → PASS; `rg -n '^## (Evidence|State model)$' plans/spikes/058/decision.md` → both headings present.

### Step 2: Define presentation and the retry authorization boundary

Add `## Presentation`, `## Retry safety`, `## Alternatives`, and `## Decision`. Supply synthetic text mockups for ordinary status, known pending cleanup, incomplete/unknown information, and a failed refresh. Prefer a read-only first increment. If a retry action is proposed, name the exact existing service operation, preconditions, confirmation behavior, workspace identity fence, cancellation limit, ownership check, and error reporting. A marker with a live root must never become permission to delete that root automatically. Opening status must never run archive scripts or mutate tombstones.

Explicitly decide whether latest error/stage is durable or session-local and label it truthfully. Preserve names-only env display. Keep user-requested retry failures at `logging.Error`; background recovery remains `logging.Warn` under the project's logging convention. Handle the status-dialog token and selected workspace changing during a probe.

Create `recovery-cases.json` with a `decision` enum GO/DEFER/REJECT and at least ten `cases`, each having unique `id`, `given`, `action`, `expected`, and `may_mutate` boolean. Include opening status for every recovery state, stale result delivery, retries when root reappears or identity changes, and session/branch/metadata failures. For every diagnostic-open case, `may_mutate` must be false.

**Verify:** `python3 -c 'import json; from pathlib import Path; d=json.loads(Path("plans/spikes/058/recovery-cases.json").read_text()); assert d["decision"] in ("GO","DEFER","REJECT"); c=d["cases"]; assert len(c)>=10 and len({x["id"] for x in c})==len(c); assert all(all(x.get(k) for k in ("id","given","action","expected")) and isinstance(x["may_mutate"],bool) for x in c)'` → exit 0.

### Step 3: Produce the build handoff and record the verdict

If GO, write a standalone `implementation.md`: final presentation choice, exact production/test files, data access with no side effects, async result fencing, bounded error sanitization, any approved persistence/retry changes, tests mapped from JSON, exact verification commands with expected results, STOP conditions, and updates to README.md/docs/CONFIG.md/docs/ORCHESTRATION.md. If DEFER/REJECT, record the missing evidence or unacceptable cost and a concrete revisit trigger. Do not create a build-everything backlog.

**Verify:** `python3 -c 'from pathlib import Path; p=Path("plans/spikes/058"); assert all((p/n).is_file() and (p/n).stat().st_size>0 for n in ("decision.md","recovery-cases.json","implementation.md")); s=(p/"decision.md").read_text(); assert all("## "+h in s for h in ("Evidence","State model","Presentation","Retry safety","Alternatives","Decision"))'` → exit 0; rerun Step 2's JSON check and `git diff --check` → exit 0.

## Test plan

This spike writes scenarios, not application tests. The future implementation must extend `app_workspace_status_test.go` for rendering/async identity and service tombstone tests for any permitted retry. Use `TestFinishInterruptedDelete_KeepsTombstonedWithLiveWorktree` and `TestFinishInterruptedDelete_SurfacesWorkspaceWhenBranchDeleteFails` as safety exemplars. Assert exact mutation calls (including zero calls during diagnostics), not just displayed strings.

## Done criteria

- [ ] All three artifacts exist; Python validations and `git diff --check` pass.
- [ ] Every displayed field has a named authoritative source and unknown-state behavior.
- [ ] Read-only opens have no side effects; any retry reuses a named, bounded existing authority with fresh validation.
- [ ] Verdict is consistent across artifacts; GO has a self-contained implementation handoff.
- [ ] Only plans changed; index records spike verdict, not shipped feature status.

## STOP conditions

Stop if the desired diagnosis cannot be derived without introducing a broad event journal, or if retry requires bypassing existing deletion/trust/ownership protections. Record an unresolved dependency if 039/041/048 change these boundaries. No real-user recovery experiment is part of this spike. If a verification fails twice, report it instead of fixing unrelated source.

## Maintenance notes

Recovery text is a user contract: keep unknown/pending states distinct from confirmed failures. Revisit the state table whenever tombstone identity, lifecycle phases, or Git timeout semantics change. Preserve the existing rejection of an unsolicited lifecycle CLI.
