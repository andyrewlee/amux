# Plan 046: Load shelf rows from the shared metadata snapshot

> **Executor instructions:** Follow each step and its verification. This is a self-contained implementation handoff; no audit-session context is assumed. Honor the STOP conditions. No commits or pushes are authorized.
>
> **Drift check (run first):**
> ~~~sh
> git diff --stat 7c530ee..HEAD -- internal/app/workspacesvc/workspace_service_load.go internal/app/workspacesvc/workspace_service_shelve.go internal/app/workspacesvc/workspace_service_shelve_test.go internal/app/workspacesvc/workspace_service_snapshot_test.go
> git diff HEAD -- internal/app/workspacesvc/workspace_service_load.go internal/app/workspacesvc/workspace_service_shelve.go internal/app/workspacesvc/workspace_service_shelve_test.go internal/app/workspacesvc/workspace_service_snapshot_test.go
> git status --short
> ~~~
> Compare BOTH commit changes and the current working tree against the Current state excerpts below. The audited baseline intentionally includes dirty user work on 2026-09-26. A known dirty file is not automatically a mismatch: preserve the audited behavior and the user's additional changes. On unexplained semantic drift, stop and report. New files will appear only in status, so inspect them before creating a same-named file.

## Status

- **Priority:** P2
- **Effort:** S
- **Risk:** LOW
- **Depends on:** none
- **Category:** perf
- **Planned at:** commit 7c530ee, 2026-09-26, INCLUDING the known dirty working tree (run-session picker/transcript-browser changes).
- **Status:** TODO

## Why this matters

LoadProjects already reads one shared metadata snapshot for live workspaces. Its shelf helper bypasses that snapshot and performs another full metadata scan for every project, even when no shelves exist. Reuse the paid-for snapshot while keeping row ordering and error behavior unchanged.

## Current state

- internal/app/workspacesvc/workspace_service_load.go:80 gathers the shared set:
~~~go
recordSet := s.loadWorkspaceRecordSet()
~~~
- The same project loop bypasses it at :128–132:
~~~go
project.Workspaces = workspaces
project.ShelvedWorkspaces = s.listShelvedWorkspaces(path)
projects = append(projects, *project)
~~~
- internal/app/workspacesvc/workspace_service_shelve.go:249–253:
~~~go
func (s *Service) listShelvedWorkspaces(repoPath string) []data.Workspace {
    if s == nil || s.store == nil {
        return nil
    }
    all, err := s.store.ListByRepoIncludingArchived(repoPath)
~~~
- internal/data/workspace_store.go:434–441 calls ListAll for every ListByRepo invocation.
- The existing listByRepoFromSet in workspace_service_load.go is the exact convention to reuse: set.ByRepo when available, otherwise the store interface. WorkspaceRecordSet.ByRepo preserves canonical matching, deduplication, ordering, and per-record error attribution.
- Vocabulary: only Archived && Shelved belongs to the shelf; accidental archives are excluded. Never mix shelf rows into project.Workspaces.

## Steps

### Step 1: Characterize live/shelf results

Read workspace_service_shelve_test.go and the recordSetLister/listByRepoFromSet helpers. Add a counting wrapper around a real temporary WorkspaceStore in new workspace_service_snapshot_test.go; increment ListAll and fallback-list calls without reproducing their implementation. Build at least two valid temporary Git projects with primary records, one live managed workspace, one intentional shelf, and one accidental archive.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/app/workspacesvc -run 'Test.*(LoadProjects|Shelv)' -count=1 → pre-existing behavior tests pass before the call-count assertion is tightened.

### Step 2: Pass the existing snapshot to the shelf helper

Change the helper to accept the already-loaded set and call s.listByRepoFromSet(set, repoPath, true). Update its sole production LoadProjects call and direct test callers. Keep nil service/store behavior, error logging, value-copy semantics, and Archived && Shelved filtering. Do not cache the set across separate loads.

Assert ListAll occurs exactly once per LoadProjects invocation with a snapshot-capable store and that both projects' shelves/live rows are identical to the expected records. Include zero shelves, archived-only rows, nil-store, snapshot load failure/fallback, and a store without ListAll capability; keep existing fallback behavior.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/app/workspacesvc -run 'Test.*(Snapshot|LoadProjects|Shelv)' -count=1 → one snapshot in the optimized path; expected fallback calls only when snapshot support/read is unavailable.

### Step 3: Verify the broader load path

Run all workspace-service tests and race coverage. No renderer, schema, lifecycle, key, or config behavior is changed, so no documentation or perf-baseline update is required. This plan proves fewer metadata reads structurally and makes no unmeasured p95 claim.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/app/workspacesvc ./internal/data → all pass. Then run every command below.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Focused package verification | env -u AMUX_WORKSPACES_ROOT go test ./internal/app/workspacesvc ./internal/data | All selected tests pass; no new skips |
| Race verification | env -u AMUX_WORKSPACES_ROOT go test -race ./internal/app/workspacesvc ./internal/data | Exit 0, no race reports |
| Standard repository gate | env -u AMUX_WORKSPACES_ROOT make devcheck | Exit 0; investigate and record baseline exceptions as described below |
| Changed-code strict gate | make lint-strict-new | Exit 0, zero new issues and clean formatter diff |
| Diff integrity | git diff --check | Exit 0 |

No dependency installation or module upgrade is needed. Format changed Go files with the repository's gofumpt-compatible tooling; make fmt is the repository formatting command, but do not accept unrelated formatting changes in this dirty checkout.

**Verification baseline:** Audit verification is not wholly green: the broad devcheck run failed in real e2e tests. An ambient AMUX_WORKSPACES_ROOT escaped the test HOME and caused a collision with the user's real workspace root; plan 059 owns that isolation defect. Until 059 lands, prefix every command below that can reach e2e with env -u AMUX_WORKSPACES_ROOT. The filtered-picker regression is separately owned by plan 036 and may affect e2e results. The real input verify-loop passed during the audit. Record exact remaining failures; do not fix unrelated failures, weaken checks, kill user sessions, or claim skipped real-tmux tests provide end-to-end validation.

## Scope

**In scope — only these paths may be changed for this implementation:**

- internal/app/workspacesvc/workspace_service_load.go
- internal/app/workspacesvc/workspace_service_shelve.go
- internal/app/workspacesvc/workspace_service_shelve_test.go
- internal/app/workspacesvc/workspace_service_snapshot_test.go
- plans/README.md — only this plan's status row, unless the reviewer maintains it.

**Out of scope:** all other source files; unrelated refactoring; dependency/toolchain upgrades; changes to the trust model or automatic Git push/checkout behavior; other plans' implementation. Do not expand wildcard test scope into production edits.

## Git workflow

Work on advisor/046-reuse-shelved-workspace-snapshot only if the operator has selected that branch; otherwise use their current checkout. Do not commit, push, open a PR, stash, clean, reset, or discard user edits without explicit instruction. Before editing, record git status --short and the existing diff. Existing dirty changes are input to this plan, not cleanup targets. Only this plan's implementation delta must fit Scope; pre-existing unrelated edits may remain. Update only this plan's row in plans/README.md at completion unless the reviewer maintains that index.

## Test plan

Use the regression cases and existing test exemplars in the steps. Tests must assert the observable outcome, not merely that a new helper was called. Use temporary repositories/metadata and isolated tmux servers, never the user's actual state. Channel/barrier-based scheduling is preferred over arbitrary pacing sleeps.

## Done criteria

- [ ] Every regression case in Steps passes with env -u AMUX_WORKSPACES_ROOT go test ./internal/app/workspacesvc ./internal/data.
- [ ] env -u AMUX_WORKSPACES_ROOT go test -race ./internal/app/workspacesvc ./internal/data exits 0 with no races.
- [ ] env -u AMUX_WORKSPACES_ROOT make devcheck and make lint-strict-new were run; both pass, or the exact independently established baseline blocker is recorded and this plan remains BLOCKED rather than DONE.

- [ ] git diff --check exits 0.
- [ ] git diff --name-only and git status --short, compared to the captured initial state, show no implementation edits outside Scope.
- [ ] User work is preserved; no commit/push occurred; this plan's status row is updated only when all required work is complete.

## STOP conditions

- The relevant live semantics differ from the excerpts and steps for reasons not explained by the declared dependencies or audited dirty baseline.
- A verification fails twice after a focused, reasonable fix attempt.
- The change requires production files outside Scope, alters a public behavior this plan explicitly preserves, or cannot be made without discarding user edits.
- If shelf listing must observe writes made after the shared snapshot within the same load, stop and demonstrate that requirement before changing consistency semantics.
- Do not move filters into a new cache, alter WorkspaceRecordSet, or expand this into a renderer optimization.
- A broad baseline check fails outside scope: report the exact test/error and retain BLOCKED status; do not silently waive the gate or repair unrelated code.

## Maintenance notes

- Every new per-project metadata consumer should take the existing recordSet where snapshot semantics suffice.
- Keep the fallback test: custom/test stores intentionally may not implement the optional optimization capability.

