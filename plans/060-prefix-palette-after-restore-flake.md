# Plan 060: Prefix palette intermittently fails after workspace restore

> Found during plan-059/036 stack execution (2026-09-26): `TestShelveRestorePurgeLifecycle` fails ~2/3 of runs at `lifecycle_shelve_test.go:126` — `sendPrefixCommand` waits 5s for the palette footer "Esc cancel" and times out. Reproduces identically at `7c530ee`, at the 032/033 baseline commit, and under plan-059 — **pre-existing**, not introduced by this stack.

## Symptoms

- Deterministic location: the *third* prefix use (the post-restore `sendPrefixCommand "h"` at line 126), after step-2 restore. Earlier prefix arms in the same test work.
- Screen at timeout: normal dashboard, `shelveme ?` row — no palette, no visible dialog.
- Failure runs take longer overall (30–58s vs ~20s pass), consistent with an extra wait/timeout upstream slowing the sequence.

## Root cause (confirmed via instrumented run)

Two chained defects, captured in the app log of a failing run:

1. **Identity-drift guard hole** — `markWorkspaceMutationInFlight` marked each
   form of `WorkspaceIdentitySet` independently and accepted the mark when ANY
   form succeeded. `ComputedID` flips when the worktree dir appears
   (`NormalizePath` resolves `/var`→`/private/var` only for existing paths),
   so a `RestoreWorkspace` request dispatched after restore #1's
   `git worktree add` created the dir — but before its completion released
   the mark — carried a fresh ComputedID that marked cleanly alongside the
   still-marked storeID. Two restores ran **concurrently**; #2's
   `worktree add` lost the race ("branch already exists" / "path already
   exists").
2. **Error eats a keypress** — the #2 failure surfaced as `a.err`, and
   `handleKeyPress` consumes the next key to dismiss the overlay. The test's
   NUL leader byte was that keypress; the palette never armed.

The e2e restore loop presses Enter every poll interval until the worktree
appears, so post-completion presses are guaranteed under load — this is also
a real production defect (double-Enter on a shelved row could run
concurrent `git worktree add`s and show a spurious internal error).

## Fix

- `workspaceLifecycleState.markMutatingWorkspaceIDs`: atomic set-wide
  mark — reject when ANY identity form or the root bridge is already
  mutating; all-or-nothing marking with rollback on a failed transition.
- `Service.RestoreWorkspace`: after snapshot validation, consult the store —
  if the record is already live, return `WorkspaceRestoreSkipped` (a benign
  no-op success) instead of re-running worktree add / setup / toasts.
- `handleWorkspaceRestoreSkipped`: release the guard and spinner, reload
  projects, advance a bulk drain as a success.
- e2e: the step-3 prefix arm keeps the log dump on failure permanently.

Not in scope: shelve/delete have the same theoretical stale-duplicate
exposure, but their key cadence is gated by confirm dialogs and no flake was
observed there.

## Verification

- `env -u AMUX_WORKSPACES_ROOT go test ./internal/e2e -run '^TestShelveRestorePurgeLifecycle$' -count=10` — 10/10 pass.
- `env -u AMUX_WORKSPACES_ROOT make devcheck` — e2e green.
