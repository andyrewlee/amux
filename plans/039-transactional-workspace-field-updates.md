# Plan 039: Apply workspace metadata changes as ordered field transactions

> **Executor instructions:** Follow each step and its verification. This is a self-contained implementation handoff; no audit-session context is assumed. Honor the STOP conditions. No commits or pushes are authorized.
>
> **Drift check (run first):**
> ~~~sh
> git diff --stat 7c530ee..HEAD -- internal/data/workspace_store.go internal/data/workspace_store_env.go internal/data/workspace_store_scripts.go internal/data/workspace_store_update.go internal/data/workspace_store_update_test.go internal/data/workspace_store_env_test.go internal/data/workspace_store_scripts_test.go internal/app/app_persistence.go internal/app/workspace_lifecycle_state.go internal/app/workspacesvc/services.go internal/app/workspacesvc/workspace_service.go internal/app/workspacesvc/workspace_service_init.go internal/app/workspacesvc/workspace_service_load.go internal/app/workspacesvc/workspace_service_shelve.go internal/app/workspacesvc/workspace_tab_persistence.go internal/app/*_test.go internal/app/workspacesvc/*_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md
> git diff HEAD -- internal/data/workspace_store.go internal/data/workspace_store_env.go internal/data/workspace_store_scripts.go internal/data/workspace_store_update.go internal/data/workspace_store_update_test.go internal/data/workspace_store_env_test.go internal/data/workspace_store_scripts_test.go internal/app/app_persistence.go internal/app/workspace_lifecycle_state.go internal/app/workspacesvc/services.go internal/app/workspacesvc/workspace_service.go internal/app/workspacesvc/workspace_service_init.go internal/app/workspacesvc/workspace_service_load.go internal/app/workspacesvc/workspace_service_shelve.go internal/app/workspacesvc/workspace_tab_persistence.go internal/app/*_test.go internal/app/workspacesvc/*_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md
> git status --short
> ~~~
> Compare BOTH commit changes and the current working tree against the Current state excerpts below. The audited baseline intentionally includes dirty user work on 2026-09-26. A known dirty file is not automatically a mismatch: preserve the audited behavior and the user's additional changes. On unexplained semantic drift, stop and report. New files will appear only in status, so inspect them before creating a same-named file.

## Status

- **Priority:** P1
- **Effort:** M
- **Risk:** MED
- **Depends on:** none
- **Category:** tech-debt
- **Planned at:** commit 7c530ee, 2026-09-26, INCLUDING the known dirty working tree (run-session picker/transcript-browser changes).
- **Status:** TODO

## Why this matters

Atomic JSON replacement prevents torn files but does not merge independent changes. A tab-save command captures the whole workspace, so executing it after a successful name/env/script edit silently restores stale fields. The field setters also load outside the workspace lock and can overwrite each other. Add a narrow transactional boundary and preserve the order of tab snapshots.

## Current state

- internal/app/app_persistence.go:125–129 captures a whole object:
~~~go
tabs, activeIdx := a.center.GetTabsInfoForWorkspace(wsID)
ws.OpenTabs = tabs
ws.ActiveTabIndex = activeIdx
snapshots = append(snapshots, snapshotWorkspaceForSave(ws))
~~~
Its later command calls service.Save(snap) at :146–147. Shutdown performs another complete save synchronously.
- internal/data/workspace_store.go:234–235:
~~~go
ws.Version = workspaceFileVersion
if err := fsatomic.WriteJSON(path, ws); err != nil {
    return fmt.Errorf("save workspace %s: %w", id, err)
}
~~~
- internal/data/workspace_store_env.go:20–31 loads first and calls Save only after changing Env. SetScripts and Rename have the same shape.
- Existing internal/data/workspace_store_discovery.go:43–77 is the locking exemplar: lockWorkspaceIDs, fresh load while locked, merge, then saveWorkspaceLocked. Do not call Save while holding that flock; flock is per-open-fd and not reentrant.
- internal/app/workspacesvc/services.go declares persistence requirements explicitly on WorkspaceStore; do not add an optional type assertion with a behaviorally weaker fallback.
- Architecture constraints: App.Update is the only writer of UI state. Worktree create/delete/shelve/restore share lifecycle mutation guards. Persisted MetadataID is authoritative; discovery owns Repo/Root/Branch, UI persistence owns OpenTabs/ActiveTabIndex. A dirty marker survives a failed lifecycle mutation.

## Behavior decisions

Introduce WorkspaceStore.Update(id, func(*Workspace) (changed bool, err error)) error as a fresh-load/mutate/write transaction under the existing ID flock. It does not create missing metadata, follow a callback-selected new ID, migrate paths, clear tombstones, or call external code. Reject identity/schema changes in the callback; preserve the existing future-schema refusal. A false changed result performs no disk write.

Tab persistence updates only OpenTabs and ActiveTabIndex. Store field setters update only their named fields. Existing lifecycle flag updates update only Archived/Shelved/ArchivedAt using the same transaction. Full Save remains for intentional complete creation/import cases, not delayed UI snapshots.

For two queued tab snapshots in one App, newer accepted capture wins even if commands start in reverse order. Put a monotonic capture sequence on the Update-owned persistence state; send it with the immutable tab snapshot. The workspace service owns a per-ID mutex and last-successful sequence, checks order and writes under that mutex. It does not hold the global lifecycle lock while waiting for unrelated workspaces. Shutdown captures a newer sequence and uses the same service path, waiting behind any current write. Across distinct app instances, tab state retains existing last-committed-writer semantics; this plan does not invent a distributed tab-list CRDT.

## Steps

### Step 1: Characterize lost fields and reversed saves

Read app_persistence_test.go and workspace_persist_inflight_test.go; preserve their fake-store conventions. Add a deterministic fixture that captures the command returned by handlePersistDebounce, applies SetEnv/SetScripts/Rename, then executes the saved command. Assert both the new field and intended tabs survive. Add two captures executed newer-first and a shutdown flush while an older write is blocked on a barrier.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/app -run 'Test.*Persist' -count=1 → existing tests pass; new tests isolate the stale-field/ordering failure until Steps 2–3. No test may touch the user's metadata.

### Step 2: Add the locked update primitive and migrate field mutations

Implement Update in a new short workspace_store_update.go. Validate ID; take its flock; load current supported metadata; clone/check identity before invoking the callback; propagate callback/read/write errors; validate the final workspace; write via saveWorkspaceLocked only on change. Prohibit callback reentrancy and keep callbacks pure. Clone caller-owned env maps/tab slices before retaining them.

Move Rename, SetEnv, and SetScripts onto Update while preserving no-op checks and current validation/defaulting. Add Update to the service interface and adapt test fakes only to preserve equivalent semantics. Convert service shelve intent/rollback/restore unarchive and archiveWorkspaceRecord/archiveDeletedWorkspaceMetadata flag changes to field transactions, preserving their operation ordering and error handling. Do not change durable tombstone deletion policy.

Tests must interleave independent setters using distinct store instances over one root; assert neither update is lost. Cover missing/corrupt/future-version records, invalid ID, callback error/no-op, identity mutation rejection, and immutable caller buffers.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/data ./internal/app/workspacesvc -count=1 → all store/lifecycle tests pass, including two-instance setter interleavings.

### Step 3: Route normal and shutdown tab persistence through ordered narrow writes

Add the service tab-persistence coordinator in workspace_tab_persistence.go. It accepts stable workspace ID, capture sequence, copied tabs, and active index, and calls store.Update only for these fields. A sequence at or below the last successfully committed sequence is a benign superseded outcome; a failed newest write is returned to the existing re-dirty path, never marked committed.

Increment capture sequence synchronously while collecting snapshots in App.Update and in the synchronous shutdown flush; never assign it inside a Cmd. Keep mutation-in-flight checks atomic with the save callback and preserve the existing failed-delete/shelve/restore requeue behavior. Replace both app_persistence complete-save call paths. Preserve the local-save fingerprint marker after actual successful writes; skipped stale writes should not fabricate a local write marker.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/app -run 'Test.*(Persist|WorkspaceMutation|Lifecycle|Delete|Shelve|Restore)' -count=1 → stale field, reversed command, shutdown ordering, and existing deletion barriers all pass.

### Step 4: Integrate and document field ownership

Review every service Save call remaining: each must be an intentional complete creation/import, not a delayed mutation of one field. Keep the in-memory UI update and store result handling coherent; do not move file locks onto new Update handlers. Document narrow tab writes and preserved settings across concurrent refreshes in README.md, docs/CONFIG.md, and docs/ORCHESTRATION.md. No schema bump, new key, or altered workspace ID is needed.

**Verify:** rg -n 'service.Save|workspaceService.Save' internal/app/app_persistence.go → no matches (exit 1 is expected). env -u AMUX_WORKSPACES_ROOT go test -race ./internal/data ./internal/app/workspacesvc ./internal/app → exit 0, no races. Run every final gate below.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Focused package verification | env -u AMUX_WORKSPACES_ROOT go test ./internal/data ./internal/app/workspacesvc ./internal/app | All selected tests pass; no new skips |
| Race verification | env -u AMUX_WORKSPACES_ROOT go test -race ./internal/data ./internal/app/workspacesvc ./internal/app | Exit 0, no race reports |
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

- internal/data/workspace_store.go
- internal/data/workspace_store_env.go
- internal/data/workspace_store_scripts.go
- internal/data/workspace_store_update.go
- internal/data/workspace_store_update_test.go
- internal/data/workspace_store_env_test.go
- internal/data/workspace_store_scripts_test.go
- internal/app/app_persistence.go
- internal/app/workspace_lifecycle_state.go
- internal/app/workspacesvc/services.go
- internal/app/workspacesvc/workspace_service.go
- internal/app/workspacesvc/workspace_service_init.go
- internal/app/workspacesvc/workspace_service_load.go
- internal/app/workspacesvc/workspace_service_shelve.go
- internal/app/workspacesvc/workspace_tab_persistence.go
- internal/app/*_test.go
- internal/app/workspacesvc/*_test.go
- README.md
- docs/CONFIG.md
- docs/ORCHESTRATION.md
- plans/README.md — only this plan's status row, unless the reviewer maintains it.

**Out of scope:** all other source files; unrelated refactoring; dependency/toolchain upgrades; changes to the trust model or automatic Git push/checkout behavior; other plans' implementation. Do not expand wildcard test scope into production edits.

## Git workflow

Work on advisor/039-transactional-workspace-field-updates only if the operator has selected that branch; otherwise use their current checkout. Do not commit, push, open a PR, stash, clean, reset, or discard user edits without explicit instruction. Before editing, record git status --short and the existing diff. Existing dirty changes are input to this plan, not cleanup targets. Only this plan's implementation delta must fit Scope; pre-existing unrelated edits may remain. Update only this plan's row in plans/README.md at completion unless the reviewer maintains that index.

## Test plan

Use the regression cases and existing test exemplars in the steps. Tests must assert the observable outcome, not merely that a new helper was called. Use temporary repositories/metadata and isolated tmux servers, never the user's actual state. Channel/barrier-based scheduling is preferred over arbitrary pacing sleeps.

## Done criteria

- [ ] Every regression case in Steps passes with env -u AMUX_WORKSPACES_ROOT go test ./internal/data ./internal/app/workspacesvc ./internal/app.
- [ ] env -u AMUX_WORKSPACES_ROOT go test -race ./internal/data ./internal/app/workspacesvc ./internal/app exits 0 with no races.
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
- If a callback requires path migration, nested store calls, process execution, or App-state mutation, stop and split that responsibility instead of broadening Update.
- Do not turn stale tab sequences into errors or clear dirty state after a failed write. Do not silently change cross-instance tab conflict policy.
- The test-file wildcards authorize interface-fake adaptations and relevant regressions only; they do not authorize relaxing unrelated assertions.
- If a new lock order conflicts with phaseMu, document a consistent ordering and prove it with the shutdown/mutation tests before proceeding.
- A broad baseline check fails outside scope: report the exact test/error and retain BLOCKED status; do not silently waive the gate or repair unrelated code.

## Maintenance notes

- Every new persisted field needs an explicit owner and a narrow transaction; adding it to Workspace must not make tab persistence overwrite it.
- Keep per-ID sequence ordering separate from the debounce token: a current token alone cannot order commands already dispatched.
- Plan 051 depends on this locking/field-ownership groundwork. It must not add port allocation to delayed full Workspace saves.
- Review shutdown and failed-lifecycle re-dirty paths whenever persistence scheduling changes.

