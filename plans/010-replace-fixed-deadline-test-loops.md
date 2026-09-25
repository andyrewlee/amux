# Plan 010: Replace remaining fixed-deadline polling loops in tmux/app tests with the shared wait helpers

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/tmux/ internal/app/ internal/testutil/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none (complementary to plans/005 — that one fixes the shared e2e *helpers*; this one sweeps the scattered inline loops)
- **Category**: tests
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

Beyond the shared e2e helpers (plans/005), a set of ad-hoc `deadline := now+N; for !cond { sleep }` polling loops remains in `internal/tmux`, `internal/app/workspacesvc`, and `internal/app` tests. Each is a wall-clock race that fails only under host load — the sharpest is the delete-kill-order test: if process spawn exceeds its 2s window, `scripts.IsRunning` never becomes true and the ordering assertion silently never executes. `internal/testutil` already provides `Eventually`/`WaitForAtomic` with failure diagnostics — these are mechanical swaps onto the shared, better-instrumented helpers (or onto readiness seams where one exists).

## Current state

Shared helpers already in the repo (`internal/testutil/wait.go`): `Eventually` (~:44), `WaitForAtomic` (~:70), plus a diagnostics-on-timeout convention — check the exact signatures before writing.

Sites to convert (verify each still exists — the drift check covers it):

1. `internal/tmux/tmux_kill_test.go:40-53` and `:89-104` — 2s deadline + 10ms sleeps polling `syscall.Kill(-pgid, 0)`.
2. `internal/tmux/probe_live_test.go:281-288` (5s) and `:296-304` (10s) — `waitForPaneMeta`/`waitForActivityAfterCreation` poll loops.
3. `internal/app/workspacesvc/workspace_service_delete_kill_order_test.go:161-167` — 2s deadline + 10ms sleep for `scripts.IsRunning` after launching `RunSetup` on a goroutine. Currently:

```go
	setupDone := make(chan error, 1)
	go func() {
		setupDone <- scripts.RunSetup(ws)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !scripts.IsRunning(ws) {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for setup script to be tracked")
		}
		time.Sleep(10 * time.Millisecond)
	}
```

4. `internal/app/state_watcher_test.go:207` — fixed 50ms sleep for fsnotify registration before the write under test.
5. `internal/app/service_tmux_test.go:381-386` — 40×25ms poll for pane content.
6. `internal/app/workspace_run_script_integration_test.go:167-170` and `:195-205` — 5s poll loops for script self-exit and marker file.
7. `internal/tmux/detached_session_test.go:52-53` — reuses `waitForSessionStatus` (5s deadline, 25ms sleep) for async `pane_dead` — this helper itself is the known-flake shape; check whether it needs a longer bound or a readiness signal rather than a rewrite (its sibling already flaked once in CI on the apt lane — widen the deadline or convert to the shared helper per site judgment).

## Commands you will need

| Purpose     | Command                                        | Expected on success |
|-------------|------------------------------------------------|---------------------|
| Build tests | `go vet ./internal/tmux ./internal/app/...`    | exit 0              |
| Tmux tests  | `go test ./internal/tmux -count=1`             | all pass or documented skip |
| App tests   | `go test ./internal/app ./internal/app/workspacesvc -count=1` | all pass |
| Lint        | `make lint`                                    | exit 0              |
| Full gate   | `make devcheck`                                | exit 0              |

## Scope

**In scope**:
- The seven files listed in Current state (test files only).
- `internal/testutil/wait.go` — ONLY if a needed helper shape is missing (e.g. a poll-with-timeout that returns the last-observed value for diagnostics); prefer reusing existing helpers.

**Out of scope**:
- `internal/e2e/*` — plans/005 owns the e2e pacing fixes.
- Production code — do not add readiness seams to non-test files; if a site genuinely cannot be polled (fsnotify registration), use the existing pattern (retry the write until observable, or a slightly longer bounded sleep documented as unavoidable).
- `waitForSessionStatus` callers beyond site 7 — batch only what this file needs.
- The `workspaceAgentTimeout`-style constant files — leave constants alone unless a site's deadline genuinely needs widening; the point is pollability, not longer sleeps.

## Git workflow

- Branch: `advisor/010-fixed-deadline-test-loops` off `main`.
- Commit style: `test: replace fixed-deadline poll loops with shared wait helpers`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Read `internal/testutil/wait.go` and map each site to a helper

Inventory the exported helpers and their signatures (`Eventually(cond func() bool, timeout time.Duration, msg string)`-style). For each of the seven sites, choose:

- `WaitForAtomic` where the condition is an atomic/int counter.
- `Eventually`/`WaitFor` where the condition is a bool func (e.g. `scripts.IsRunning(ws)`, `syscall.Kill(-pgid,0) != nil`, marker-file `os.Stat`).
- For `state_watcher_test.go:207` (fsnotify registration — no observable): replace the fixed sleep with a bounded retry of the *operation under test* (write → poll for the event) rather than a bigger sleep — check how sibling tests in that file handle watcher readiness and match.

**Verify**: `go vet` on the touched files → clean.

### Step 2: Convert the loops

Each conversion keeps semantics identical: same timeout budget (or the shared helper's default), same failure message quality (prefer the helper's built-in diagnostics). For site 3 specifically, keep the `setupDone` channel pattern — only the IsRunning poll changes.

**Verify** per file: `go test ./internal/tmux -run 'Kill|Probe' -count=1`, `go test ./internal/app -run 'Watcher|Tmux|RunScript' -count=1`, `go test ./internal/app/workspacesvc -count=1` → all pass.

### Step 3: Double-run the touched tests

**Verify**: `go test ./internal/tmux ./internal/app ./internal/app/workspacesvc -count=2` → green twice (these are the load-sensitive tests; double-run is the flake signal).

### Step 4: Full gate

**Verify**: `make devcheck` → exit 0.

## Test plan

- This plan IS test changes — the conversions are their own regression coverage. The kill-order test must still exercise the real ordering assertion (verify `removeCalled` is actually reached — the point of the fix is the ordering check can't silently skip).
- Verification: `go test ./internal/tmux ./internal/app ./internal/app/workspacesvc -count=1` → all pass.

## Done criteria

- [ ] No `deadline := time.Now().Add(...)` + `time.Sleep` poll loops remain at the listed sites (grep returns none there).
- [ ] `state_watcher_test.go` no longer relies on a fixed sleep for watcher readiness.
- [ ] `go test ./internal/tmux ./internal/app ./internal/app/workspacesvc -count=1` exits 0.
- [ ] `make devcheck` exits 0.
- [ ] No production files modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- `internal/testutil/wait.go` lacks a usable helper shape and adding one exceeds a few lines — report the gap.
- A site's deadline encodes a *behavioral* assertion, not a poll (e.g. "must complete within 2s" as the actual contract) — keep that deadline as the budget but still convert the loop shape; if in doubt, leave the site and note it.
- The fsnotify site has no observable at all and the retry-the-write approach breaks test semantics — keep the sleep with a justified comment and note in the PR.

## Maintenance notes

- New tests should never reintroduce `for !cond { time.Sleep }` — the convention is `testutil.Eventually`/`WaitForAtomic`; add to CONTRIBUTING or the package's test doc only if reviewers ask.
- The apt CI lane is where these flakes bite; after both timing plans land, the historical flake signatures (`TestRunSessionStatusReportsNonzeroExit`, `TestDragSelectUpAutoScrollsWhileRepainting`) should stop.
