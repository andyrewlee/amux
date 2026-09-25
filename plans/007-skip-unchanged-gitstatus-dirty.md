# Plan 007: Skip dashboard/sidebar work when a `GitStatusResult` carries no change

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/app/app_input_messages_workspace_gitstatus.go internal/app/app_operations.go internal/ui/dashboard/model_update.go internal/ui/dashboard/model_dirty_gate_test.go internal/ui/sidebar/model_lifecycle.go internal/ui/sidebar/model.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MED
- **Depends on**: none
- **Category**: perf
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

Every 3 seconds the git-status ticker delivers a `GitStatusResult` for each workspace; in the idle steady state the status hasn't changed, but the pipeline still (a) bumps the dashboard `contentVersion` — because `dashboard.Update`'s funnel marks *every* handled message dirty via a blanket `defer` — and (b) rebuilds the sidebar display list — because `sidebar.SetGitStatus` unconditionally runs `rebuildDisplayList()` (O(changes) + per-item `strings.ToLower`). The dirty-gate discipline already exists (`internal/ui/dashboard/model_dirty_gate_test.go` enforces it for `SetActiveWorkspaces`/`SetAgentStates` via `maps.Equal` early-outs); this path is the inconsistent holdout — a guaranteed content-version bump + list rebuild every 3s at full idle.

## Current state

`internal/app/app_input_messages_workspace_gitstatus.go:10-17`:

```go
func (a *App) handleGitStatusResult(msg messages.GitStatusResult) tea.Cmd {
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	if a.activeWorkspace != nil && rootsReferToSameWorkspace(msg.Root, a.activeWorkspace.Root) {
		a.sidebar.SetGitStatus(msg.Status)
	}
	return cmd
}
```

- `internal/app/app_operations.go:191-198` — `requestGitStatusCached` returns the same `*git.StatusResult` pointer on cache hits, so pointer equality is a valid no-change signal for the sidebar path.
- `internal/ui/dashboard/model_update.go:~19` — `defer m.markContentDirty()` at the top of `Update`: every message that reaches the switch bumps the version. The status write `m.statusCache[msg.Root] = msg.Status` is a no-op when the pointer is identical, but the defer fires regardless.
- `internal/ui/sidebar/model_lifecycle.go:107-111` — `SetGitStatus` → `rebuildDisplayList()` + `markContentDirty()` unconditionally.
- `internal/ui/sidebar/model.go:111-164` — `rebuildDisplayList` cost.
- `internal/ui/dashboard/model_dirty_gate_test.go:10` — the existing convention: gate tests assert unchanged-state publishes do NOT bump `contentVersion`. Match it.

## Commands you will need

| Purpose    | Command                                    | Expected on success |
|------------|--------------------------------------------|---------------------|
| Build      | `go build ./internal/app ./internal/ui/...`| exit 0              |
| Unit tests | `go test ./internal/app ./internal/ui/dashboard ./internal/ui/sidebar -count=1` | all pass |
| Perf check | `make perf-check` (render-path adjacency)  | within baselines or reported delta |
| Lint       | `make lint && make lint-strict-new`        | exit 0              |
| Full gate  | `make devcheck`                            | exit 0              |

## Scope

**In scope**:
- `internal/app/app_input_messages_workspace_gitstatus.go` — route unchanged results around `dashboard.Update`.
- `internal/ui/dashboard/model_update.go` — if needed, replace the blanket `defer` with per-case marking for the `GitStatusResult` case ONLY (see Step 2 decision).
- `internal/ui/sidebar/model_lifecycle.go` — pointer-equality guard in `SetGitStatus`.
- `internal/ui/dashboard/model_dirty_gate_test.go` and/or the sidebar test file — extend the gate tests.
- `internal/app/*_test.go` covering `handleGitStatusResult` if one exists (`grep -rn 'handleGitStatusResult\|GitStatusResult' internal/app --include='*_test.go'`).

**Out of scope**:
- `requestGitStatusCached` / the singleflight machinery — the cache is the mechanism this plan exploits; don't change it.
- The 3s tick interval (`internal/app/defaults.go:10-11`) — not a finding.
- `handleGitStatusBatchResult` — it already writes the cache map directly; check it has the same dirty-gate discipline and report, don't expand.
- Refactoring the whole `defer` funnel for all message types — that broader change is deliberately out (risk); this plan gates only the `GitStatusResult` case.
- `rootsReferToSameWorkspace` semantics — plans/022 covers predicate alignment.

## Git workflow

- Branch: `advisor/007-gitstatus-dirty-gate` off `main`.
- Commit style: `perf: skip dashboard/sidebar rebuild on unchanged git status`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Sidebar pointer guard

In `sidebar.SetGitStatus` (model_lifecycle.go:107-111), early-return when the incoming pointer equals the stored one:

```go
func (m *Model) SetGitStatus(status *git.StatusResult) {
	if status == m.gitStatus {
		return
	}
	m.gitStatus = status
	m.rebuildDisplayList()
	m.markContentDirty()
}
```

This is correct *because* `requestGitStatusCached` returns the cached pointer on hits — document that dependency in a one-line comment (pointer identity is the change signal, not content equality).

**Verify**: `go build ./internal/ui/sidebar` → exit 0.

### Step 2: Dashboard gate — two acceptable shapes, pick the safer one

Option A (preferred, smaller blast radius): short-circuit in `handleGitStatusResult` — before calling `a.dashboard.Update(msg)`, check `a.dashboard`'s cached status for `msg.Root`. If the dashboard exposes the status cache read (`grep -n 'statusCache' internal/ui/dashboard/*.go`), compare `msg.Status` pointer vs cached; skip the `Update` call entirely on equality. If no accessor exists, add a tiny `dashboard.GitStatusChanged(root, status) bool` method (or `StatusCacheHit`) — a read-only check, no funnel surgery.

Option B (only if A is impossible without funnel changes): in `dashboard.Update`, handle `messages.GitStatusResult` above the switch with an explicit early-return-before-defer — i.e. move the no-change case *above* `defer m.markContentDirty()` so unchanged results never reach the dirty path. This requires the defer to not be the first statement — check the function shape; if the defer must stay first, Option A.

**Verify**: `go build ./internal/app ./internal/ui/dashboard` → exit 0; `go test ./internal/app ./internal/ui/dashboard -count=1` → pass.

### Step 3: Extend the gate tests

- `internal/ui/dashboard/model_dirty_gate_test.go`: add `GitStatusResult` cases — same-pointer publish does NOT bump `contentVersion`; a new-pointer publish DOES.
- Sidebar: add `SetGitStatus` cases — same pointer → `rebuildDisplayList` not invoked (assert via an observable: e.g. a marked counter isn't exposed, so assert via `contentVersion`/dirty flag or a reorder-sensitive fixture — check how the existing sidebar tests observe rebuilds; `grep -rn 'rebuildDisplayList\|SetGitStatus' internal/ui/sidebar --include='*_test.go'`).

**Verify**: `go test ./internal/ui/dashboard ./internal/ui/sidebar -count=1 -run 'Dirty|GitStatus' -v` → pass.

### Step 4: Gates

**Verify**: `make devcheck` → exit 0; `make lint-strict-new` → exit 0; `make perf-check` → within baseline (this touches the dashboard update path; a p95 regression means re-examine).

## Test plan

- New: dirty-gate cases for the unchanged/changed `GitStatusResult` (dashboard), pointer-identity rebuild skip (sidebar).
- Existing: `TestHandleGitStatus*` / `handleGitStatusResult` tests keep passing — the happy path (status actually changes → UI updates) must be pinned by at least one test.
- Structural pattern: `internal/ui/dashboard/model_dirty_gate_test.go` — it exists precisely for this convention; follow its helper style.

## Done criteria

- [ ] An unchanged (same-pointer) `GitStatusResult` causes zero `contentVersion` bump on the dashboard and zero `rebuildDisplayList` on the sidebar — asserted by tests.
- [ ] A changed status still propagates fully (asserted by tests).
- [ ] `go test ./internal/app ./internal/ui/dashboard ./internal/ui/sidebar -count=1` exits 0.
- [ ] `make devcheck`, `make lint-strict-new`, `make perf-check` all pass.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- `requestGitStatusCached` no longer returns pointer-stable results (e.g. it deep-copies per call) — pointer equality becomes wrong; switch to a cheap content compare (`maps.Equal` on the status fields) or report.
- `dashboard.Update`'s funnel was refactored so the blanket defer no longer exists — verify the no-change gate is still needed, then apply Option A regardless.
- `SetGitStatus` callers exist that publish mutated-in-place statuses (same pointer, different content) — `grep -rn 'SetGitStatus' internal/` and check each producer; if any mutates in place, pointer-compare is unsafe → report.
- `handleGitStatusBatchResult` shares the funnel and needs the identical treatment — if folding it in changes more than ~20 lines, leave it and note in the PR.

## Maintenance notes

- Pointer-identity-as-change-signal is a contract with `requestGitStatusCached`: if the cache ever returns reconstructed copies, the gate silently under-publishes. Add a comment at `SetGitStatus` naming that dependency, and a test asserting the cache returns the identical pointer on hits (one may already exist in `internal/app`).
- The broader "replace the blanket defer with per-case marking" refactor stays deferred — the funnel's defense-in-depth is intentional; don't expand this plan into it.
- Reviewer focus: the gate must fire *only* on true no-op publishes — a workspace's first-ever status (empty cache slot) must count as changed.
