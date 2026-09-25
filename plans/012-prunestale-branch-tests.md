# Plan 012: Cover `WorkspaceStore.PruneStale` secondary retention branches

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/data/workspace_store_prune.go internal/data/workspace_store_prune_test.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none (independent; shares no code with plan 002 beyond the same package)
- **Category**: tests
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`PruneStale` decides which workspace metadata records survive restarts — its grace periods exist specifically to protect against destroying metadata on transient filesystem trouble or timing races. Several retention branches have no test: the backup-file modtime fallback, orphan-lock pruning's error accumulation and name filtering, unreadable-metadata retention, the primary-checkout gate on `missing_root`, and the managed-root boundary check. A regression in any of these silently ages out or deletes records the grace logic exists to protect — and pruning bugs surface as "my workspace's setup/port/state vanished", the worst kind of data loss to diagnose.

## Current state

`internal/data/workspace_store_prune.go` — branches lacking coverage (line numbers at audit time; re-verify):

- `:216-229` — `metadataModTime`: falls back to the `.bak` file's modtime when the primary is gone (`backupErr` path).
- `:231-263` — `pruneOrphanLocks`: error accumulation (`errs` append), invalid-name skip, keep-when-metadata-exists.
- `:119-128` — unreadable metadata → record retained + error accumulated (must NOT be pruned).
- `:185` — `!ws.IsPrimaryCheckout()` gate on the `missing_root` reason (primary checkouts retain metadata even when the root is gone — verify this is the actual condition when you read the function).
- `:282-299` — `withinManagedRoot` exclusion (root outside managed root → retained).

Existing tests: four tests in `internal/data/workspace_store_prune_test.go` cover happy-path reconcile + cleanup-failure retention; `internal/data/workspace_store_shelve_test.go:27` covers the shelved gate. Test style: real `t.TempDir()` filesystem fixtures + real `WorkspaceStore` (this package tests against real files, not fakes).

Fixture-building primitives you'll need — find them in the existing test file: how tests mint a `WorkspaceRecord` (likely `store.Save`/`store.Upsert` or a record constructor), how `.bak` files are produced (check `internal/fsatomic` behavior or write the fixture file directly), the `LockPath`/lock-file naming for `pruneOrphanLocks` fixtures.

## Commands you will need

| Purpose    | Command                               | Expected on success |
|------------|---------------------------------------|---------------------|
| Build      | `go build ./internal/data`            | exit 0              |
| Unit tests | `go test ./internal/data -count=1`    | all pass            |
| Lint       | `make lint`                           | exit 0              |
| Full gate  | `make devcheck`                       | exit 0              |

## Scope

**In scope**:
- `internal/data/workspace_store_prune_test.go` (extend) or a new `workspace_store_prune_branches_test.go` if the file is getting long (file-length guard is 500 lines — check current size first).

**Out of scope**:
- `workspace_store_prune.go` production code — test-only plan. A test that exposes a *bug* in retention logic → STOP and report, don't fix in a test plan.
- `internal/fsatomic` — backup-file semantics are the fixture's dependency, not the subject.
- Other store files' version handling — plans/002 territory.

## Git workflow

- Branch: `advisor/012-prunestale-branch-tests` off `main`.
- Commit style: `test: cover PruneStale retention branches`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Map the five branches to fixtures

Read `workspace_store_prune.go` fully and, for each listed branch, write down the minimal fixture that reaches it:

- `.bak` modtime fallback: metadata dir with only `<id>.json.bak` (no primary) — or however `metadataModTime` resolves paths.
- `pruneOrphanLocks`: a lock file with (a) valid name + no matching metadata, (b) valid name + metadata present, (c) a name that fails the ID validation pattern, (d) an unreadable/removal-failing file if injectable — check whether removal errors are simulable (permission bits on tmpdir files are unreliable as root-owned; `os.Remove` failure may need a different trick — if none exists, cover the reachable branches and note the gap).
- Unreadable metadata: a `.json` file whose bytes don't parse.
- Primary-checkout gate: a record with `IsPrimaryCheckout() == true` whose root is deleted.
- `withinManagedRoot`: a record whose `Root` points outside the managed workspaces root.

**Verify**: you have a concrete fixture plan per branch — if any branch is unreachable without production seams, that's a STOP note (not a blocker for the others).

### Step 2: Write the tests

One `t.Run` subtest per branch inside a `TestPruneStale_*` umbrella (or separate `Test` funcs — match the file's existing organization). Each asserts the retention decision AND, where the code accumulates errors, the error surface (`errs` contents/length). Keep fixtures minimal — reuse the existing file's record-minting helpers.

**Verify**: `go test ./internal/data -run 'PruneStale' -count=1 -v` → all new cases pass.

### Step 3: Confirm branch coverage empirically

`go test ./internal/data -count=1 -coverprofile=/tmp/prune.cover && go tool cover -func=/tmp/prune.cover | grep -E 'metadataModTime|pruneOrphanLocks|withinManagedRoot|PruneStale'` — confirm the named functions' coverage rose (the exact percentages matter less than that the named branches are now entered; `go tool cover -html` offline if a branch still shows 0%).

**Verify**: coverage output shows the functions exercised.

### Step 4: Full gate

**Verify**: `make devcheck` → exit 0.

## Test plan

- The new subtests themselves are the deliverable (Step 2 list).
- Structural pattern: existing `workspace_store_prune_test.go` fixtures.
- Edge cases: invalid lock names, parse-failure retention, primary-checkout missing root, outside-root record.
- Verification: `go test ./internal/data -count=1` → all pass.

## Done criteria

- [ ] Each listed branch has a dedicated test asserting retention vs pruning.
- [ ] `go test ./internal/data -count=1` exits 0 with the new tests.
- [ ] Coverage instrumentation confirms the named functions are exercised.
- [ ] `make devcheck` exits 0.
- [ ] No production files modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- A branch turns out to be dead code (unreachable by construction) — report it as a dead-code finding, don't test unreachable paths.
- A "retention" test fails because the code actually *prunes* — you've found a real bug; STOP and report with the fixture.
- `IsPrimaryCheckout` semantics changed (drift) — re-derive the gate before writing the fixture.

## Maintenance notes

- `PruneStale` is a data-loss boundary: any future change to its retention rules needs a test per rule — extend this file's table, don't rely on the big reconcile test.
- If lock-file naming moves, `pruneOrphanLocks` fixtures must move with it.
