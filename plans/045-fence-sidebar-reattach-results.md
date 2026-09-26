# Plan 045: Fence sidebar attachment outcomes by generation

> **Executor instructions:** Follow the lifecycle decisions and gates in order. Do not commit or push. The coordinating reviewer owns the index unless explicitly delegated.
>
> **Drift check first:** Run `git status --short`, `git diff --stat 7c530ee..HEAD -- internal/ui/sidebar/terminal.go internal/ui/sidebar/terminal_pty_config.go internal/ui/sidebar/terminal_reattach_stall.go internal/ui/sidebar/terminal_sessions.go internal/ui/sidebar/terminal_pty_attach.go internal/ui/sidebar/terminal_pty_lifecycle.go internal/ui/sidebar/terminal_update_session.go internal/ui/sidebar/terminal_reattach_generation_test.go internal/ui/sidebar/terminal_reattach_test.go internal/ui/sidebar/terminal_reattach_stall_test.go internal/ui/sidebar/terminal_pty_attach_test.go internal/ui/sidebar/terminal_pty_attach_ops_test.go internal/ui/sidebar/terminal_pty_attach_env_test.go internal/ui/sidebar/terminal_pty_attach_snapshot_test.go internal/ui/sidebar/terminal_pty_attach_safety_test.go internal/ui/sidebar/terminal_pty_attach_snapshot_race_test.go internal/ui/sidebar/terminal_update_session_test.go internal/ui/sidebar/terminal_capture_size_test.go internal/ui/sidebar/terminal_workspace_rebind_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md`, then `git diff HEAD --` with the same paths. Read untracked in-scope files too. Compare Current state; baseline includes the dirty tree audited on 2026-09-26, not only committed code. Preserve all existing changes.

## Status

- **Priority:** P2
- **Effort:** M
- **Risk:** MED
- **Depends on:** [plan 059](059-isolate-e2e-workspace-root.md) before unrestricted e2e validation; until then use the sanitized commands below
- **Category:** bug / lifecycle
- **Planned at:** commit `7c530ee`, plus audited uncommitted working tree, 2026-09-26

## Why this matters

Sidebar reattachment can outlive its stall timeout or an explicit user detach. Its outcomes carry no generation, so a late success overwrites a newer terminal and a late failure marks the newer attachment stopped. Apply results only to the still-current attempt, and release orphaned clients without reversing the user's action.

## Current state

amux is a Go Bubble Tea v2 application. Commands do I/O and return messages; tab state changes belong on Update, with `TerminalState.mu` protecting fields also read by workers. The center pane already implements reattach epochs; this plan brings the sidebar to that existing discipline without changing the shared PTY reader algorithm.

`internal/ui/sidebar/terminal_sessions.go:34–44` guards automatic attachment:

```go
if ts.Reattach.InFlight {
    return false
}
if ts.UserDetached {
    return false
}
// ... existing running check ...
ts.beginReattachLocked()
return true
```

Manual `ReattachActiveTab` and `RestartActiveTab` in `terminal_pty_attach.go:153–198` do not acquire that guard. `terminal_reattach_stall.go:25–31` explicitly says a stall sweep releases the lock without cancelling the command.

`terminal_pty_config.go:76–84` declares success with workspace/tab identity and Terminal but no epoch. `terminal_update_session.go:132–136` unconditionally applies it:

```go
ts.Terminal = msg.Terminal
ts.Running = true
ts.Detached = false
ts.UserDetached = false
ts.Reattach.InFlight = false
```

Failure at `:164–165` unconditionally sets `Running=false` and clears InFlight. The lost attempt can therefore damage a newer successful one. `TerminalState` at `terminal.go:39–50` has a shared `ptyio.ReattachGuard`, but no local generation.

Use `internal/ui/center/model_tabs_session_reattach.go:55–77` and `model_input_lifecycle.go:89–122` as read-only exemplars: capture the epoch at dispatch; reject superseded results; close rejected agents; retain explicit-detach intent. The center tests in `model_reattach_detach_race_test.go` show state assertions. Sidebar seam tests must not use `t.Parallel`; see `internal/ui/sidebar/no_parallel_guard_test.go`.

## Commands you will need

| Purpose | Command | Expected result |
|---|---|---|
| Baseline | `go test ./internal/ui/sidebar -count=1` | PASS |
| Lifecycle regressions | `go test ./internal/ui/sidebar -run 'TestSidebarReattachGeneration|Test.*Reattach|Test.*Attach' -count=1 -v` | Named new and existing tests PASS |
| Race | `go test -race ./internal/ui/sidebar ./internal/ui/ptyio -count=1` | PASS, no race reports |
| Repository race | `env -u AMUX_WORKSPACES_ROOT make test-race` | Exit 0 with no race reports |
| Real-tmux race | `env -u AMUX_WORKSPACES_ROOT make test-race-tmux` | Exit 0; actual tmux tests execute and no race reports |
| Real tmux | `env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e -count=1 -v` | PASS; report skips and failures precisely |
| Real input | `env -u AMUX_WORKSPACES_ROOT make verify-loop` | Both required tests PASS, not SKIP |
| Required checks | `env -u AMUX_WORKSPACES_ROOT make devcheck` | Exit 0, or report unresolved baseline/fix failures honestly |
| Lint | `make lint-strict-new` | Exit 0 |
| Hygiene | `git diff --check` | Exit 0 |

The audit observed failing devcheck real-e2e tests and a passing verify-loop. It later identified inherited `AMUX_WORKSPACES_ROOT` defeating test HOME isolation; plan 059 repairs that boundary. Until it lands, every command reaching e2e must remove that variable as above. Do not classify remaining failures as unrelated without isolated evidence or change unrelated tests to obtain green output.

## Scope

**In scope:** the seven production files listed in the drift check (`terminal.go`, `terminal_pty_config.go`, `terminal_reattach_stall.go`, `terminal_sessions.go`, `terminal_pty_attach.go`, `terminal_pty_lifecycle.go`, `terminal_update_session.go`); new `terminal_reattach_generation_test.go`; the existing sidebar test files listed in the drift check only to adapt command signatures and stamp current epochs on fixtures; README, CONFIG, ORCHESTRATION; index status if delegated.

**Out of scope:** center source, shared `ptyio.ReattachGuard` API, PTY reader flush/EOF fixes, tmux session names/tags/ownership policy, session discovery, agent config, pending terminal creation, UI rendering, and broad cleanup refactors.

## Git workflow

Use the operator's checkout (optional isolated branch `advisor/045-sidebar-reattach-generation`). Record initial dirty files and preserve all user work. No stash, clean, reset, commit, push, or PR without authorization. Use only temporary test sessions; do not target production tmux clients.

## Steps

### Step 1: Add the attempt identity and failing interleaving tests

Add a `uint64` reattach epoch to `TerminalState` and an `Epoch uint64` to both result types. Zero is not a production wildcard. Introduce locked helpers for beginning an attempt (increment epoch, acquire ReattachGuard), checking that an outcome is current (epoch matches and InFlight remains true), and invalidating an attempt (advance epoch and clear InFlight). No worker may read the mutable epoch after dispatch.

Create controlled command/result tests named `TestSidebarReattachGeneration...`: old success after a newer successful attempt, old failure after new success, user detach while result is pending, duplicate success, and retry after a simulated stall. Use explicit ordering/channel barriers or direct result delivery, not sleeps. Construct independent `*pty.Terminal` test objects through existing seams and verify rejected clients are closed. A test-only close seam is allowed in `terminal_pty_lifecycle.go` if the existing Terminal test object cannot observe closure; restore it via t.Cleanup.

**Verify:** `go test ./internal/ui/sidebar -run TestSidebarReattachGeneration -count=1 -v` → the new tests fail on stale-state/client-lifetime assertions until routing is fixed. Existing fixtures may temporarily require epoch initialization; do not bypass validation for them.

### Step 2: Acquire and snapshot an attempt at every dispatch boundary

Unify manual reattach, manual restart, and automatic discovery attachment around the locked begin/check helper. At most one current attempt may be dispatched for a tab; duplicate manual actions return an informational in-progress toast instead of issuing another command. Explicit manual attach may clear UserDetached as part of starting a new attempt; automatic attach continues to respect it. Pass the captured epoch explicitly into `attachToSession` and stamp every success/error return, including early shell/env/tmux failures. Keep workspace/tab identity capture and rebind resolution intact.

For restart, retain the current running-tab refusal and kill/recreate semantics; acquire the attempt once, after any deliberate detach invalidation, and do not accidentally invalidate the new attempt during its own cleanup. Do not move unrelated tmux operations in this step.

**Verify:** `go test ./internal/ui/sidebar -run 'TestSidebarReattachGeneration.*Dispatch|Test.*Attach|Test.*Restart' -count=1 -v` → one command per current attempt, stamped outcomes on every path, existing ownership/capture tests PASS.

### Step 3: Reject stale outcomes and invalidate on user/lifecycle changes

Resolve the tab, then validate the epoch and InFlight under its mutex before touching capture, running flags, terminal, or reader state. A stale success closes only its orphaned incoming client; a stale failure changes nothing and emits no misleading failure toast. A nil incoming terminal for a current success is a failed attach that releases the attempt and surfaces a failure; it is not Running=true.

On an accepted success, finish the attempt so duplicate deliveries are rejected. If a distinct obsolete client remains, stop its reader and close that client outside the state mutex before it can be leaked; keep the new client's ownership separate. Never close an incoming pointer that is already the accepted current client when rejecting a duplicated result. Ensure reader restart points at the newly accepted Terminal, preserving the existing capture/resize/response-writer ordering.

Invalidate before explicit detach and tab/workspace teardown. A stall sweep must invalidate the expired epoch, not just clear InFlight, so late completion is rejected even before retry begins. Preserve epochs across workspace rebinding because TabID/TerminalState identity survives that operation. Update fixture results to acquire/carry genuine epochs; no zero-epoch compatibility bypass.

**Verify:** `go test ./internal/ui/sidebar -run 'TestSidebarReattachGeneration|Test.*Reattach|Test.*Attach' -count=1 -v` → all PASS; `go test -race ./internal/ui/sidebar ./internal/ui/ptyio -count=1` → PASS and no races/client leaks reported.

### Step 4: Document lifecycle guarantees and run real gates

Update README, CONFIG, and ORCHESTRATION consistently: explicit detach is not reversed by a pending attach, duplicate attachment requests coalesce/refuse while in progress, and stalled attempts can be retried safely. No names/tags/new config keys change. Run the sanitized real-tmux/input checks and final gates from the table; record their actual results and do not treat a skip as proof of real attachment.

**Verify:** `env -u AMUX_WORKSPACES_ROOT make verify-loop` → required PASS lines; `env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e -count=1 -v` → PASS with real coverage, no unexplained skips; `env -u AMUX_WORKSPACES_ROOT make test-race` and `env -u AMUX_WORKSPACES_ROOT make test-race-tmux` → exit 0 with no races; `make lint-strict-new` → exit 0; `env -u AMUX_WORKSPACES_ROOT make devcheck` → exit 0, otherwise record BLOCKED; `git diff --check` → clean.

## Test plan

Cover old success/failure arriving before and after the replacement success; duplicate success with the same Terminal pointer; detach before outcome; delete/close before outcome; stall invalidation without immediate retry; retry after stall; duplicate manual reattach/restart; auto attach honoring UserDetached; manual attach clearing user detach only for the current attempt; nil successful Terminal; workspace rebind; all early command failures; normal capture/resize paths. Use the center's tests as behavior exemplars, and sidebar's existing injection seams without parallel tests. Assert exact current pointer/flags, incoming orphan closure, and no closure of the accepted pointer.

## Done criteria

All required final gates must pass before this plan is marked DONE. A known or newly discovered gate failure leaves the plan BLOCKED with the exact command/test and evidence; recording a failure is not a substitute for passing. Do not expand implementation scope to repair other findings. Until [plan 059](059-isolate-e2e-workspace-root.md) lands, remove `AMUX_WORKSPACES_ROOT` from every command reaching e2e, including devcheck, verify-loop, and test-race-tmux.

- [ ] `TestSidebarReattachGeneration...` cases execute and PASS.
- [ ] No production sidebar reattach result is constructed without its captured epoch.
- [ ] `go test -race ./internal/ui/sidebar ./internal/ui/ptyio -count=1` and `make lint-strict-new` pass.
- [ ] Sanitized repository `make test-race` and `make test-race-tmux` pass without races, with real tmux execution recorded.
- [ ] Sanitized verify-loop passes; sanitized tmux/e2e and devcheck outcomes are recorded accurately.
- [ ] All three lifecycle contract documents agree; no session tag/name/config change was introduced.
- [ ] `git diff --check` is clean and implementation changes stay within the stated files beyond the starting dirty tree.
- [ ] Designated index owner records status and any validation blockers.

## STOP conditions

Stop if the shared reader cannot be rebound without editing `ptyio`, if valid result identity cannot survive the existing workspace rebind, if the fix requires persistent epoch storage or tmux tags, or after two failed focused correction attempts. Stop rather than accepting zero epochs to satisfy old tests. Do not run e2e with inherited workspace-root overrides before plan 059 lands. Report new gate failures and investigate attribution before calling them baseline.

## Maintenance notes

The guard prevents concurrent current attempts; the epoch protects against abandoned attempts that still finish. Both are needed. Every future detach/restart/teardown path must explicitly preserve or invalidate the attempt identity. Keep rejected-success resource cleanup and duplicate-pointer protection in review checks.
