# Plan 013: Add e2e coverage for unclean-restart reattach, rename, update prompt, and dialog queue ordering

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/e2e/ internal/app/app_overlay_arbiter.go internal/app/app_input_workspace.go internal/app/app_input_dialogs.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: L
- **Risk**: LOW
- **Depends on**: plans/005 (the pacing helpers make the new tests reliable — land that first)
- **Category**: tests
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

The highest-risk user-visible flows are proven only at unit level, never end-to-end against the real binary: (1) an **unclean exit** — kill -9, not the quit dialog — with tombstone recovery and tmux session reattach on restart (a wiring break here fails no test); (2) workspace **rename** through the UI; (3) the **update prompt** chain (check → settings → upgrade → toast); (4) two dialogs driven through `requestOverlayOpen`'s defer/drain queue. These are the exact flows a real user hits across the lifecycle the unit tests can't fully wire — quit cleanly is covered; die dirty is not.

## Current state

Existing e2e shape (`internal/e2e/`):

- `persistence_test.go:46-49` — `quitApp` exercises only the graceful path; no unclean-kill helper exists.
- 27 `Test` funcs cover shelve/restore/purge, script trust, clean-quit persistence, drag-scroll, keystroke close-loop, agent create/delete, discovery, GC. `grep -i 'rename|reattach|crash|update'` across `internal/e2e` returns only unrelated hits.
- The unit-level logic being e2e-gapped: tombstone recovery (`internal/app/workspace_delete_tombstone_test.go` covers the store side), `handleRenameWorkspace` (`internal/app/app_input_workspace.go:53` — unit-tested), update chain (`internal/app/app_input_dialogs.go:354-425` — unit-tested), overlay queue (`internal/app/app_overlay_arbiter.go:37-71` — unit-tested at `app_overlay_arbiter_test.go`, blind at e2e level).
- Harness/env knobs available to tests: `AMUX_E2E_BIN` (prebuilt binary), PTY helpers in `pty.go`, `timeouts.go` constants, `skipIfNoTmux`-style guards. To simulate an update-available state or a crash you may need seams — check what env vars `cmd/amux/main.go` and `internal/update` honor (e.g. a check URL override) and what signal an unclean kill needs (`exec.Command(...).Process.Kill()` on the spawned test app — confirm how tests spawn the binary in `helpers_test.go`).

Four scenarios, each a distinct test file (match the one-feature-per-file convention):

1. `crash_recovery_test.go`: create workspace+agent → `Process.Kill()` the app (not `quitApp`) → restart → assert the workspace's tmux session reattaches (agent tab present) AND a mid-delete tombstone, if left pending, is recovered per the tombstone contract.
2. `rename_test.go`: drive the rename dialog (prefix key per `prefixCommandTable` in `app_prefix.go` — check the actual binding) → new name appears in sidebar + session tags update.
3. `update_flow_test.go`: point the update check at a fake server (`httptest` in-process? — check whether `internal/update` supports an env URL override; if not, this scenario may be infeasible e2e → STOP condition).
4. `dialog_queue_test.go`: trigger two overlays that contend (e.g. trust prompt + delete confirm) → assert FIFO order per `app_overlay_arbiter` semantics — the first resolves before the second renders.

## Commands you will need

| Purpose    | Command                                  | Expected on success |
|------------|------------------------------------------|---------------------|
| Build      | `go build ./...`                         | exit 0              |
| E2E suite  | `go test ./internal/e2e -count=1`        | all pass (new tests included) |
| Focused    | `go test ./internal/e2e -run 'Crash|Rename|Update|DialogQueue' -v` | pass |
| Full gate  | `make devcheck`                          | exit 0              |

## Scope

**In scope**:
- New `internal/e2e/*_test.go` files for the four scenarios.
- `internal/e2e/helpers_test.go`/`pty.go` — a `killApp` helper (unclean kill) and any shared fixture the scenarios need.

**Out of scope**:
- Production seams for update URL injection — if none exists, the update scenario reduces to asserting the *absence* path plus a unit-level note; report rather than adding an env override just for tests (that'd be a product decision).
- More scenarios — this is the audit-identified gap set, not an exhaustive list.
- plans/005's helper refactors — depend on it, don't redo it.

## Git workflow

- Branch: `advisor/013-e2e-coverage-gaps` off `main`.
- Commit style: `test: add e2e coverage for crash recovery, rename, update flow, dialog queue`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: `killApp` helper + crash-recovery test

Add `killApp(t, inst)` that SIGKILLs the spawned binary (find the spawn helper in `helpers_test.go`/`pty.go` — likely `exec.Command` + PTY). Then `crash_recovery_test.go`: boot, create workspace + agent, kill, restart against the same `AMUX_HOME` fixture, assert reattach (agent tab present per `waitForUIContains` on tab name) and tombstone reconciliation (grep the tombstone contract in `internal/data/workspace_store_tombstone.go` to know what "recovered" means observably — likely workspace reappears or record gone).

**Verify**: `go test ./internal/e2e -run 'Crash' -v` → pass.

### Step 2: rename test

Drive `prefixCommandTable`'s rename binding (`grep -n 'rename' internal/app/app_prefix.go` for the key) through `sendPrefixCommand`, type the new name through the dialog input helpers, assert the sidebar shows it and (stronger) `tmux ls`/session tags reflect it via the existing tmux assertion helpers.

**Verify**: `go test ./internal/e2e -run 'Rename' -v` → pass.

### Step 3: update-flow test — feasibility first

`grep -rn 'update.*url\|UpdateURL\|AMUX_UPDATE' internal/update cmd/amux --include='*.go' | head` — if an env override exists, point it at an `httptest` server serving a fake newer release manifest and drive settings → upgrade → toast. If no seam exists, reduce scope: assert the update-prompt UI renders given an injectable `messages.UpdateAvailable`-equivalent trigger — or mark this scenario blocked and document why in the PR (do NOT add a production seam silently).

**Verify**: `go test ./internal/e2e -run 'Update' -v` → pass (or documented skip).

### Step 4: dialog-queue test

Trigger two contending overlays (e.g. open a dialog that defers while a trust prompt is up — find which pairs contend by reading `requestOverlayOpen` at `app_overlay_arbiter.go:37-71`), drive the first to resolution, assert the second renders only after (screen observable per scenario).

**Verify**: `go test ./internal/e2e -run 'DialogQueue' -v` → pass.

### Step 5: Full suite twice

**Verify**: `go test ./internal/e2e -count=2` → green twice; `make devcheck` → exit 0.

## Test plan

- The four new test files themselves; use `lifecycle_shelve_test.go`/`persistence_test.go` as structural patterns (spawn → drive keys → assert screen).
- Edge inside scenarios: restart with a *pending tombstone* (kill mid-delete) is the high-value edge — if triggering a mid-delete kill is racy, seed the tombstone record directly into the `AMUX_HOME` fixture metadata dir before restart (read `workspace_store_tombstone.go` for the record shape).

## Done criteria

- [ ] Four new e2e scenarios exist and pass (or the update scenario is documented as seam-blocked).
- [ ] `killApp` helper exists and is used by the crash test.
- [ ] `go test ./internal/e2e -count=2` exits 0 twice; `make devcheck` exits 0.
- [ ] No production files modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- plans/005 hasn't landed — pacing sleeps make these new tests flaky-by-construction; land 005 first.
- No unclean-kill mechanism exists in the spawn helper AND none can be added within test files — report.
- The update flow has no env/injection seam at all — reduce to the documented partial scope or mark BLOCKED.
- The dialog arbiter's defer semantics changed (drift) — re-derive the expected order.

## Maintenance notes

- These are the "restart matrix" tests; any future lifecycle state (e.g. a new tombstone kind) needs a row in the crash test.
- `killApp` tests are inherently slower (two boot cycles); keep them in the normal suite — the e2e package is already the slow lane.
- If CI's apt lane flakes on the new tests specifically, the readiness observables (plans/005) are the first suspect, not these tests.
