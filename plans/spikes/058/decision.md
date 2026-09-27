# Plan 058 decision: workspace recovery diagnostics

## Evidence

Two durable mechanisms exist today, neither of which surfaces to the user:

1. **Store tombstone** — `WorkspaceStore.MarkDeleting(id)` writes a
   crash-safe `.deleting` marker (fsatomic temp+fsync+rename, payload `"1"`)
   inside `store/<id>/` (`internal/data/workspace_store_tombstone.go:16-56`).
   It is per *metadata ID*; a workspace can carry an alias set
   (`data.WorkspaceIdentitySet(ws)` via `WorkspaceMetadataIDs`), and
   `IsDeleting`/`ClearDeleting` are boolean stat/remove — **no stage, no
   error, no timestamp**. A successful `Delete` removes the whole metadata
   directory, clearing the marker implicitly.
2. **Git-layer pending-cleanup state** —
   `persistAndResumeWorkspaceCleanup`/`writePendingWorkspaceCleanupState`
   (`internal/git/workspace_cleanup_recovery.go`) stage resumable cleanup
   state *inside the workspace path* and return
   `ErrWorkspaceCleanupPending` (typed error, `internal/git/workspace_errors.go`).
   This covers worktree-removal interruption, distinct from the store
   tombstone's delete-flow interruption.

Consumers of the tombstone:

- `finishInterruptedDelete` (workspace_delete_tombstone.go:83) runs at load
  (`workspace_service_load.go:103`): tombstone + root gone → kill sessions,
  retry branch delete (tombstone proves the delete passed validation →
  branch is amux-owned; `IsBranchNotFoundError` = success), retry metadata
  delete. Branch-delete failure **surfaces the workspace** and preserves
  the tombstone so the next load retries; metadata-delete failure keeps the
  workspace hidden and retries next load; session-kill failure likewise.
- Tombstone + root still present → returns false: workspace stays usable
  (`KeepsTombstonedWithLiveWorktree` pins this).
- `RescanWorkspaces` consults `hasDeleteTombstone` and skips archival so a
  tombstoned record stays in `listByRepo` and the recovery loop cannot be
  permanently broken by archiving (`workspace_service_load.go:292-302`).

The status dialog (`internal/app/app_workspace_status.go`) is the natural
surface: `i` opens a one-shot read-only snapshot; run-session and port reads
already run off-Update-loop under `runOutputToken` fencing, stale results
are dropped (lines 64, 97-98), a failed port read toasts instead of
rendering a guess, and env values are names-only by contract (lines 39-41).
README documents `i` as a read-only snapshot — any action would change that
contract.

## State model

What the code distinguishes today, honestly derivable from `IsDeleting`
per alias ID + `DirExists(root)` + presence in `project.Workspaces`:

| State | Observable source | Survives restart | Row visible | App cannot know |
|---|---|---|---|---|
| No tombstone | `IsDeleting` false for all alias IDs | n/a | normal row | — |
| Tombstone + root present | marker exists, `DirExists(root)` | yes | yes (surfaced) | which pre-removal step failed; the error |
| Tombstone + root gone + branch cleanup failed | marker exists, no root, row surfaced by `finishInterruptedDelete` | yes | yes | that it was specifically the branch step — only "root gone, cleanup incomplete" |
| Tombstone + root gone + metadata/session cleanup pending | marker exists, no root, row suppressed at next load | yes | **no** — workspace vanishes between loads while tombstone remains | pending step identity; whether another instance is mid-retry |
| Completed cleanup | marker + whole metadata dir gone | n/a | no | — |
| No-longer-present workspace (dir deleted out-of-band, no tombstone) | stored record, `DirExists` false | yes | yes until rescan archives it | whether deletion was user-initiated |

Alias IDs matter: the marker may live under the legacy ID form while the
live workspace reports the canonical `MetadataID()` — diagnostics must check
the full `WorkspaceMetadataIDs` set, mirroring `finishInterruptedDelete`.

Deliberately **not** derivable from the boolean marker: exact failed stage,
the error, wall-clock age, retry-in-progress. Labeling any of those from a
boolean would be fabrication — the design presents only the honest
two-bit derivation (tombstone? root?) plus a pointer to logs.

New structured persistence (a stage/error journal inside the marker) was
evaluated: schema + ownership + lifetime + bounded sanitized fields +
unknown-version policy + 039-transaction interaction, all to answer "which
step." Cost is real (new durable format in `internal/data`), benefit is
marginal — the pending steps retry automatically at load and the only
user-visible stale state (branch survives) already resolves via "delete it
again." Rejected for v1; a revisit trigger is recorded below.

## Presentation

Read-only `cleanup:` section in the existing status dialog, rendered only
when a tombstone is detected — absence of the section = no pending cleanup
(the ordinary case stays byte-identical). Rows:

```
ordinary (unchanged today)          tombstone + root present            tombstone + root gone
──────────────────────────          ──────────────────────────          ─────────────────────────
identity                            identity                            identity
  branch:     feat/x                  branch:     feat/x                  branch:     feat/x
  path:       ~/repo/.amux/feat-x     path:       ~/repo/.amux/feat-x     path:       ~/repo/.amux/feat-x
...                                 cleanup                             cleanup
                                      deletion:   interrupted — worktree  deletion:   pending — worktree
                                                still present; workspace                        already removed
                                                remains usable          retry:      automatic on next load;
                                      detail:     see log level=warn                            branch/metadata steps may
                                                  "workspace delete"                              still be outstanding
                                                                            detail:   see log level=warn
                                                                                                "startup recovery"
```

Incomplete-information rendering (store unreadable / alias set empty):

```
cleanup
  deletion:   unknown — could not read recovery state
```

Failed off-loop probe (port read error): unchanged contract — toast, no
dialog. A `DirExists`/`IsDeleting` stat failure is indistinguishable from
absent by signature; the section renders "unknown" only when the workspace
itself is missing identity fields, never invents a state.

## Retry safety

**No retry action ships in v1.** The boundary analysis for a follow-up:

- The only existing authorized operation is `finishInterruptedDelete` —
  it already runs unattended at every load, is identity-fenced by
  `WorkspaceMetadataIDs`, re-validates `DirExists(root)` before any
  destructive step (a reappeared root aborts — the live-worktree pin), and
  its branch retry is fenced by the tombstone proving prior validation.
- A user-triggered retry would need: the same `DirExists` precondition, a
  confirmation step, the `runOutputToken`+workspace-pointer identity fence
  against mid-dialog selection change, and error reporting at
  `logging.Error` (user-initiated) vs `Warn` (background recovery).
- Because the automatic load-time retry already covers every reachable
  pending state, a manual action adds only latency-to-fix, not capability —
  insufficient to change `i`'s read-only contract in v1. Revisit trigger:
  a reported case where auto-retry is unreachable without restart.
- **Invariant regardless of version**: a marker with a live root is never
  permission to delete that root; opening status never runs archive scripts
  or mutates tombstones (`may_mutate=false` on every diagnostic case).
- Latest error/stage stays **session-local log-only** — the UI never claims
  a durable "last error" it doesn't have; the detail row points at logs.

## Alternatives

- **Structured delete journal** (stage + sanitized error in the marker):
  rejected above — new durable schema for a question auto-retry already
  answers.
- **Retry button in the dialog**: deferred — duplicates the load-time path;
  `i` is documented read-only.
- **New lifecycle CLI**: rejected — ORCHESTRATION.md's standing rule and
  this spike establishes no unmet orchestration requirement.
- **Dashboard badge on tombstoned rows**: deferred — the surfaced row is
  already visible; `i` explains it. A badge is polish, not diagnosis.
- **Push the probe onto the Update loop**: rejected — consistent with every
  other status read; file IO joins the existing off-loop closure for free.

## Decision

**GO** — read-only `cleanup:` diagnostic section, presented only when a
tombstone exists over the alias-ID set, honestly limited to the two-bit
derivation (interrupted-with-live-root vs pending-with-root-gone) plus a
log pointer. No retry action, no new persistence. Effort S.

Handoff: `implementation.md`; scenario matrix in `recovery-cases.json`.
