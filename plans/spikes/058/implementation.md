# Plan 058 build handoff: recovery diagnostics in the status dialog

Self-contained build plan for the GO verdict in `decision.md`. Cases cited
as `case:<id>` reference `recovery-cases.json`.

## Final presentation choice

A `cleanup` section appended to the existing `renderWorkspaceStatus`
output, rendered **only** when at least one alias-ID tombstone is detected
(`case:status-no-tombstone`). Two honest states plus unknown:

- `interrupted — worktree still present; workspace remains usable`
  (tombstone + `DirExists(root)`), detail row → warn-level `"workspace
  delete"` log (`case:status-tombstone-root-present`)
- `pending — worktree already removed; retry automatic on next load`
  (tombstone + no root), detail row → warn-level `"startup recovery"` log
  (`case:status-tombstone-root-gone`, `case:branch-cleanup-failure-state`)
- `unknown — could not read recovery state` when identity fields are
  missing/unreadable (`case:status-probe-read-failure`)

No stage or error text is ever rendered — the marker is a boolean and the
UI must not fabricate one (`case:session-metadata-cleanup-pending`
documents the suppressed-row limitation).

## Scoped files

- `internal/app/app_workspace_status.go` — `workspaceStatus` gains
  `cleanupPending bool` and `cleanupRootPresent bool` (a two-bit honest
  state, not a stage enum); `buildWorkspaceStatus`/`fillRunnerStatus` do
  NOT read it — it joins the off-loop probe below; `renderWorkspaceStatus`
  gains the conditional section.
- `internal/app/app_workspace_status_test.go` — render + async-identity
  tests (extend existing patterns).
- No changes to `internal/data`, `internal/git`, `workspacesvc` deletion
  code, or any key map. The dialog stays read-only
  (`case:no-retry-action-in-v1`).

## Data access — read-only, off-loop

Add the probe to the existing `handleShowWorkspaceStatus` cmd (which
already runs `RunScriptStatus` + `WorkspacePortInterval` off-Update-loop
under `runOutputToken` fencing):

```go
cleanupPending, cleanupRootPresent := svc.WorkspaceCleanupSnapshot(&snap)
```

New `workspacesvc` method (small, side-effect-free, all stat reads):

```go
// WorkspaceCleanupSnapshot derives the honest two-bit recovery state:
// any alias-ID tombstone, and whether the worktree root still exists.
// It performs no mutation — os.Stat only.
func (s *Service) WorkspaceCleanupSnapshot(ws *data.Workspace) (pending, rootPresent bool)
```

Implementation: iterate `WorkspaceMetadataIDs(ws)` calling
`s.store.IsDeleting(id)` (`case:status-alias-id-tombstone`,
`case:retry-boundary-identity-drift`); `rootPresent = DirExists(ws.Root)`.
Nil store → `(false, false)` → no section — the ordinary case is unchanged.
Missing identity/root → render the unknown state
(`case:status-probe-read-failure`).

## Async fencing

- Results ride the existing `workspaceStatusReadyMsg` (add the two bool
  fields) — the `runOutputToken` stale-drop at
  `handleWorkspaceStatusReady` covers them for free
  (`case:stale-result-delivery`).
- The probe reads `ws.Clone()` taken at open — the snapshot describes the
  workspace at open time even if selection changes mid-flight
  (`case:selected-workspace-changed-mid-open`).
- A port-read error keeps today's contract: toast, no dialog — the cleanup
  booleans die with the dropped dialog (no partial render).

## Retry safety boundary (unchanged by this increment)

No action is added. The section text points at automatic load-time retry;
the live-root pin (`case:retry-boundary-root-reappears`) and identity
fencing belong to `finishInterruptedDelete` and are untouched. Any future
retry action must re-clear the plan's boundary list before implementation.

## Tests → case map

| Case IDs | Test |
|---|---|
| status-no-tombstone | `TestBuildWorkspaceStatus_NoCleanupSection` — assert rendered text has no `cleanup` header |
| status-tombstone-root-present | `TestBuildWorkspaceStatus_CleanupInterrupted` — fake store marker + real TempDir root |
| status-tombstone-root-gone, branch-cleanup-failure-state | `TestBuildWorkspaceStatus_CleanupPending` — marker + nonexistent root |
| status-alias-id-tombstone | `TestWorkspaceCleanupSnapshot_AliasID` — marker under legacy ID form only |
| status-probe-read-failure | `TestBuildWorkspaceStatus_CleanupUnknown` |
| stale-result-delivery | extend existing token-fence test with the new fields |
| selected-workspace-changed-mid-open | extend clone-snapshot coverage |
| retry-boundary-root-reappears, retry-boundary-identity-drift, no-retry-action-in-v1, env-privacy-preserved | assert-zero-mutation tests: drive `handleShowWorkspaceStatus` cmd + `handleWorkspaceStatusReady` with a spy `gitOps`/`store` and assert no mutating calls (mirroring `TestFinishInterruptedDelete_` exemplars) |
| session-metadata-cleanup-pending | document-only: asserted via suppressed-row behavior already covered by tombstone tests |

## Verification

```bash
go test ./internal/app -run 'TestBuildWorkspaceStatus|TestWorkspaceStatus|WorkspaceCleanupSnapshot' -count=1
go test ./internal/app/workspacesvc -run 'TestFinishInterruptedDelete_|TestWorkspaceCleanupSnapshot' -count=1
go test -race ./internal/app ./internal/app/workspacesvc -count=1
make lint-strict-new
env -u AMUX_WORKSPACES_ROOT make devcheck       # until 059 lands
env -u AMUX_WORKSPACES_ROOT make verify-loop    # overlay input path untouched, but keep the gate
make harness-presets && PERF_STRICT=1 make perf-check   # status render path changed
```

Expected: all green; perf within noise (two `os.Stat` calls per status
open, off-loop; render adds ≤3 lines).

## STOP conditions

- If deriving the section requires reading anything beyond
  `IsDeleting`/`DirExists` (i.e. someone wants stage/error text), STOP —
  that is the rejected journal; re-spike rather than fabricate.
- If the probe cannot stay allocation/mutation-free, STOP.
- If a caller needs the section to refresh live, STOP — the dialog is
  one-shot by contract; a refresh feature is a different spike.

## Documentation (same change)

- `README.md` — `i` status row: note the cleanup section appears when a
  workspace delete was interrupted, and that the view stays read-only.
- `docs/CONFIG.md` — no new keys; nothing to add unless a config surface
  appears (it does not).
- `docs/ORCHESTRATION.md` — lifecycle/delete section: document the
  tombstone → status-section → auto-retry-on-load flow so operators can
  diagnose without reading code.

## Maintenance notes

- Revisit the state table if tombstone identity, lifecycle phases, or git
  timeout semantics change (plan text's standing rule).
- If a manual retry is ever proposed, it must reuse
  `finishInterruptedDelete`'s precondition chain verbatim — no new
  destructive authority.
