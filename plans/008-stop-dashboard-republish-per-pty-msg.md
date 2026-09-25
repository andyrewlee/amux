# Plan 008: Stop republishing dashboard active/agent state on every PTY message

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/app/app_input.go internal/app/app_input_keys.go internal/app/app_tmux_activity.go internal/app/app_tmux_activity_result.go internal/app/app_input_workspace.go internal/app/app_input_workspace_shelve.go internal/app/app_input_messages_workspace.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: MED
- **Depends on**: none
- **Category**: perf
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

Every `center.PTYOutput`/`PTYFlush`/`PTYStopped` message — up to ~25-60Hz per hot tab — calls `syncActiveWorkspacesToDashboard`, which allocates a fresh map, iterates `tmuxActivity.activeWorkspaceIDs` through `isWorkspaceMutationInFlight`, and calls `SetActiveWorkspaces` + `SetAgentStates`. But all three inputs are written only in the activity-result and lifecycle handlers, and every one of those mutators already invokes the sync itself. The per-PTY-message call republishes state that cannot have changed — residual overhead on the Update goroutine that scales with output rate, the opposite of the dirty-gate discipline the dashboard otherwise enforces.

## Current state

`internal/app/app_input.go:104-115`:

```go
	case center.PTYOutput, center.PTYFlush, center.PTYStopped:
		if cmd := a.handlePTYMessages(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Sync active agents state to dashboard (show spinner only when actively outputting)
		if cmd := a.syncActiveWorkspacesToDashboard(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if startCmd := a.dashboard.StartSpinnerIfNeeded(); startCmd != nil {
			cmds = append(cmds, startCmd)
		}
```

`syncActiveWorkspacesToDashboard` (`internal/app/app_input_keys.go:15-34`) rebuilds and republishes `activeWorkspaceIDs`/`agentStates`.

The publishers that already cover every mutation (verified call sites):

- `internal/app/app_tmux_activity_result.go:95` and `:156` — after the activity result mutates `activeWorkspaceIDs`/agent states.
- `internal/app/app_tmux_activity.go:327-330` — workspace activity edges.
- `internal/app/app_input_workspace_shelve.go:92`, `app_input_workspace.go:281`, `app_input_messages_workspace.go:293` — lifecycle mutations.

`StartSpinnerIfNeeded` is cheap (spinner state gate) and stays — only the map-rebuilding sync is removed.

## Commands you will need

| Purpose    | Command                             | Expected on success |
|------------|-------------------------------------|---------------------|
| Build      | `go build ./internal/app`           | exit 0              |
| Unit tests | `go test ./internal/app -count=1`   | all pass            |
| Lint       | `make lint && make lint-strict-new` | exit 0              |
| Perf       | `make perf-check`                   | within baseline     |
| Full gate  | `make devcheck`                     | exit 0              |

## Scope

**In scope**:
- `internal/app/app_input.go` — remove the `syncActiveWorkspacesToDashboard` call from the PTY-message case.
- `internal/app/app_input_keys.go` — only if the removed call was the function's last caller (check `grep -rn 'syncActiveWorkspacesToDashboard' internal/app` first; it almost certainly isn't — keep it for the listed mutators).
- Tests: whichever `internal/app` test file asserts dashboard sync behavior (`grep -rn 'syncActiveWorkspacesToDashboard\|SetActiveWorkspaces\|SetAgentStates' internal/app --include='*_test.go'`).

**Out of scope**:
- `syncActiveWorkspacesToDashboard` itself and all its remaining call sites — they are the correct publishers.
- `StartSpinnerIfNeeded` — stays on the PTY path.
- `internal/ui/dashboard` internals.
- Any consolidation of the mutator-side call sites — this plan only removes the redundant one.

## Git workflow

- Branch: `advisor/008-pty-dashboard-republish` off `main`.
- Commit style: `perf: stop republishing dashboard state on every PTY message`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Enumerate every producer of the synced inputs

`grep -rn 'activeWorkspaceIDs\|SetAgentStates\|agentStates\[' internal/app --include='*.go' | grep -v _test` — list every write site and confirm each is paired with a `syncActiveWorkspacesToDashboard` call. The audit identified the six sites in Current state; re-verify the complete list — if any write site LACKS a sync call, this plan is invalid as written → STOP.

**Verify**: the grep output has a sync call at or adjacent to every mutation site.

### Step 2: Remove the per-PTY-message call

Delete the `syncActiveWorkspacesToDashboard` block in the `center.PTYOutput/PTYFlush/PTYStopped` case (app_input.go:106-113), keeping `handlePTYMessages` and `StartSpinnerIfNeeded`. Adjust the comment: the spinner note can stay attached to `StartSpinnerIfNeeded`.

**Verify**: `go build ./internal/app` → exit 0.

### Step 3: Prove no regression — dashboard state still tracks activity edges

Add or extend an `internal/app` test: drive a `tmuxActivityResult`-style message that flips a workspace's active/agent state → assert `SetActiveWorkspaces`/`SetAgentStates` effect lands on the dashboard model (check how existing tests observe dashboard state — `a.dashboard` internals or a fake). If a test already covers the mutator-path sync (likely — `app_tmux_activity*_test.go`), pin it and add one case asserting a `PTYFlush` alone does NOT republish (e.g. counter fake or `contentVersion` unchanged — use whatever observation point exists).

**Verify**: `go test ./internal/app -count=1 -run 'Activity|Dashboard|PTY' -v` → pass.

### Step 4: Gates

**Verify**: `make devcheck` → exit 0; `make lint-strict-new` → exit 0; `make perf-check` → within baseline.

## Test plan

- New/extended: mutator-path sync still lands; PTY path no longer republishes.
- Structural pattern: `internal/app/app_tmux_activity*_test.go` — the activity-edge tests.
- Edge: a `PTYStopped` arriving between activity-edge writes must not revert state (it can't — it no longer publishes).

## Done criteria

- [ ] `center.PTY*` messages no longer invoke `syncActiveWorkspacesToDashboard`.
- [ ] Every mutation site of `activeWorkspaceIDs`/agent-states still calls the sync (verified by grep + test).
- [ ] `go test ./internal/app -count=1` exits 0.
- [ ] `make devcheck`, `lint-strict-new`, `perf-check` pass.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- Step 1 finds a write site without a paired sync call — the per-PTY republish may be *load-bearing* for that path; report which site.
- `syncActiveWorkspacesToDashboard` reads inputs that are ALSO mutated inside `handlePTYMessages` (e.g. PTY-stopped removes an active ID) — then the call is partially load-bearing; report rather than deleting.
- The dashboard's dirty-gate semantics already made this call free (i.e. `SetActiveWorkspaces` early-outs on `maps.Equal`) — the map-alloc/iteration cost is still real; proceed, but note if reviewers prefer the cheaper fix of keeping the call (gate still pays the alloc).

## Maintenance notes

- This is an "invariant" fix: correct today only because every mutator publishes. Any FUTURE write to `activeWorkspaceIDs`/agent-states MUST call `syncActiveWorkspacesToDashboard` — add that sentence to the function's godoc or the struct field's comment if not already stated (check `internal/app/app_core.go` field docs).
- If a reviewer prefers belt-and-braces (keep a low-frequency re-sync), the honest version is a periodic tick, not per-message — don't reintroduce this on the PTY path.
- Watch `make perf-check` output for the hot-tabs preset; this removal should be neutral-to-positive.
