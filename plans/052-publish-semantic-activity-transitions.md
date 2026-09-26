# Plan 052: Publish activity changes that occur through elapsed time

> **Executor instructions:** Follow each step and its verification. This is a self-contained implementation handoff; no audit-session context is assumed. Honor the STOP conditions. No commits or pushes are authorized.
>
> **Drift check (run first):**
> ~~~sh
> git diff --stat 7c530ee..HEAD -- internal/app/app_tmux_activity.go internal/app/app_tmux_activity_state.go internal/app/app_tmux_activity_result.go internal/app/app_tmux_activity_shared.go internal/app/app_tmux_activity_agent_state_tag_test.go internal/app/app_tmux_activity_shared_test.go internal/app/app_tmux_activity_handle_result_test.go internal/app/app_on_done_test.go internal/app/activity/agent_state.go internal/app/activity/agent_state_test.go internal/app/activity/logic.go internal/app/activity/logic_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md
> git diff HEAD -- internal/app/app_tmux_activity.go internal/app/app_tmux_activity_state.go internal/app/app_tmux_activity_result.go internal/app/app_tmux_activity_shared.go internal/app/app_tmux_activity_agent_state_tag_test.go internal/app/app_tmux_activity_shared_test.go internal/app/app_tmux_activity_handle_result_test.go internal/app/app_on_done_test.go internal/app/activity/agent_state.go internal/app/activity/agent_state_test.go internal/app/activity/logic.go internal/app/activity/logic_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md
> git status --short
> ~~~
> Compare BOTH commit changes and the current working tree against the Current state excerpts below. The audited baseline intentionally includes dirty user work on 2026-09-26. A known dirty file is not automatically a mismatch: preserve the audited behavior and the user's additional changes. On unexplained semantic drift, stop and report. New files will appear only in status, so inspect them before creating a same-named file.

## Status

- **Priority:** P2
- **Effort:** M
- **Risk:** MED
- **Depends on:** none
- **Category:** bug
- **Planned at:** commit 7c530ee, 2026-09-26, INCLUDING the known dirty working tree (run-session picker/transcript-browser changes).
- **Status:** TODO

## Why this matters

The published tmux state is compared by reclassifying both old and new snapshots at the same current time. When the done window expires, both classify idle and no update is sent, even though the last published value was done. External consumers can retain stale state indefinitely. Track the actual last accepted semantic value, and evaluate quiet retained sessions as well as sessions whose score changed.

## Current state

- internal/app/app_tmux_activity_result.go:192–200:
~~~go
func sessionAgentStateChanges(prevStates, updatedStates map[string]*activity.SessionState, now time.Time) []agentStateTagChange {
    var changes []agentStateTagChange
    for name, next := range updatedStates {
        prevState := activity.ClassifyState(prevStates[name], now)
        nextState := activity.ClassifyState(next, now)
        if nextState != prevState {
            changes = append(changes, agentStateTagChange{sessionName: name, state: nextState, prev: prevState})
        }
    }
    return changes
}
~~~
- internal/app/activity/agent_state.go:70–79 bases state on timestamps:
~~~go
if !isActive && !state.LastActiveAt.IsZero() && now.Sub(state.LastActiveAt) < HoldDuration {
    isActive = true
}
// ...
if !state.LastWorkingAt.IsZero() && now.Sub(state.LastWorkingAt) < DoneWindow {
    return StateDone
}
return StateIdle
~~~
HoldDuration is six seconds; DoneWindow is thirty seconds. Neither duration changes in this plan.
- applyTmuxActivityPayload calls the comparison before merging UpdatedStates at app_tmux_activity_result.go:136. The state struct currently stores hysteresis pointers, not last accepted per-session semantic values.
- app_tmux_activity.go:174 builds workspace states from updatedStates only. Some idle/seen paths mutate a scan snapshot but do not emit an updated entry, so solely iterating UpdatedStates also misses quiet sessions.
- Match the immutable scan-result / Update-only-writer convention and token/epoch fencing in app_tmux_activity.go and app_tmux_activity_shared.go. Existing tests use fakeSetAgentStateTag and stubOnDoneHook; those are the side-effect seams to extend.
- docs/ORCHESTRATION.md:227 promises idle/working/done telemetry written best effort on changes. It permits transient failures, not deterministic permanent staleness.

## Behavior decisions

Store the last accepted semantic enum per session on tmuxActivityState, separately from hysteresis snapshots. Compare current classification to that enum, never to a timestamp-reinterpreted previous snapshot. Evaluate every retained eligible session each successful owner scan, including those with no content/score update. Use one explicit classification timestamp per result/helper so tests can advance a clock without sleeping.

Keep tag writes best effort and bounded to accepted state changes; this plan does not add retry-until-success or exactly-once distributed delivery. On first observation, publish the current value to correct a stale existing tag, but do not fire on-done without a locally observed prior Working state in this ownership epoch. On owner acquisition/reset clear the local semantic baseline; followers never run session hooks or publish session-state transitions from shared workspace-only snapshots. Drop removed sessions from both maps. A confirmed stopped/disappeared session must not manufacture a completion hook solely from pruning.

## Steps

### Step 1: Pin clock-only transitions with pure tests

Extend app_tmux_activity_agent_state_tag_test.go with an explicit sequence of accepted states at supplied timestamps: Working, then Done, then Idle after DoneWindow with no terminal-content change. Cover a below-threshold score held Working by LastActiveAt that becomes Done after HoldDuration. Assert exactly one tag per semantic change, not one per scan; unchanged states produce no additional writes.

Use existing seam-based tests to assert first observation of Done causes no hook and a genuinely observed Working→Done edge causes exactly one hook. A hand-built previous snapshot reclassified at the new time is not an adequate regression.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/app -run 'Test.*(SessionAgentState|AgentStateTag|OnDone)' -count=1 → existing cases pass; the new clock-only sequence demonstrates the missing tag before Step 2.

### Step 2: Carry complete eligible session state through accepted owner scans

Add the enum baseline map to tmuxActivityState/newTmuxActivityState. At the scan boundary, assemble a complete current session-state snapshot from the scan-owned state map plus updated entries, excluding removed sessions. Preserve mutations from seen-but-not-updated paths. Carry sufficient observed/stopped information to distinguish quiet live sessions from disappeared sessions; do not infer live completion from absence.

Classify the complete eligible set at one timestamp. Use it consistently for per-session transitions and the workspace semantic summary while preserving the existing active-workspace hysteresis decisions. If the existing active classifier's special fresh-tag path disagrees with ClassifyState, characterize it and preserve its documented activity behavior; do not tune thresholds to make tests convenient.

On the Update loop, compare accepted enums, generate tag/hook changes, then update the baseline. Commands receive immutable change slices; never let background commands mutate either map.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/app/activity ./internal/app -run 'Test.*(AgentState|Activity|OnDone)' -count=1 → quiet retained sessions become idle and all prior working/settle/hysteresis assertions pass.

### Step 3: Fence ownership, stale results, and pruning

Reset/reseed the enum baseline at the same owner/epoch transition that resets hysteresis. Ignore failed, SkipApply, stale token, and stale ownership results for both semantic publication and hooks. Followers continue consuming shared workspace state without fabricating per-session changes. Pruning removes baseline entries; reappearing sessions start as first observations.

Extend shared-scan and result-handler tests: owner→follower→new owner does not duplicate a previous on-done; stale result cannot revert a tag; unseen/stopped pruning does not fire completion; a new working cycle after reappearance can fire once. Preserve existing trust handling for on-done scripts.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test -race ./internal/app/activity ./internal/app -run 'Test.*(Activity|AgentState|OnDone)' -count=1 → no duplicate hooks, stale publications, or races.

### Step 4: Document telemetry semantics and run real gates

Update README.md, docs/CONFIG.md, and docs/ORCHESTRATION.md: quiet sessions publish Done→Idle expiry, first observations/handoffs do not replay hooks, and tag writes remain best effort. Do not add a public tag, timer, config key, or promise exactly-once external side effects.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/app/activity ./internal/app ./internal/tmux → all pass. Then run every final gate.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Focused package verification | env -u AMUX_WORKSPACES_ROOT go test ./internal/app/activity ./internal/app ./internal/tmux | All selected tests pass; no new skips |
| Race verification | env -u AMUX_WORKSPACES_ROOT go test -race ./internal/app/activity ./internal/app | Exit 0, no race reports |
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

- internal/app/app_tmux_activity.go
- internal/app/app_tmux_activity_state.go
- internal/app/app_tmux_activity_result.go
- internal/app/app_tmux_activity_shared.go
- internal/app/app_tmux_activity_agent_state_tag_test.go
- internal/app/app_tmux_activity_shared_test.go
- internal/app/app_tmux_activity_handle_result_test.go
- internal/app/app_on_done_test.go
- internal/app/activity/agent_state.go
- internal/app/activity/agent_state_test.go
- internal/app/activity/logic.go
- internal/app/activity/logic_test.go
- README.md
- docs/CONFIG.md
- docs/ORCHESTRATION.md
- plans/README.md — only this plan's status row, unless the reviewer maintains it.

**Out of scope:** all other source files; unrelated refactoring; dependency/toolchain upgrades; changes to the trust model or automatic Git push/checkout behavior; other plans' implementation. Do not expand wildcard test scope into production edits.

## Git workflow

Work on advisor/052-publish-semantic-activity-transitions only if the operator has selected that branch; otherwise use their current checkout. Do not commit, push, open a PR, stash, clean, reset, or discard user edits without explicit instruction. Before editing, record git status --short and the existing diff. Existing dirty changes are input to this plan, not cleanup targets. Only this plan's implementation delta must fit Scope; pre-existing unrelated edits may remain. Update only this plan's row in plans/README.md at completion unless the reviewer maintains that index.

## Test plan

Use the regression cases and existing test exemplars in the steps. Tests must assert the observable outcome, not merely that a new helper was called. Use temporary repositories/metadata and isolated tmux servers, never the user's actual state. Channel/barrier-based scheduling is preferred over arbitrary pacing sleeps.

## Done criteria

- [ ] Every regression case in Steps passes with env -u AMUX_WORKSPACES_ROOT go test ./internal/app/activity ./internal/app ./internal/tmux.
- [ ] env -u AMUX_WORKSPACES_ROOT go test -race ./internal/app/activity ./internal/app exits 0 with no races.
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
- If a new behavior requires changing HoldDuration, DoneWindow, active scores, bell policy, or owner lease protocol, stop; none is authorized here.
- Do not use only UpdatedStates as the evaluated set; the regression includes a retained quiet session that emits no content update.
- Do not replay on-done on startup/handoff or claim exactly-once delivery across crashed owners.
- If first-observation publication would require trusting unowned sessions or bypassing existing session filters, stop and preserve the filter boundary.
- A broad baseline check fails outside scope: report the exact test/error and retain BLOCKED status; do not silently waive the gate or repair unrelated code.

## Maintenance notes

- Future time-derived states must be compared against stored semantic values, not recomputed historical snapshots.
- Keep owner resets, removed-state pruning, and enum baselines in sync. Pure clock tests should remain independent of five-second real timers.
- A failed best-effort write can still leave telemetry temporarily stale; reliable delivery/retry is a separate design, not an accidental expansion of this change.

